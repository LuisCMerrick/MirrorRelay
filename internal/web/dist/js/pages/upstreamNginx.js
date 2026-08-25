// Managed Upstream Nginx page: concise status and history, with runtime,
// build and effective-configuration details available on demand.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { card, disclosure, kv, showPreview } from '../components.js';
import { $, copyText, esc, notice } from '../dom.js';
import { date, duration, exitSummary, stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';
import { state } from '../state.js';
import { loadMirrors } from './mirrors.js';

function normalizedVersion(value) {
  return (value || '—').replace(/^nginx version:\s*/, '');
}

function hasTimestamp(value) {
  return Boolean(value) && !String(value).startsWith('0001-01-01');
}

function technicalDetails(status) {
  return `
    <section class="preview-section">
      <h3>${L('Process')}</h3>
      ${kv(L('State'), stateLabel(status.state))}
      ${kv(L('Mode'), status.mode || '—')}
      ${kv('PID', status.pid || '—')}
      ${kv(L('Started at'), hasTimestamp(status.started_at) ? date(status.started_at) : '—')}
      ${kv(L('Uptime'), duration(status.uptime_seconds || 0))}
      ${kv(L('Last exit'), hasTimestamp(status.last_exit_at) ? exitSummary(status) : '—')}
    </section>
    <section class="preview-section">
      <h3>${L('Binary')}</h3>
      ${kv(L('Managed Upstream Nginx version'), normalizedVersion(status.version))}
      ${kv(L('Build ID'), status.build_id || '—')}
      ${kv(L('Architecture'), status.architecture || '—')}
      ${kv('SHA-256', status.sha256 || '—')}
    </section>
    <section class="preview-section">
      <h3>${L('Configuration lifecycle')}</h3>
      ${kv(L('Config version'), `v${status.current_config_version || '—'}`)}
      ${kv(L('Configuration hash'), status.current_config_hash || '—')}
      ${kv(L('Last reload'), hasTimestamp(status.last_reload) ? date(status.last_reload) : '—')}
      ${kv(L('Reload result'), status.last_reload_result || '—')}
    </section>
    <section class="preview-section">
      <h3>${L('Nginx compile options')}</h3>
      <pre class="config-preview">${esc(status.build_options || L('Build options unavailable.'))}</pre>
    </section>`;
}

export async function loadUpstreamNginx() {
  try {
    const [status, history] = await Promise.all([
      api('/upstream-nginx/status'),
      api('/upstream-nginx/history')
    ]);

    const isRunning = status.state === 'running';
    const canManage = state.role === 'admin' || state.role === 'operator';
    const canRevealConfiguration = state.role === 'admin';
    const reloadAction = canManage
      ? `<button id="reload-upstream-nginx" class="btn-primary">
          ${icon('refresh', 14)} ${L('Regenerate, validate and reload')}
        </button>`
      : '';
    const effectiveConfiguration = canRevealConfiguration
      ? disclosure(
          L('Effective configuration'),
          `<div class="panel-header-row">
            <p class="muted">${L('Loaded only when this section is opened.')}</p>
            <button id="copy-upstream-config" class="secondary small" type="button" disabled>
              ${icon('copy', 13)} ${L('Copy configuration')}
            </button>
          </div>
          <div id="upstream-config-status" class="muted" role="status">${L('Loading configuration…')}</div>
          <pre id="upstream-config-preview" class="config-preview hidden"></pre>`,
          {
            id: 'upstream-config-disclosure',
            iconName: 'code',
            description: L('Hidden by default. Expand to inspect and copy.')
          }
        )
      : '';

    $('#page-upstream-nginx').innerHTML = `
      <div class="cards compact-cards">
        ${card(L('State'), stateLabel(status.state), isRunning, 'server')}
        ${card(L('Uptime'), duration(status.uptime_seconds || 0), false, 'activity')}
        ${card(L('Config version'), `v${status.current_config_version || '—'}`, false, 'code')}
      </div>
      ${status.last_error ? `<div class="notice error">${icon('alert', 16)} ${esc(status.last_error)}</div>` : ''}
      <div class="toolbar">
        <span class="muted">${L('Last reload')}: ${hasTimestamp(status.last_reload) ? date(status.last_reload) : '—'}</span>
        <div class="actions">
          <button id="upstream-technical-details" class="secondary" type="button">
            ${icon('cpu', 14)} ${L('Technical details')}
          </button>
          ${reloadAction}
        </div>
      </div>
      <div class="panel">
        <h2>${icon('shield', 18)} ${L('Configuration history')}</h2>
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>${L('Version')}</th>
                <th>${L('Time')}</th>
                <th>${L('Triggered by')}</th>
                <th>${L('Description')}</th>
                <th>${L('State')}</th>
                ${canManage ? '<th></th>' : ''}
              </tr>
            </thead>
            <tbody>
              ${history.map(item => `<tr>
                <td><code>v${item.version}</code></td>
                <td>${date(item.created_at)}</td>
                <td><span class="badge blue">${esc(item.operator)}</span></td>
                <td>${esc(item.description)}</td>
                <td><span class="badge ${item.active ? 'ok' : ''}">${item.active ? L('Active') : L('History')}</span></td>
                ${canManage ? `<td>${item.active ? '' : `<button class="small secondary" data-action="rollback-config" data-version="${item.version}">${icon('refresh', 12)} ${L('Rollback')}</button>`}</td>` : ''}
              </tr>`).join('') || `<tr><td colspan="${canManage ? 6 : 5}" class="empty">${L('No configuration history yet.')}</td></tr>`}
            </tbody>
          </table>
        </div>
      </div>
      ${effectiveConfiguration}`;

    $('#upstream-technical-details')?.addEventListener('click', () => {
      showPreview(L('Managed Upstream Nginx technical details'), technicalDetails(status));
    });

    $('#reload-upstream-nginx')?.addEventListener('click', async () => {
      try {
        await api('/upstream-nginx/reload', {method: 'POST'});
        notice(L('Validation passed and Managed Upstream Nginx reloaded.'));
        await loadUpstreamNginx();
      } catch (error) {
        notice(error.message, true);
      }
    });

    const configDisclosure = $('#upstream-config-disclosure');
    let effectiveConfig = '';
    configDisclosure?.addEventListener('toggle', async () => {
      if (!configDisclosure.open || configDisclosure.dataset.loaded === 'true') return;
      configDisclosure.dataset.loaded = 'true';
      $('#upstream-config-status').textContent = L('Loading configuration…');
      $('#upstream-config-status').classList.remove('hidden', 'error');
      try {
        const value = await api('/upstream-nginx/config');
        effectiveConfig = value.configuration || '';
        $('#upstream-config-preview').textContent = effectiveConfig;
        $('#upstream-config-preview').classList.remove('hidden');
        $('#upstream-config-status').classList.add('hidden');
        $('#copy-upstream-config').disabled = false;
      } catch (error) {
        configDisclosure.dataset.loaded = 'false';
        $('#upstream-config-status').textContent = error.message;
        $('#upstream-config-status').classList.add('error');
      }
    });

    $('#copy-upstream-config')?.addEventListener('click', async () => {
      try {
        await copyText(effectiveConfig);
        notice(L('Configuration copied.'));
      } catch (error) {
        notice(error.message, true);
      }
    });
  } catch (error) {
    $('#page-upstream-nginx').innerHTML = `<div class="notice error">${esc(error.message)}</div>`;
  }
}

registerAction('rollback-config', async button => {
  const version = Number(button.dataset.version);
  if (!confirm(L('Rollback repositories and custom configuration to v%s?', version))) return;
  try {
    await api(`/upstream-nginx/history/${version}/rollback`, {method: 'POST'});
    notice(L('Rolled back through a validated graceful reload.'));
    await Promise.all([loadUpstreamNginx(), loadMirrors()]);
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('view-effective-config', async () => {
  try {
    const value = await api('/upstream-nginx/config');
    showPreview(L('Effective Managed Upstream Nginx configuration'), `
      <div class="panel-header-row">
        <span class="muted">${L('Effective configuration')}</span>
        <button class="secondary small" data-action="copy-effective-config">${icon('copy', 13)} ${L('Copy configuration')}</button>
      </div>
      <pre class="config-preview">${esc(value.configuration)}</pre>`);
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('copy-effective-config', async () => {
  try {
    const value = await api('/upstream-nginx/config');
    await copyText(value.configuration || '');
    notice(L('Configuration copied.'));
  } catch (error) {
    notice(error.message, true);
  }
});
