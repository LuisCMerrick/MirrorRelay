// Repository detail dialog: desired/active comparison, statistics, upstreams,
// interactive client config playground, configuration previews, cache purge and profile upgrades.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { card, disclosure, kv, showPreview } from '../components.js';
import { $, copyText, esc, notice } from '../dom.js';
import { bytes, date, number, stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';
import { publicURL } from '../repositories.js';
import { state } from '../state.js';
import { loadMirrors } from './mirrors.js';

function repositorySummary(repository) {
  const blocked = (repository.blocked_packages || []).length > 0 ? `<span class="status-pill status-unhealthy">${(repository.blocked_packages || []).length} ${L('rules')}</span>` : L('None');
  const allowed = (repository.allowed_packages || []).length > 0 ? `<span class="status-pill status-healthy">${(repository.allowed_packages || []).length} ${L('rules')}</span>` : L('All permitted');
  return `${kv(L('Public URL'), publicURL(repository))}${kv(L('Type / mode'), `${repository.type} / ${repository.proxy_mode}`)}${kv(L('Profile'), `${repository.profile_name || 'Custom'} ${repository.profile_version || ''}`)}${kv(L('Cache'), repository.cache_enabled ? `${repository.cache_profile} · ${repository.cache_authenticated ? L('authenticated enabled') : L('anonymous only')}` : L('Disabled'))}${kv(L('Browsable HTML URL rewrite'), repository.html_rewrite_enabled ? L('Enabled') : L('Disabled'))}${kv(L('Rewrite hosts'), (repository.rewrite_hosts || []).join(', ') || '—')}${kv(L('Blocked packages (Blacklist)'), blocked)}${kv(L('Allowed packages (Whitelist)'), allowed)}${repository.config_error ? `<div class="notice error">${esc(repository.config_error)}</div>` : ''}`;
}

function generateClientPlayground(repository, currentBaseUrl) {
  const type = repository.type;
  const slug = repository.slug;
  const baseUrl = currentBaseUrl || publicURL(repository);
  let defaultVariant = '';
  let variants = [];
  let formats = [];

  switch (type) {
    case 'apt': {
      const profileName = `${repository.profile_name || ''} ${repository.name || ''}`.toLowerCase();
      const debian = [
        { id: 'debian-12', label: 'Debian 12 (Bookworm)' },
        { id: 'debian-11', label: 'Debian 11 (Bullseye)' }
      ];
      const debianSecurity = [
        { id: 'debian-security-12', label: 'Debian 12 Security (Bookworm)' },
        { id: 'debian-security-11', label: 'Debian 11 Security (Bullseye)' }
      ];
      const ubuntu = [
        { id: 'ubuntu-2404', label: 'Ubuntu 24.04 LTS (Noble)' },
        { id: 'ubuntu-2204', label: 'Ubuntu 22.04 LTS (Jammy)' }
      ];
      if (profileName.includes('debian security') || profileName.includes('debian-security')) {
        variants = debianSecurity;
      } else if (profileName.includes('ubuntu')) {
        variants = ubuntu;
      } else if (profileName.includes('debian')) {
        variants = debian;
      } else {
        variants = [...debian, ...ubuntu];
      }
      formats = [
        { id: 'deb822', label: L('DEB822 (.sources)'), default: true },
        { id: 'sources.list', label: L('sources.list one-line format') }
      ];
      defaultVariant = variants[0].id;
      break;
    }
    case 'rpm':
      variants = [
        { id: 'rocky-9', label: 'Rocky Linux 9', contentdir: 'rocky', releasever: '9' },
        { id: 'rocky-8', label: 'Rocky Linux 8', contentdir: 'rocky', releasever: '8' },
        { id: 'almalinux-9', label: 'AlmaLinux 9', contentdir: 'almalinux', releasever: '9' },
        { id: 'centos-7', label: 'CentOS 7', contentdir: 'centos', releasever: '7' }
      ];
      defaultVariant = variants[0].id;
      break;
    case 'pypi':
      variants = [
        { id: 'pip', label: 'pip (Standard)', cli: `pip config set global.index-url ${baseUrl}/simple/` },
        { id: 'uv', label: 'uv (Astral)', cli: `export UV_DEFAULT_INDEX="${baseUrl}/simple/"` },
        { id: 'poetry', label: 'poetry', cli: `poetry source add --priority=default mirror ${baseUrl}/simple/` }
      ];
      defaultVariant = variants[0].id;
      break;
    case 'npm':
      variants = [
        { id: 'npm', label: 'npm', cli: `npm config set registry ${baseUrl}/` },
        { id: 'yarn', label: 'yarn', cli: `yarn config set registry ${baseUrl}/` },
        { id: 'pnpm', label: 'pnpm', cli: `pnpm config set registry ${baseUrl}/` }
      ];
      defaultVariant = variants[0].id;
      break;
    case 'docker-registry':
    case 'oci-registry':
      variants = [
        { id: 'daemon-json', label: 'Docker daemon.json mirror' },
        { id: 'podman', label: 'Podman registry' }
      ];
      defaultVariant = variants[0].id;
      break;
    case 'goproxy':
      variants = [
        { id: 'go', label: 'Go Environment', cli: `go env -w GOPROXY=${baseUrl},direct` }
      ];
      defaultVariant = variants[0].id;
      break;
    case 'cargo':
      variants = [
        { id: 'cargo', label: 'Cargo (Rust)', filename: 'config.toml' }
      ];
      defaultVariant = variants[0].id;
      break;
    case 'maven':
      variants = [
        { id: 'maven', label: 'Maven', filename: 'settings.xml' }
      ];
      defaultVariant = variants[0].id;
      break;
    case 'iso':
      variants = [
        { id: 'aria2', label: 'Aria2 (16-thread High Speed)' },
        { id: 'wget', label: 'Wget (Resumable Download)' },
        { id: 'curl', label: 'Curl' },
        { id: 'sha256', label: 'SHA256 Checksum Verification' }
      ];
      defaultVariant = variants[0].id;
      break;
    default:
      variants = [
        { id: 'generic', label: 'Standard', cli: `curl -fLO ${baseUrl}/path/to/file` }
      ];
      defaultVariant = variants[0].id;
  }

  return { variants, formats, defaultVariant, baseUrl, slug, type };
}

function shellQuote(value) {
  return "'" + String(value).replace(/'/g, "'\"'\"'") + "'";
}

function computePlaygroundOutput(type, variantId, baseUrl, selectedFormat = '', repositorySlug = '') {
  let cliCmd = '';
  let fileContent = '';
  let fileName = '';
  let filePath = '';

  switch (type) {
    case 'apt': {
      const specs = {
        'debian-12': { suites: ['bookworm', 'bookworm-updates', 'bookworm-backports'], components: 'main contrib non-free non-free-firmware', keyring: '/usr/share/keyrings/debian-archive-keyring.gpg' },
        'debian-11': { suites: ['bullseye', 'bullseye-updates', 'bullseye-backports'], components: 'main contrib non-free', keyring: '/usr/share/keyrings/debian-archive-keyring.gpg' },
        'debian-security-12': { suites: ['bookworm-security'], components: 'main contrib non-free non-free-firmware', keyring: '/usr/share/keyrings/debian-archive-keyring.gpg' },
        'debian-security-11': { suites: ['bullseye-security'], components: 'main contrib non-free', keyring: '/usr/share/keyrings/debian-archive-keyring.gpg' },
        'ubuntu-2404': { suites: ['noble', 'noble-updates', 'noble-backports', 'noble-security'], components: 'main restricted universe multiverse', keyring: '/usr/share/keyrings/ubuntu-archive-keyring.gpg' },
        'ubuntu-2204': { suites: ['jammy', 'jammy-updates', 'jammy-backports', 'jammy-security'], components: 'main restricted universe multiverse', keyring: '/usr/share/keyrings/ubuntu-archive-keyring.gpg' }
      };
      const spec = specs[variantId] || specs['debian-12'];
      const repositoryURL = String(baseUrl).replace(/[\r\n]/g, '').replace(/\/+$/, '') + '/';
      const safeSlug = String(repositorySlug || 'repository').toLowerCase().replace(/[^a-z0-9._-]+/g, '-');
      const fileBase = `mirrorrelay-${safeSlug}`;
      if (selectedFormat === 'sources.list') {
        fileName = `${fileBase}.list`;
        filePath = `/etc/apt/sources.list.d/${fileName}`;
        fileContent = spec.suites.map(suite => `deb [signed-by=${spec.keyring}] ${repositoryURL} ${suite} ${spec.components}`).join('\n');
      } else {
        fileName = `${fileBase}.sources`;
        filePath = `/etc/apt/sources.list.d/${fileName}`;
        fileContent = `Types: deb\nURIs: ${repositoryURL}\nSuites: ${spec.suites.join(' ')}\nComponents: ${spec.components}\nSigned-By: ${spec.keyring}`;
      }
      const quotedLines = fileContent.split('\n').map(shellQuote).join(' ');
      cliCmd = `printf '%s\\n' ${quotedLines} | sudo tee ${filePath} >/dev/null\nsudo apt update`;
      break;
    }

    case 'rpm':
      fileName = 'mirrorrelay.repo';
      filePath = '/etc/yum.repos.d/mirrorrelay.repo';
      if (variantId.startsWith('rocky')) {
        const ver = variantId.endsWith('9') ? '9' : '8';
        fileContent = `[baseos]\nname=Rocky Linux $releasever - BaseOS\nbaseurl=${baseUrl}/$releasever/BaseOS/$basearch/os/\ngpgcheck=1\nenabled=1\n\n[appstream]\nname=Rocky Linux $releasever - AppStream\nbaseurl=${baseUrl}/$releasever/AppStream/$basearch/os/\ngpgcheck=1\nenabled=1`;
        cliCmd = `sudo sed -e 's|^mirrorlist=|#mirrorlist=|g' -e 's|^#baseurl=http://dl.rockylinux.org/\\$contentdir|baseurl=${baseUrl}|g' -i.bak /etc/yum.repos.d/rocky*.repo\nsudo dnf makecache`;
      } else if (variantId.startsWith('almalinux')) {
        fileContent = `[baseos]\nname=AlmaLinux $releasever - BaseOS\nbaseurl=${baseUrl}/$releasever/BaseOS/$basearch/os/\ngpgcheck=1\nenabled=1\n\n[appstream]\nname=AlmaLinux $releasever - AppStream\nbaseurl=${baseUrl}/$releasever/AppStream/$basearch/os/\ngpgcheck=1\nenabled=1`;
        cliCmd = `sudo sed -e 's|^mirrorlist=|#mirrorlist=|g' -e 's|^# baseurl=https://repo.almalinux.org/almalinux|baseurl=${baseUrl}|g' -i.bak /etc/yum.repos.d/almalinux*.repo\nsudo dnf makecache`;
      } else {
        fileContent = `[base]\nname=CentOS-$releasever - Base\nbaseurl=${baseUrl}/7/os/$basearch/\ngpgcheck=1\nenabled=1`;
        cliCmd = `sudo sed -e 's|^mirrorlist=|#mirrorlist=|g' -e 's|^#baseurl=http://mirror.centos.org/centos|baseurl=${baseUrl}|g' -i.bak /etc/yum.repos.d/CentOS-Base.repo\nsudo yum makecache`;
      }
      break;

    case 'pypi':
      fileName = 'pip.conf';
      filePath = '~/.pip/pip.conf';
      fileContent = `[global]\nindex-url = ${baseUrl}/simple/`;
      if (variantId === 'uv') {
        cliCmd = `export UV_DEFAULT_INDEX="${baseUrl}/simple/"`;
      } else if (variantId === 'poetry') {
        cliCmd = `poetry source add --priority=default mirror ${baseUrl}/simple/`;
      } else {
        cliCmd = `pip config set global.index-url ${baseUrl}/simple/`;
      }
      break;

    case 'npm':
      fileName = '.npmrc';
      filePath = '~/.npmrc';
      fileContent = `registry=${baseUrl}/`;
      if (variantId === 'yarn') {
        cliCmd = `yarn config set registry ${baseUrl}/`;
      } else if (variantId === 'pnpm') {
        cliCmd = `pnpm config set registry ${baseUrl}/`;
      } else {
        cliCmd = `npm config set registry ${baseUrl}/`;
      }
      break;

    case 'docker-registry':
    case 'oci-registry':
      const host = baseUrl.replace(/^https?:\/\//, '');
      fileName = 'daemon.json';
      filePath = '/etc/docker/daemon.json';
      fileContent = JSON.stringify({
        "registry-mirrors": [baseUrl]
      }, null, 2);
      cliCmd = `docker pull ${host}/library/nginx:latest`;
      break;

    case 'goproxy':
      fileName = 'go.env';
      filePath = '~/.config/go/env';
      fileContent = `GOPROXY="${baseUrl},direct"`;
      cliCmd = `go env -w GOPROXY=${baseUrl},direct`;
      break;

    case 'cargo':
      fileName = 'config.toml';
      filePath = '~/.cargo/config.toml';
      fileContent = `[source.crates-io]\nreplace-with = 'mirrorrelay'\n\n[source.mirrorrelay]\nregistry = "sparse+${baseUrl}/"`;
      cliCmd = `mkdir -p ~/.cargo && cat << 'EOF' > ~/.cargo/config.toml\n[source.crates-io]\nreplace-with = 'mirrorrelay'\n\n[source.mirrorrelay]\nregistry = "sparse+${baseUrl}/"\nEOF`;
      break;

    case 'maven':
      fileName = 'settings.xml';
      filePath = '~/.m2/settings.xml';
      fileContent = `<settings>\n  <mirrors>\n    <mirror>\n      <id>mirrorrelay</id>\n      <name>MirrorRelay Central</name>\n      <url>${baseUrl}/</url>\n      <mirrorOf>central</mirrorOf>\n    </mirror>\n  </mirrors>\n</settings>`;
      cliCmd = `mkdir -p ~/.m2`;
      break;

    case 'iso':
      fileName = 'download.sh';
      filePath = './download.sh';
      if (variantId === 'aria2') {
        fileContent = `aria2c -x 16 -s 16 -k 1M -c "${baseUrl}/path/to/image.iso"`;
        cliCmd = `aria2c -x 16 -s 16 -k 1M -c "${baseUrl}/path/to/image.iso"`;
      } else if (variantId === 'wget') {
        fileContent = `wget -c --progress=bar:force "${baseUrl}/path/to/image.iso"`;
        cliCmd = `wget -c --progress=bar:force "${baseUrl}/path/to/image.iso"`;
      } else if (variantId === 'sha256') {
        fileName = 'verify.sh';
        filePath = './verify.sh';
        fileContent = `curl -sSL "${baseUrl}/SHA256SUMS" | sha256sum -c --ignore-missing`;
        cliCmd = `curl -sSL "${baseUrl}/SHA256SUMS" | sha256sum -c --ignore-missing`;
      } else {
        fileContent = `curl -C - -O -L "${baseUrl}/path/to/image.iso"`;
        cliCmd = `curl -C - -O -L "${baseUrl}/path/to/image.iso"`;
      }
      break;

    default:
      fileName = 'download.sh';
      filePath = './download.sh';
      fileContent = `curl -fLO ${baseUrl}/`;
      cliCmd = `curl -fLO ${baseUrl}/`;
  }

  return { cliCmd, fileContent, fileName, filePath };
}

function triggerDownload(fileName, content) {
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = fileName;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

async function showRepository(id) {
  try {
    const repositoryState = await api(`/mirrors/${id}/state`);
    const desired = repositoryState.desired, active = repositoryState.active_found ? repositoryState.active : null, statistics = repositoryState.statistics || {};
    const latest = state.profiles.find(profile => profile.name === desired.profile_name && profile.latest_stable);
    const canManage = state.role === 'admin' || state.role === 'operator';
    const upgrade = canManage && latest && latest.version !== desired.profile_version ? `<button class="btn-primary requires-operator" data-action="preview-profile-upgrade" data-id="${id}" data-name="${esc(latest.name)}" data-version="${esc(latest.version)}">${icon('zap', 13)} ${L('Preview upgrade to %s', latest.version)}</button>` : '';

    const playgroundData = generateClientPlayground(desired);
    let selectedVariant = playgroundData.defaultVariant;
    let selectedFormat = playgroundData.formats.find(value => value.default)?.id || playgroundData.formats[0]?.id || '';

    const variantOptions = playgroundData.variants.map(v => `<option value="${esc(v.id)}"${v.id === selectedVariant ? ' selected' : ''}>${esc(v.label)}</option>`).join('');
    const formatOptions = playgroundData.formats.map(format => `<option value="${esc(format.id)}"${format.id === selectedFormat ? ' selected' : ''}>${esc(format.label)}</option>`).join('');
    const formatControl = formatOptions ? `<label>
            <span>${L('Configuration format')}</span>
            <select id="playground-format">${formatOptions}</select>
          </label>` : '';
    const trafficDetails = [
      kv(L('Effective config'), `v${repositoryState.effective_config_version || '—'}`),
      kv(L('Observed cache traffic'), bytes(statistics.cache_bytes || 0)),
      kv('2xx / 3xx / 4xx / 5xx', `${number(statistics.status_2xx || 0)} / ${number(statistics.status_3xx || 0)} / ${number(statistics.status_4xx || 0)} / ${number(statistics.status_5xx || 0)}`)
    ].join('');

    $('#detail-title').textContent = desired.name;
    $('#detail-content').innerHTML = `
      <div class="cards detail-cards">
        ${card(L('Desired state'), stateLabel(desired.config_state), desired.config_state === 'active', 'check-circle')}
        ${card(L('Active state'), active ? L('Published') : L('Not active'), Boolean(active), 'server')}
        ${card(L('Requests today'), number(statistics.requests || 0), false, 'trend-up')}
        ${card(L('Traffic today'), bytes(statistics.bytes || 0), false, 'ingress')}
        ${card(L('Cache HIT / MISS'), `${number(statistics.cache_hits || 0)} / ${number(statistics.cache_misses || 0)}`, false, 'database')}
        ${card(L('Upstream errors'), number(statistics.upstream_errors || 0), false, 'alert')}
      </div>
      ${disclosure(L('Traffic details'), trafficDetails, {
        iconName: 'activity',
        description: L('Effective version, cache bytes and HTTP status classes')
      })}
      <div class="toolbar">
        <div class="actions">
          <button data-action="copy-repository-url" data-id="${id}">${icon('copy', 13)} ${L('Copy URL')}</button>
          <button class="requires-operator" data-action="edit-mirror-from-detail" data-id="${id}">${icon('edit', 13)} ${L('Edit')}</button>
          <button class="requires-operator" data-action="check-mirror" data-id="${id}">${icon('play', 13)} ${L('Test')}</button>
          <button class="requires-operator" data-action="preview-repository-config" data-id="${id}">${icon('code', 13)} ${L('Preview config')}</button>
          <button class="requires-operator" data-action="view-effective-config">${icon('server', 13)} ${L('Effective config')}</button>
          <button class="requires-operator" data-action="purge-repository" data-id="${id}">${icon('database', 13)} ${L('Purge cache')}</button>
          ${upgrade}
        </div>
      </div>
      
      <!-- Interactive Client Setup Playground -->
      <div class="panel playground-panel">
        <div class="panel-header-row">
          <h2>${icon('terminal', 18)} ${L('Interactive One-Click Setup Generator')}</h2>
          <span class="badge blue">${esc(desired.type)}</span>
        </div>
        <div class="playground-controls form-grid">
          <label>
            <span>${L('Client / Environment Variant')}</span>
            <select id="playground-variant">${variantOptions}</select>
          </label>
          ${formatControl}
          <label>
            <span>${L('Target Base URL')}</span>
            <input id="playground-url" value="${esc(playgroundData.baseUrl)}" />
          </label>
        </div>

        <div class="playground-output">
          <div class="example">
            <div class="toolbar">
              <strong>${icon('terminal', 14)} ${L('One-liner CLI Command')}</strong>
              <button id="copy-cli-btn" class="secondary small">${icon('copy', 12)} ${L('Copy CLI')}</button>
            </div>
            <pre id="playground-cli-pre"></pre>
          </div>

          <div class="example">
            <div class="toolbar">
              <strong>${icon('file-text', 14)} <span id="playground-file-path"></span></strong>
              <div class="actions">
                <button id="download-file-btn" class="secondary small">${icon('external-link', 12)} ${L('Download')}</button>
                <button id="copy-file-btn" class="secondary small">${icon('copy', 12)} ${L('Copy File')}</button>
              </div>
            </div>
            <pre id="playground-file-pre"></pre>
          </div>
        </div>
      </div>

      <div class="disclosure-stack">
        ${disclosure(L('Desired configuration'), repositorySummary(desired), {iconName: 'settings'})}
        ${disclosure(
          L('Active routing snapshot'),
          active ? repositorySummary(active) : `<p class="muted">${L('No active version. The desired configuration may have failed validation or activation.')}</p>`,
          {iconName: 'server'}
        )}
      </div>
      <div class="panel">
        <h2>${icon('globe', 16)} ${L('Upstreams')}</h2>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>${L('Priority')}</th>
                <th>${L('URL')}</th>
                <th>${L('Health')}</th>
                <th>${L('Latency')}</th>
                <th>${L('Last check')}</th>
              </tr>
            </thead>
            <tbody>
              ${(desired.upstreams || []).map(upstream => `<tr>
                <td><span class="badge blue">${upstream.priority}</span></td>
                <td><code>${esc(upstream.url)}</code></td>
                <td>
                  <span class="badge ${upstream.health_status === 'healthy' ? 'ok' : 'bad'}">
                    ${esc(stateLabel(upstream.health_status))}
                  </span>
                </td>
                <td><code>${number(upstream.latency_ms)} ms</code></td>
                <td>${date(upstream.last_check)}</td>
              </tr>`).join('')}
            </tbody>
          </table>
        </div>
      </div>`;

    // Bind playground interactions
    const updatePlaygroundView = () => {
      const vId = $('#playground-variant')?.value || selectedVariant;
      const format = $('#playground-format')?.value || selectedFormat;
      const bUrl = $('#playground-url')?.value.trim() || playgroundData.baseUrl;
      const out = computePlaygroundOutput(desired.type, vId, bUrl, format, desired.slug);

      const cliPre = $('#playground-cli-pre');
      const filePre = $('#playground-file-pre');
      const filePathSpan = $('#playground-file-path');

      if (cliPre) cliPre.textContent = out.cliCmd;
      if (filePre) filePre.textContent = out.fileContent;
      if (filePathSpan) filePathSpan.textContent = out.filePath || out.fileName;

      $('#copy-cli-btn')?.replaceWith($('#copy-cli-btn').cloneNode(true));
      $('#copy-file-btn')?.replaceWith($('#copy-file-btn').cloneNode(true));
      $('#download-file-btn')?.replaceWith($('#download-file-btn').cloneNode(true));

      $('#copy-cli-btn')?.addEventListener('click', async () => {
        await copyText(out.cliCmd);
        notice(L('CLI command copied.'));
      });
      $('#copy-file-btn')?.addEventListener('click', async () => {
        await copyText(out.fileContent);
        notice(L('Configuration file copied.'));
      });
      $('#download-file-btn')?.addEventListener('click', () => {
        triggerDownload(out.fileName, out.fileContent);
        notice(L('Downloaded %s', out.fileName));
      });
    };

    $('#playground-variant')?.addEventListener('change', updatePlaygroundView);
    $('#playground-format')?.addEventListener('change', updatePlaygroundView);
    $('#playground-url')?.addEventListener('input', updatePlaygroundView);
    updatePlaygroundView();

    $('#detail-dialog').showModal();
  } catch (error) {
    notice(error.message, true);
  }
}

export function initMirrorDetail() {
  $('#close-detail')?.addEventListener('click', () => $('#detail-dialog')?.close());
}

registerAction('show-repository', button => showRepository(Number(button.dataset.id)));

registerAction('preview-repository-config', async button => {
  try {
    const value = await api(`/mirrors/${Number(button.dataset.id)}/config`);
    showPreview(L('Generated repository configuration'), `<pre class="config-preview">${esc(value.configuration)}</pre>`);
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('preview-profile-upgrade', async button => {
  const id = Number(button.dataset.id);
  const name = button.dataset.name, version = button.dataset.version;
  try {
    const value = await api(`/mirrors/${id}/profile/preview`, {method: 'POST', body: JSON.stringify({name, version})});
    const rows = Object.entries(value.diff || {}).map(([field, change]) => `<tr><td>${esc(field)}</td><td><code>${esc(JSON.stringify(change.before))}</code></td><td><code>${esc(JSON.stringify(change.after))}</code></td></tr>`).join('');
    showPreview(L('Profile upgrade preview'), `<div class="table-wrap"><table><thead><tr><th>${L('Field')}</th><th>${L('Before')}</th><th>${L('After')}</th></tr></thead><tbody>${rows}</tbody></table></div><div class="toolbar end"><button id="apply-profile-upgrade" class="btn-primary requires-operator">${L('Apply upgrade')}</button></div><pre class="config-preview">${esc(value.configuration)}</pre>`);
    $('#apply-profile-upgrade').addEventListener('click', async () => {
      try {
        await api(`/mirrors/${id}/profile/apply`, {method: 'POST', body: JSON.stringify({name, version})});
        $('#preview-dialog').close();
        $('#detail-dialog').close();
        notice(L('Profile upgrade activated.'));
        await loadMirrors();
      } catch (error) {
        notice(error.message, true);
      }
    });
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('purge-repository', async button => {
  const id = Number(button.dataset.id);
  const path = prompt(L('Optional object path. Leave empty to purge the whole repository cache.'), '');
  if (path === null) return;
  try {
    const result = path ? await api(`/mirrors/${id}/cache/purge`, {method: 'POST', body: JSON.stringify({path, query: ''})}) : await api(`/mirrors/${id}/cache`, {method: 'DELETE'});
    notice(L('Logical purge completed; physical reclaim: %s.', result.physical_reclaim));
  } catch (error) {
    notice(error.message, true);
  }
});
