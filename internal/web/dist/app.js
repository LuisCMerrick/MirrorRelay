'use strict';

let csrf = '';
let mirrors = [];
let profiles = [];
let customConfigs = [];
let signedIn = false;
let currentPage = 'dashboard';

const $ = selector => document.querySelector(selector);
const esc = value => String(value ?? '').replace(/[&<>"']/g, character => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[character]));
const storedLanguage = localStorage.getItem('mirrorrelay.language');
let language = storedLanguage === 'zh' || storedLanguage === 'en'
  ? storedLanguage
  : ((navigator.languages || [navigator.language]).some(value => /^zh(?:-|$)/i.test(value || '')) ? 'zh' : 'en');

function getLocale(lang = language) {
  const locales = window.MIRRORRELAY_LOCALES || {};
  return locales[lang] || locales.en || {};
}

function L(english, ...args) {
  const loc = getLocale();
  let str;
  if (loc.strings && english in loc.strings) {
    str = loc.strings[english];
  } else {
    const enLoc = getLocale('en');
    if (enLoc && enLoc.strings && english in enLoc.strings) {
      str = enLoc.strings[english];
    } else {
      str = english;
    }
  }
  for (const arg of args) {
    str = str.replace('%s', arg);
  }
  return str;
}

function Lf(key, ...args) {
  let str = L(key);
  for (const arg of args) {
    str = str.replace('%s', arg);
  }
  return str;
}

function t(key, fallback) {
  return L(key);
}

function applyLanguage(next, persist = false) {
  language = next === 'zh' ? 'zh' : 'en';
  if (persist) localStorage.setItem('mirrorrelay.language', language);
  document.documentElement.lang = language === 'zh' ? 'zh-CN' : 'en';
  const loc = getLocale();
  const dict = loc.dictionary || {};
  document.querySelectorAll('[data-i18n]').forEach(element => {
    const value = dict[element.dataset.i18n];
    if (value) element.textContent = value;
  });
  document.querySelectorAll('.language-switch button').forEach(button => button.classList.toggle('active', button.dataset.lang === language));
  updatePageHeading();
  if (signedIn) {
    void (async () => {
      try {
        await loadProfilesData();
        await renderCurrentPage();
      } catch (error) { notice(error.message, true); }
    })();
  }
}

document.querySelectorAll('.language-switch button').forEach(button => button.addEventListener('click', () => applyLanguage(button.dataset.lang, true)));

async function api(path, options = {}) {
  const request = {...options, headers: {...(options.headers || {})}};
  if (request.body) request.headers['Content-Type'] = 'application/json';
  if (csrf && request.method && !['GET', 'HEAD'].includes(request.method)) request.headers['X-CSRF-Token'] = csrf;
  const response = await fetch('api/v1' + path, request);
  let body = null;
  try { body = await response.json(); } catch (_) {}
  if (!response.ok) throw new Error(body?.error || `HTTP ${response.status}`);
  return body;
}

function notice(message, bad = false) {
  $('#notice').innerHTML = `<div class="notice${bad ? ' error' : ''}">${esc(message)}</div>`;
  setTimeout(() => { $('#notice').innerHTML = ''; }, 4500);
}

function locale() { return getLocale().locale || (language === 'zh' ? 'zh-CN' : 'en-US'); }
function number(value = 0) { return new Intl.NumberFormat(locale()).format(value); }
function date(value) { return value ? new Date(value).toLocaleString(locale()) : '—'; }
function bytes(value = 0) {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let index = 0, amount = Number(value) || 0;
  while (amount >= 1024 && index < units.length - 1) { amount /= 1024; index++; }
  return `${amount.toFixed(index ? 2 : 0)} ${units[index]}`;
}
function duration(seconds = 0) {
  const days = Math.floor(seconds / 86400), hours = Math.floor(seconds % 86400 / 3600), minutes = Math.floor(seconds % 3600 / 60);
  const loc = getLocale();
  if (typeof loc.duration === 'function') return loc.duration(days, hours, minutes);
  return `${days}d ${hours}h ${minutes}m`;
}
function stateLabel(value) {
  const loc = getLocale();
  const key = String(value || '').toLowerCase();
  if (loc.stateLabels && key in loc.stateLabels) return loc.stateLabels[key];
  return String(value || '—');
}
function exitSummary(status = {}) {
  if (!status.last_exit_at) return '—';
  const loc = getLocale();
  if (typeof loc.exitSummary === 'function') {
    return loc.exitSummary(date(status.last_exit_at), status.last_exit_code, status.last_exit_reason);
  }
  const code = status.last_exit_code === -1
    ? L('exit code unknown')
    : `exit code ${status.last_exit_code ?? '—'}`;
  return `${date(status.last_exit_at)} · ${code} · ${status.last_exit_reason || ''}`;
}
function publicURL(repository) {
  return repository.public_mode === 'host' ? `https://${repository.public_host}/` : `${location.origin}${repository.public_path}`;
}

async function boot() {
  let session;
  try {
    session = await api('/auth/session');
  } catch (_) {
    signedIn = false;
    $('#app').classList.add('hidden');
    $('#login').classList.remove('hidden');
    return;
  }
  csrf = session.csrf_token;
  signedIn = true;
  $('#user-name').textContent = session.username;
  $('#login').classList.add('hidden');
  $('#app').classList.remove('hidden');
  try {
    await loadProfilesData();
    await Promise.all([loadDashboard(), loadMirrors()]);
  } catch (error) {
    notice(error.message, true);
  }
}

$('#login-form').addEventListener('submit', async event => {
  event.preventDefault();
  $('#login-error').textContent = '';
  try {
    const session = await api('/auth/login', {method: 'POST', body: JSON.stringify({username: $('#login-user').value, password: $('#login-password').value})});
    csrf = session.csrf_token;
    await boot();
  } catch (error) { $('#login-error').textContent = error.message; }
});
$('#logout').addEventListener('click', async () => {
  try { await api('/auth/logout', {method: 'POST'}); } catch (_) {}
  csrf = ''; location.reload();
});

function updatePageHeading() {
  const loc = getLocale();
  const pageMeta = loc.pageMeta || {};
  const metadata = pageMeta[currentPage] || pageMeta.dashboard || ['Dashboard', 'Live service status'];
  $('#page-title').textContent = metadata[0];
  $('#page-subtitle').textContent = metadata[1];
}

document.querySelectorAll('nav button').forEach(button => button.addEventListener('click', async () => {
  document.querySelectorAll('nav button').forEach(candidate => candidate.classList.toggle('active', candidate === button));
  document.querySelectorAll('.page').forEach(page => page.classList.add('hidden'));
  currentPage = button.dataset.page;
  $('#page-' + currentPage).classList.remove('hidden');
  updatePageHeading();
  await renderCurrentPage();
}));

async function renderCurrentPage() {
  const loaders = {dashboard: loadDashboard, mirrors: loadMirrors, profiles: loadProfiles, 'upstream-nginx': loadUpstreamNginx, custom: loadCustom, ingress: loadIngress, cluster: loadCluster, cache: loadCache, health: loadHealth, access: loadAccess, audit: loadAudit, system: loadSystem, settings: loadSettings, users: loadUsers, account: loadAccount};
  try { await (loaders[currentPage] || loadDashboard)(); } catch (error) { notice(error.message, true); }
}

function activeUpstreamFor(repository) {
  const healthRank = value => value === 'healthy' ? 0 : (!value || value === 'unknown') ? 1 : 2;
  return [...(repository.upstreams || [])].filter(value => value.enabled).sort((a, b) => healthRank(a.health_status) - healthRank(b.health_status) || a.priority - b.priority)[0] || {};
}
function healthFor(repository) {
  if (!repository.enabled) return 'disabled';
  const enabled = (repository.upstreams || []).filter(value => value.enabled);
  if (enabled.some(value => value.health_status === 'healthy')) return 'healthy';
  if (!enabled.length || enabled.some(value => !value.health_status || value.health_status === 'unknown')) return 'unknown';
  return 'unhealthy';
}

async function loadDashboard() {
  const [dashboard, upstreamNginx, repositoryValues] = await Promise.all([api('/stats'), api('/upstream-nginx/status'), api('/mirrors')]);
  const repositories = repositoryValues || [];
  mirrors = repositories;
  const today = dashboard.stats.today, last24 = dashboard.stats.last_24_hours, last7 = dashboard.stats.last_7_days, cache = dashboard.cache;
  const denominator = today.cache_hits + today.cache_misses;
  const hitRate = denominator ? 100 * today.cache_hits / denominator : 0;
  const maximum = cache.maximum_bytes || cache.max_bytes || 0;
  $('#status').textContent = `${L('Managed Upstream Nginx')} ${stateLabel(upstreamNginx.state)}`;
  $('#status').className = `status ${upstreamNginx.state === 'running' ? 'online' : ''}`;
  const perRepository = dashboard.stats.by_mirror || {};
  const repositoryRows = repositories.map(repository => {
    const counters = perRepository[repository.id] || {};
    const upstream = activeUpstreamFor(repository);
    const health = healthFor(repository);
    return `<tr><td><button class="text-button" data-action="show-repository" data-id="${repository.id}"><strong>${esc(repository.name)}</strong></button></td><td><span class="badge ${health === 'healthy' ? 'ok' : health === 'unhealthy' ? 'bad' : ''}">${esc(stateLabel(health))}</span></td><td>${number(counters.requests || 0)}</td><td>${bytes(counters.bytes || 0)}</td><td>${upstream.latency_ms ? `${number(upstream.latency_ms)} ms` : '—'}</td><td>${number(counters.cache_hits || 0)} / ${number(counters.cache_misses || 0)}</td><td>${number(counters.status_2xx || 0)} / ${number(counters.status_3xx || 0)} / ${number(counters.status_4xx || 0)} / ${number(counters.status_5xx || 0)}</td><td>${number(counters.upstream_errors || 0)}</td></tr>`;
  }).join('');
  $('#page-dashboard').innerHTML = `<div class="cards">
    ${card(L('Repositories / enabled'), `${dashboard.mirrors} / ${dashboard.enabled_mirrors}`)}
    ${card(L('Healthy / unhealthy'), `${dashboard.healthy_mirrors || 0} / ${dashboard.unhealthy_mirrors || 0}`, dashboard.unhealthy_mirrors === 0)}
    ${card(L('Managed Upstream Nginx'), stateLabel(upstreamNginx.state), upstreamNginx.state === 'running')}
    ${card(L('Active requests'), number(dashboard.stats.active_requests))}
    ${card(L('Requests today'), number(today.requests))}
    ${card(L('Traffic today'), bytes(today.bytes))}
    ${card(L('Traffic / 24 h'), bytes(last24.bytes))}
    ${card(L('Traffic / 7 d'), bytes(last7.bytes))}
    ${card(L('Cache hit rate'), `${hitRate.toFixed(1)}%`)}
  </div><div class="grid2"><div class="panel"><h2>${L('Cache usage')}</h2>
    <div class="bar-row"><span>${number(cache.files)} ${L('files')}</span><div class="bar"><i style="width:${maximum ? Math.min(100, 100 * cache.bytes / maximum) : 0}%"></i></div><span>${maximum ? (100 * cache.bytes / maximum).toFixed(1) : '0.0'}%</span></div>
    <p class="muted">${bytes(cache.bytes)} / ${bytes(maximum)}</p></div>
    <div class="panel"><h2>${L('MirrorRelay and Managed Upstream Nginx')}</h2>${kv(L('Managed Upstream Nginx PID'), upstreamNginx.pid || '—')}${kv(L('Managed Upstream Nginx version'), upstreamNginx.version || '—')}${kv(L('Managed Upstream Nginx build ID'), upstreamNginx.build_id || '—')}${kv(L('Managed Upstream Nginx architecture'), upstreamNginx.architecture || '—')}${kv(L('Managed Upstream Nginx uptime'), duration(upstreamNginx.uptime_seconds || 0))}${kv(L('MirrorRelay version'), dashboard.version || '—')}${kv(L('MirrorRelay build ID'), dashboard.build_id || '—')}${kv(L('MirrorRelay architecture'), dashboard.architecture || '—')}${kv(L('Active config'), `v${upstreamNginx.current_config_version || '—'}`)}${kv(L('MirrorRelay uptime'), duration(dashboard.uptime_seconds))}</div></div>
    <div class="panel"><h2>${L('Repository statistics today')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Repository')}</th><th>${L('Health')}</th><th>${L('Requests')}</th><th>${L('Traffic')}</th><th>${L('Latency')}</th><th>${L('Cache HIT / MISS')}</th><th>2xx / 3xx / 4xx / 5xx</th><th>${L('Upstream errors')}</th></tr></thead><tbody>${repositoryRows || `<tr><td colspan="8" class="empty">${L('No repositories yet.')}</td></tr>`}</tbody></table></div></div>`;
}
function card(label, value, accent = false) { return `<div class="card"><small>${esc(label)}</small><strong class="${accent ? 'accent' : ''}">${esc(value)}</strong></div>`; }
function kv(label, value) { return `<div class="kv"><span>${esc(label)}</span><span>${esc(value)}</span></div>`; }

async function loadProfilesData() {
  profiles = (await api('/profiles')) || [];
  $('#template').innerHTML = `<option value="">${L('Custom')}</option>` + profiles.map((profile, index) => `<option value="${index}">${esc(profile.name)} · ${esc(profile.version)}${profile.latest_stable ? ` · ${L('latest')}` : ''}</option>`).join('');
}

async function loadProfiles() {
  if (!profiles.length) await loadProfilesData();
  $('#page-profiles').innerHTML = `<div class="panel"><p class="muted">${L('Profiles are versioned defaults. Every field remains editable after applying a profile, and existing repositories stay pinned until an explicit upgrade.')}</p></div>
  <div class="table-wrap"><table><thead><tr><th>${L('Profile')}</th><th>${L('Version')}</th><th>${L('Type')}</th><th>${L('Upstream')}</th><th>${L('Mode')}</th><th>${L('Cache / rewrite')}</th></tr></thead><tbody>
  ${profiles.map(profile => `<tr><td><strong>${esc(profile.name)}</strong>${profile.latest_stable ? ` <span class="badge ok">${L('Latest stable')}</span>` : ''}</td><td>${esc(profile.version)}</td><td>${esc(profile.type)}</td><td><code>${esc(profile.upstream)}</code></td><td>${esc(profile.public_mode)} / ${esc(profile.proxy_mode)}</td><td>${profile.cache_enabled ? L('Cache') : '—'} ${profile.rewrite_enabled ? `· ${L('Rewrite')}` : ''}</td></tr>`).join('')}</tbody></table></div>`;
}

async function loadMirrors() {
  mirrors = (await api('/mirrors')) || [];
  const rows = mirrors.map(repository => {
    const active = activeUpstreamFor(repository);
    const health = healthFor(repository);
    return `<tr><td><button class="text-button" data-action="show-repository" data-id="${repository.id}"><strong>${esc(repository.name)}</strong></button><br><small>${esc(repository.slug)}</small></td>
      <td>${esc(repository.type)}<br><small>${esc(repository.profile_name || 'Custom')} ${esc(repository.profile_version || '')}</small></td>
      <td><code>${esc(publicURL(repository))}</code></td><td title="${esc(active.url || '')}">${esc((active.url || '').replace(/^https?:\/\//, '').slice(0, 42) || '—')}</td>
      <td><span class="badge ${health === 'healthy' ? 'ok' : health === 'unhealthy' ? 'bad' : ''}">${esc(stateLabel(health))}</span><br><small>${active.latency_ms ? `${number(active.latency_ms)} ms` : '—'}</small></td>
      <td><span class="badge ${repository.config_state === 'active' ? 'ok' : repository.config_state === 'failed' ? 'bad' : ''}" title="${esc(repository.config_error || '')}">${esc(stateLabel(repository.config_state))}</span></td>
      <td>${repository.cache_enabled ? `<span class="badge ok">${esc(repository.cache_profile)}</span>` : stateLabel('disabled')}</td>
      <td class="actions"><button data-action="show-repository" data-id="${repository.id}">${L('Details')}</button><button data-action="copy-repository-url" data-id="${repository.id}">${L('Copy URL')}</button><button data-action="check-mirror" data-id="${repository.id}">${L('Test')}</button><button data-action="preview-repository-config" data-id="${repository.id}">${L('Config')}</button><button data-action="purge-repository" data-id="${repository.id}">${L('Purge')}</button><button data-action="edit-mirror" data-id="${repository.id}">${L('Edit')}</button><button data-action="toggle-mirror" data-id="${repository.id}" data-enabled="${!repository.enabled}">${repository.enabled ? L('Disable') : L('Enable')}</button><button class="danger" data-action="delete-mirror" data-id="${repository.id}">${L('Delete')}</button></td></tr>`;
  }).join('');
  $('#mirror-list').innerHTML = `<div class="table-wrap"><table><thead><tr><th>${L('Name')}</th><th>${L('Type / profile')}</th><th>${L('Public URL')}</th><th>${L('Active upstream')}</th><th>${L('Health / latency')}</th><th>${L('Desired state')}</th><th>${L('Cache')}</th><th>${L('Actions')}</th></tr></thead><tbody>${rows || `<tr><td colspan="8" class="empty">${L('No repositories yet.')}</td></tr>`}</tbody></table></div>`;
}

$('#add-mirror').addEventListener('click', () => openMirrorForm());
$('#close-dialog').addEventListener('click', () => $('#mirror-dialog').close());
$('#cancel-dialog').addEventListener('click', () => $('#mirror-dialog').close());
$('#close-detail').addEventListener('click', () => $('#detail-dialog').close());
$('#close-preview').addEventListener('click', () => $('#preview-dialog').close());

$('#template').addEventListener('change', () => {
	const selected = $('#template').value;
	const profile = selected === '' ? null : profiles[Number(selected)];
  if (!profile) return;
  $('#mirror-name').value = profile.name;
  $('#mirror-slug').value = profile.name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
  $('#repository-type').value = profile.type;
  $('#proxy-mode').value = profile.proxy_mode;
  $('#public-mode').value = profile.public_mode;
  $('#upstream-list').value = `100 ${profile.upstream}`;
  $('#health-path').value = profile.health_path || '';
  $('#cache-enabled').checked = Boolean(profile.cache_enabled);
  $('#cache-profile').value = profile.cache_profile || 'standard';
  $('#cache-authenticated').checked = Boolean(profile.cache_authenticated);
  $('#rewrite-enabled').checked = Boolean(profile.rewrite_enabled);
  $('#html-rewrite-enabled').checked = Boolean(profile.html_rewrite_enabled);
  $('#rewrite-profile').value = profile.rewrite_profile || '';
  $('#rewrite-hosts').value = (profile.rewrite_hosts || []).join('\n');
  $('#auth-mode').value = profile.auth_mode || 'direct';
  $('#blob-redirect').value = profile.blob_redirect_mode || 'full_proxy';
  $('#connect-timeout').value = profile.connect_timeout_sec || 10;
  $('#read-timeout').value = profile.read_timeout_sec || 3600;
  $('#send-timeout').value = profile.send_timeout_sec || 3600;
  $('#metadata-limit').value = profile.metadata_rewrite_limit_bytes || 0;
  $('#metadata-ttl').value = profile.metadata_ttl_sec || 0;
  $('#package-ttl').value = profile.package_ttl_sec || 0;
  $('#immutable-ttl').value = profile.immutable_ttl_sec || 0;
  $('#blob-ttl').value = profile.blob_ttl_sec || 0;
});

function openMirrorForm(repository = null) {
  $('#mirror-form').reset();
  const set = (selector, value) => { $(selector).value = value ?? ''; };
  set('#mirror-id', repository?.id || '');
  $('#form-title').textContent = repository ? L('Edit repository') : L('Add repository');
  set('#mirror-name', repository?.name); set('#mirror-slug', repository?.slug); set('#repository-type', repository?.type || 'generic');
  set('#public-mode', repository?.public_mode || 'path'); set('#public-host', repository?.public_host); set('#public-path', repository?.public_path); set('#access-policy', repository?.access_policy || 'public');
  set('#mirror-description', repository?.description); set('#proxy-mode', repository?.proxy_mode || 'transparent'); set('#redirect-mode', repository?.redirect_mode || 'rewrite');
  set('#upstream-list', (repository?.upstreams || []).map(upstream => `${upstream.priority || 100} ${upstream.url}`).join('\n'));
  set('#strip-prefix', repository?.strip_prefix); set('#add-prefix', repository?.add_prefix); set('#host-rewrite', repository?.host_rewrite);
  set('#header-add', Object.entries(repository?.header_add || {}).map(([name, value]) => `${name}: ${value}`).join('\n'));
  set('#header-remove', (repository?.header_remove || []).join('\n'));
  set('#connect-timeout', repository?.connect_timeout_sec || 10); set('#read-timeout', repository?.read_timeout_sec || 3600); set('#send-timeout', repository?.send_timeout_sec || 3600);
  set('#cache-profile', repository?.cache_profile || 'standard'); set('#rewrite-profile', repository?.rewrite_profile || ''); set('#rewrite-hosts', (repository?.rewrite_hosts || []).join('\n'));
  set('#metadata-limit', repository?.metadata_rewrite_limit_bytes || 0); set('#metadata-ttl', repository?.metadata_ttl_sec || 0); set('#package-ttl', repository?.package_ttl_sec || 0);
  set('#immutable-ttl', repository?.immutable_ttl_sec || 0); set('#blob-ttl', repository?.blob_ttl_sec || 0);
  set('#health-path', repository?.health_check_path); set('#health-expected', repository?.health_expected || 200); set('#health-interval', repository?.health_interval_sec || 60);
  set('#health-timeout', repository?.health_timeout_sec || 5); set('#health-method', repository?.health_method || 'HEAD'); set('#rate-profile', repository?.rate_limit_profile || '');
  set('#max-concurrency', repository?.max_concurrency || 0); set('#bandwidth-limit', repository?.bandwidth_limit_bps || 0);
  set('#auth-mode', repository?.auth_mode || 'direct'); set('#blob-redirect', repository?.blob_redirect_mode || 'full_proxy'); set('#token-upstream', repository?.token_upstream);
  $('#mirror-enabled').checked = repository?.enabled ?? true; $('#cache-enabled').checked = repository?.cache_enabled ?? false; $('#cache-authenticated').checked = repository?.cache_authenticated ?? false;
  $('#rewrite-enabled').checked = repository?.rewrite_enabled ?? false; $('#html-rewrite-enabled').checked = repository?.html_rewrite_enabled ?? false; $('#health-enabled').checked = repository?.health_check_enabled ?? true; $('#pull-only').checked = repository?.pull_only ?? true;
  $('#allow-http').checked = repository?.allow_http_upstream ?? false; $('#allow-private').checked = repository?.allow_private_upstream ?? false;
  const profileIndex = profiles.findIndex(profile => profile.name === repository?.profile_name && profile.version === repository?.profile_version);
  $('#template').value = profileIndex >= 0 ? String(profileIndex) : '';
  $('#form-error').textContent = '';
  $('#mirror-dialog').showModal();
}

function parseList(value) { return value.split(/[\n,]+/).map(item => item.trim()).filter(Boolean); }
function parseHeaders(value) {
  const result = {};
  for (const line of value.split(/\n+/).map(item => item.trim()).filter(Boolean)) {
    const index = line.indexOf(':');
    if (index <= 0) throw new Error(L('Invalid header line: %s', line));
    result[line.slice(0, index).trim()] = line.slice(index + 1).trim();
  }
  return result;
}
function parseUpstreams(value) {
  return value.split(/\n+/).filter(line => line.trim()).map(line => {
    const match = line.trim().match(/^(\d+)\s+(https?:\/\/\S+)$/);
    if (!match) throw new Error(L('Invalid upstream line: %s', line));
    return {url: match[2], priority: Number(match[1]), weight: 1, enabled: true};
  });
}

$('#mirror-form').addEventListener('submit', async event => {
  event.preventDefault();
	const id = $('#mirror-id').value;
	const templateValue = $('#template').value;
	const selectedProfile = templateValue === '' ? null : profiles[Number(templateValue)];
  try {
    const body = {
      name: $('#mirror-name').value, slug: $('#mirror-slug').value, type: $('#repository-type').value,
      profile_name: selectedProfile?.name || 'Custom', profile_version: selectedProfile?.version || '1.0.0',
      enabled: $('#mirror-enabled').checked, description: $('#mirror-description').value,
      public_mode: $('#public-mode').value, public_host: $('#public-host').value, public_path: $('#public-path').value,
      access_policy: $('#access-policy').value, proxy_mode: $('#proxy-mode').value, redirect_mode: $('#redirect-mode').value,
      upstreams: parseUpstreams($('#upstream-list').value), strip_prefix: $('#strip-prefix').value, add_prefix: $('#add-prefix').value, host_rewrite: $('#host-rewrite').value,
      header_add: parseHeaders($('#header-add').value), header_remove: parseList($('#header-remove').value),
      connect_timeout_sec: Number($('#connect-timeout').value), read_timeout_sec: Number($('#read-timeout').value), send_timeout_sec: Number($('#send-timeout').value),
      cache_enabled: $('#cache-enabled').checked, cache_profile: $('#cache-profile').value, cache_authenticated: $('#cache-authenticated').checked,
      rewrite_enabled: $('#rewrite-enabled').checked, html_rewrite_enabled: $('#html-rewrite-enabled').checked, rewrite_profile: $('#rewrite-profile').value, rewrite_hosts: parseList($('#rewrite-hosts').value),
      metadata_rewrite_limit_bytes: Number($('#metadata-limit').value), metadata_ttl_sec: Number($('#metadata-ttl').value), package_ttl_sec: Number($('#package-ttl').value),
      immutable_ttl_sec: Number($('#immutable-ttl').value), blob_ttl_sec: Number($('#blob-ttl').value),
      health_check_enabled: $('#health-enabled').checked, health_check_path: $('#health-path').value, health_interval_sec: Number($('#health-interval').value),
      health_timeout_sec: Number($('#health-timeout').value), health_method: $('#health-method').value, health_expected: Number($('#health-expected').value),
      rate_limit_profile: $('#rate-profile').value, max_concurrency: Number($('#max-concurrency').value), bandwidth_limit_bps: Number($('#bandwidth-limit').value),
      auth_mode: $('#auth-mode').value, token_upstream: $('#token-upstream').value, blob_redirect_mode: $('#blob-redirect').value, pull_only: $('#pull-only').checked,
      allow_http_upstream: $('#allow-http').checked, allow_private_upstream: $('#allow-private').checked, insecure_skip_verify: false
    };
    await api(id ? `/mirrors/${id}` : '/mirrors', {method: id ? 'PUT' : 'POST', body: JSON.stringify(body)});
    $('#mirror-dialog').close();
    notice(L('Candidate validated and activated with a graceful reload.'));
    await Promise.all([loadMirrors(), loadDashboard()]);
  } catch (error) { $('#form-error').textContent = error.message; }
});

window.editMirror = id => openMirrorForm(mirrors.find(repository => repository.id === id));
window.copyRepositoryURL = async id => {
  const repository = mirrors.find(value => value.id === id);
  if (!repository) return;
  try { await copyText(publicURL(repository)); notice(L('Repository URL copied.')); } catch (error) { notice(error.message, true); }
};

async function copyText(value) {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const input = document.createElement('textarea');
  input.value = value;
  input.setAttribute('readonly', '');
  input.style.position = 'fixed';
  input.style.opacity = '0';
  document.body.appendChild(input);
  input.select();
  const copied = document.execCommand('copy');
  input.remove();
  if (!copied) throw new Error(L('Clipboard access is unavailable.'));
}
window.checkMirror = async id => {
  notice(L('Checking upstreams…'));
  try {
    const results = await api(`/mirrors/${id}/check`, {method: 'POST'});
    const healthy = results.length > 0 && results.every(result => result.healthy);
    notice(healthy ? L('All upstreams are healthy.') : L('One or more upstreams are unhealthy.'), !healthy);
    await loadMirrors();
  } catch (error) { notice(error.message, true); }
};
window.toggleMirror = async (id, enabled) => {
  try {
    await api(`/mirrors/${id}/${enabled ? 'enable' : 'disable'}`, {method: 'POST'});
    notice(enabled ? L('Repository enabled.') : L('Repository disabled.'));
    await loadMirrors();
  } catch (error) { notice(error.message, true); }
};
window.deleteMirror = async id => {
  if (!confirm(L('Delete this repository and logically invalidate its cache? This cannot be undone.'))) return;
  try { await api(`/mirrors/${id}`, {method: 'DELETE'}); notice(L('Repository deleted.')); await loadMirrors(); } catch (error) { notice(error.message, true); }
};

window.showRepository = async id => {
  try {
    const [state, examples] = await Promise.all([api(`/mirrors/${id}/state`), api(`/mirrors/${id}/client-config`)]);
    const desired = state.desired, active = state.active_found ? state.active : null, statistics = state.statistics || {};
    const latest = profiles.find(profile => profile.name === desired.profile_name && profile.latest_stable);
    const upgrade = latest && latest.version !== desired.profile_version ? `<button data-action="preview-profile-upgrade" data-id="${id}" data-name="${esc(latest.name)}" data-version="${esc(latest.version)}">${L('Preview upgrade to %s', latest.version)}</button>` : '';
    $('#detail-title').textContent = desired.name;
    $('#detail-content').innerHTML = `<div class="cards detail-cards">${card(L('Desired state'), stateLabel(desired.config_state), desired.config_state === 'active')}${card(L('Active state'), active ? L('Published') : L('Not active'), Boolean(active))}${card(L('Effective config'), `v${state.effective_config_version || '—'}`)}${card(L('Requests today'), number(statistics.requests || 0))}${card(L('Traffic today'), bytes(statistics.bytes || 0))}${card(L('Observed cache traffic'), bytes(statistics.cache_bytes || 0))}${card(L('Cache HIT / MISS'), `${number(statistics.cache_hits || 0)} / ${number(statistics.cache_misses || 0)}`)}${card('2xx / 3xx / 4xx / 5xx', `${number(statistics.status_2xx || 0)} / ${number(statistics.status_3xx || 0)} / ${number(statistics.status_4xx || 0)} / ${number(statistics.status_5xx || 0)}`)}${card(L('Upstream errors'), number(statistics.upstream_errors || 0))}</div>
      <div class="toolbar"><div class="actions"><button data-action="copy-repository-url" data-id="${id}">${L('Copy URL')}</button><button data-action="edit-mirror-from-detail" data-id="${id}">${L('Edit')}</button><button data-action="check-mirror" data-id="${id}">${L('Test')}</button><button data-action="preview-repository-config" data-id="${id}">${L('Preview config')}</button><button data-action="view-effective-config">${L('Effective config')}</button><button data-action="purge-repository" data-id="${id}">${L('Purge cache')}</button>${upgrade}</div></div>
      <div class="grid2"><div class="panel"><h2>${L('Desired configuration')}</h2>${repositorySummary(desired)}</div><div class="panel"><h2>${L('Active routing snapshot')}</h2>${active ? repositorySummary(active) : `<p class="muted">${L('No active version. The desired configuration may have failed validation or activation.')}</p>`}</div></div>
      <div class="panel"><h2>${L('Upstreams')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Priority')}</th><th>${L('URL')}</th><th>${L('Health')}</th><th>${L('Latency')}</th><th>${L('Last check')}</th></tr></thead><tbody>${(desired.upstreams || []).map(upstream => `<tr><td>${upstream.priority}</td><td><code>${esc(upstream.url)}</code></td><td>${esc(stateLabel(upstream.health_status))}</td><td>${number(upstream.latency_ms)} ms</td><td>${date(upstream.last_check)}</td></tr>`).join('')}</tbody></table></div></div>
      <div class="panel"><h2>${L('Client configuration examples')}</h2>${examples.map((example, index) => `<div class="example"><div class="toolbar"><strong>${esc(example.name)}</strong><button class="copy-example" data-index="${index}">${L('Copy')}</button></div><pre>${esc(example.command)}</pre></div>`).join('')}</div>`;
    $('#detail-content').querySelectorAll('.copy-example').forEach(button => button.addEventListener('click', async () => { await copyText(examples[Number(button.dataset.index)].command); notice(L('Copied.')); }));
    $('#detail-dialog').showModal();
  } catch (error) { notice(error.message, true); }
};

function repositorySummary(repository) {
  return `${kv(L('Public URL'), publicURL(repository))}${kv(L('Type / mode'), `${repository.type} / ${repository.proxy_mode}`)}${kv(L('Profile'), `${repository.profile_name || 'Custom'} ${repository.profile_version || ''}`)}${kv(L('Cache'), repository.cache_enabled ? `${repository.cache_profile} · ${repository.cache_authenticated ? L('authenticated enabled') : L('anonymous only')}` : L('Disabled'))}${kv(L('Browsable HTML URL rewrite'), repository.html_rewrite_enabled ? L('Enabled') : L('Disabled'))}${kv(L('Rewrite hosts'), (repository.rewrite_hosts || []).join(', ') || '—')}${repository.config_error ? `<div class="notice error">${esc(repository.config_error)}</div>` : ''}`;
}

window.previewRepositoryConfig = async id => {
  try {
    const value = await api(`/mirrors/${id}/config`);
    showPreview(L('Generated repository configuration'), `<pre class="config-preview">${esc(value.configuration)}</pre>`);
  } catch (error) { notice(error.message, true); }
};
window.viewEffectiveConfig = async () => {
  try {
    const value = await api('/upstream-nginx/config');
    showPreview(L('Effective Managed Upstream Nginx configuration'), `<pre class="config-preview">${esc(value.configuration)}</pre>`);
  } catch (error) { notice(error.message, true); }
};
window.previewProfileUpgrade = async (id, name, version) => {
  try {
    const value = await api(`/mirrors/${id}/profile/preview`, {method: 'POST', body: JSON.stringify({name, version})});
    const rows = Object.entries(value.diff || {}).map(([field, change]) => `<tr><td>${esc(field)}</td><td><code>${esc(JSON.stringify(change.before))}</code></td><td><code>${esc(JSON.stringify(change.after))}</code></td></tr>`).join('');
    showPreview(L('Profile upgrade preview'), `<div class="table-wrap"><table><thead><tr><th>${L('Field')}</th><th>${L('Before')}</th><th>${L('After')}</th></tr></thead><tbody>${rows}</tbody></table></div><div class="toolbar end"><button id="apply-profile-upgrade">${L('Apply upgrade')}</button></div><pre class="config-preview">${esc(value.configuration)}</pre>`);
    $('#apply-profile-upgrade').addEventListener('click', async () => {
      try { await api(`/mirrors/${id}/profile/apply`, {method: 'POST', body: JSON.stringify({name, version})}); $('#preview-dialog').close(); $('#detail-dialog').close(); notice(L('Profile upgrade activated.')); await loadMirrors(); } catch (error) { notice(error.message, true); }
    });
  } catch (error) { notice(error.message, true); }
};
window.purgeRepository = async id => {
  const path = prompt(L('Optional object path. Leave empty to purge the whole repository cache.'), '');
  if (path === null) return;
  try {
    const result = path ? await api(`/mirrors/${id}/cache/purge`, {method: 'POST', body: JSON.stringify({path, query: ''})}) : await api(`/mirrors/${id}/cache`, {method: 'DELETE'});
    notice(L('Logical purge completed; physical reclaim: %s.', result.physical_reclaim));
  } catch (error) { notice(error.message, true); }
};
function showPreview(title, content) { $('#preview-title').textContent = title; $('#preview-content').innerHTML = content; $('#preview-dialog').showModal(); }

async function loadUpstreamNginx() {
  try {
    const [status, config, history] = await Promise.all([api('/upstream-nginx/status'), api('/upstream-nginx/config'), api('/upstream-nginx/history')]);
    $('#page-upstream-nginx').innerHTML = `<div class="cards">${card(L('State'), stateLabel(status.state), status.state === 'running')}${card('PID', status.pid || '—')}${card(L('Uptime'), duration(status.uptime_seconds || 0))}${card(L('Config version'), `v${status.current_config_version || '—'}`)}${card(L('Managed Upstream Nginx version'), (status.version || '—').replace(/^nginx version:\s*/, ''))}${card(L('Build ID'), status.build_id || '—')}${card(L('Architecture'), status.architecture || '—')}</div>
      ${status.last_error ? `<div class="notice error">${esc(status.last_error)}</div>` : ''}<div class="toolbar"><div>${status.integration_snippet ? `<span class="muted">${L('Integration snippet')}: ${esc(status.integration_snippet)} · ${esc(status.integration_result || '')}</span>` : ''}</div><button id="reload-upstream-nginx">${L('Regenerate, validate and reload')}</button></div>
      <div class="grid2"><div class="panel"><h2>${L('Configuration history')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Version')}</th><th>${L('Time')}</th><th>${L('Operator')}</th><th>${L('Description')}</th><th>${L('State')}</th><th></th></tr></thead><tbody>${history.map(item => `<tr><td>v${item.version}</td><td>${date(item.created_at)}</td><td>${esc(item.operator)}</td><td>${esc(item.description)}</td><td><span class="badge ${item.active ? 'ok' : ''}">${item.active ? L('Active') : L('History')}</span></td><td>${item.active ? '' : `<button data-action="rollback-config" data-version="${item.version}">${L('Rollback')}</button>`}</td></tr>`).join('')}</tr></thead></table></div></div><div class="panel"><h2>${L('Runtime and build')}</h2>${kv(L('Last reload'), status.last_reload ? date(status.last_reload) : '—')}${kv(L('Reload result'), status.last_reload_result || '—')}${kv(L('Last exit'), exitSummary(status))}<pre class="config-preview">${esc(status.build_options || L('Build options unavailable.'))}</pre></div><div class="panel"><h2>${L('Effective configuration')}</h2><pre class="config-preview">${esc(config.configuration)}</pre></div></div>`;
    $('#reload-upstream-nginx').addEventListener('click', async () => { try { await api('/upstream-nginx/reload', {method: 'POST'}); notice(L('Validation passed and Managed Upstream Nginx reloaded.')); await loadUpstreamNginx(); } catch (error) { notice(error.message, true); } });
  } catch (error) { $('#page-upstream-nginx').innerHTML = `<div class="notice error">${esc(error.message)}</div>`; }
}
window.rollbackConfig = async version => {
  if (!confirm(L('Rollback repositories and custom configuration to v%s?', version))) return;
  try { await api(`/upstream-nginx/history/${version}/rollback`, {method: 'POST'}); notice(L('Rolled back through a validated graceful reload.')); await Promise.all([loadUpstreamNginx(), loadMirrors()]); } catch (error) { notice(error.message, true); }
};

async function loadCustom() {
  customConfigs = (await api('/custom-configs')) || [];
  $('#custom-list').innerHTML = `<div class="panel"><p class="muted">${L('These directives apply only to Managed Upstream Nginx. Dangerous process, filesystem and context-escape directives are rejected.')}</p></div><div class="table-wrap"><table><thead><tr><th>${L('Name')}</th><th>${L('Context')}</th><th>${L('Repository')}</th><th>${L('State')}</th><th>${L('Last validation')}</th><th>${L('Actions')}</th></tr></thead><tbody>${customConfigs.map(value => `<tr><td><strong>${esc(value.name)}</strong></td><td>${esc(value.context)}</td><td>${value.repository_id || L('Global')}</td><td><span class="badge ${value.enabled ? 'ok' : ''}">${value.enabled ? L('Enabled') : L('Disabled')}</span></td><td>${esc(value.last_validation_result || '—')}</td><td class="actions"><button data-action="edit-custom" data-id="${value.id}">${L('Edit')}</button><button class="danger" data-action="delete-custom" data-id="${value.id}">${L('Delete')}</button></td></tr>`).join('')}</tbody></table></div>`;
}
function openCustom(value = null) {
  $('#custom-form').reset(); $('#custom-id').value = value?.id || ''; $('#custom-title').textContent = value ? L('Edit custom Managed Upstream Nginx configuration') : L('Add custom Managed Upstream Nginx configuration');
  $('#custom-name').value = value?.name || ''; $('#custom-context').value = value?.context || 'http'; $('#custom-repository').value = value?.repository_id || 0; $('#custom-enabled').checked = value?.enabled ?? true; $('#custom-content').value = value?.content || ''; $('#custom-error').textContent = ''; $('#custom-dialog').showModal();
}
$('#add-custom').addEventListener('click', () => openCustom()); $('#close-custom').addEventListener('click', () => $('#custom-dialog').close()); $('#cancel-custom').addEventListener('click', () => $('#custom-dialog').close());
window.editCustom = id => openCustom(customConfigs.find(value => value.id === id));
window.deleteCustom = async id => { if (!confirm(L('Delete this custom configuration and reload Managed Upstream Nginx?'))) return; try { await api(`/custom-configs/${id}`, {method: 'DELETE'}); notice(L('Custom configuration deleted.')); await loadCustom(); } catch (error) { notice(error.message, true); } };
$('#custom-form').addEventListener('submit', async event => { event.preventDefault(); const id = $('#custom-id').value; const body = {name: $('#custom-name').value, context: $('#custom-context').value, repository_id: Number($('#custom-repository').value), enabled: $('#custom-enabled').checked, content: $('#custom-content').value}; try { await api(id ? `/custom-configs/${id}` : '/custom-configs', {method: id ? 'PUT' : 'POST', body: JSON.stringify(body)}); $('#custom-dialog').close(); notice(L('Custom configuration validated and activated.')); await loadCustom(); } catch (error) { $('#custom-error').textContent = error.message; } });

async function loadIngress() {
  const integration = await api('/ingress/snippet');
  $('#page-ingress').innerHTML = `<div class="cards">${card(L('Ingress mode'), integration.mode)}${card(L('Frontend network'), integration.frontend_network)}${card(L('Frontend address'), integration.frontend_address)}</div><div class="panel"><h2>${L('External Shared Nginx integration snippet')}</h2><p class="muted">${L('The generated file is a scoped deployment aid. MirrorRelay does not own or reload the External Shared Nginx process.')}</p><pre class="config-preview">${esc(integration.configuration)}</pre></div>`;
}

async function loadCache() {
  const [cache, dashboard, repositories] = await Promise.all([api('/cache'), api('/stats'), api('/mirrors')]);
  const jobs = cache.purge_jobs || [], maximum = cache.maximum_bytes || cache.max_bytes || 0, byRepository = dashboard.stats.by_mirror || {};
  $('#page-cache').innerHTML = `<div class="cards">${card(L('Cache files'), number(cache.files))}${card(L('Used space'), bytes(cache.bytes))}${card(L('Maximum space'), bytes(maximum))}${card(L('Global generation'), cache.global_generation)}</div>
    <div class="panel"><h2>${L('Cache storage')}</h2>${kv(L('Path'), cache.path)}${kv(L('Maximum files'), number(cache.maximum_files))}${kv(L('Minimum free space'), bytes(cache.minimum_free_bytes))}${kv(L('Inactive window'), duration(cache.inactive_seconds))}<button class="danger" id="clear-cache">${L('Global logical purge')}</button><p class="muted">${L('Logical invalidation is immediate. Physical files remain until the asynchronous Nginx cache manager completes its inactive/max_size cleanup window.')}</p></div>
    <div class="panel"><h2>${L('Repository cache traffic today')}</h2><p class="muted">${L('Nginx cache files are content-keyed; this table reports observed cache-served traffic, not guessed physical ownership.')}</p><div class="table-wrap"><table><thead><tr><th>${L('Repository')}</th><th>${L('HIT')}</th><th>${L('MISS')}</th><th>${L('Cache-served bytes')}</th></tr></thead><tbody>${repositories.map(repository => { const value = byRepository[repository.id] || {}; return `<tr><td>${esc(repository.name)}</td><td>${number(value.cache_hits)}</td><td>${number(value.cache_misses)}</td><td>${bytes(value.cache_bytes)}</td></tr>`; }).join('')}</tbody></table></div></div>
    <div class="panel"><h2>${L('Purge / reclaim jobs')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Time')}</th><th>${L('Scope')}</th><th>${L('Generation')}</th><th>${L('Logical purge')}</th><th>${L('Physical reclaim')}</th><th>${L('Reclaimed')}</th><th>${L('Operator')}</th></tr></thead><tbody>${jobs.map(job => `<tr><td>${date(job.created_at)}</td><td>${esc(job.scope)} ${job.repository_id || ''}</td><td>${job.old_generation} → ${job.new_generation}</td><td><span class="badge ok">${L('Completed')}</span></td><td><span class="badge ${job.reclaim_state === 'completed' ? 'ok' : job.reclaim_state === 'failed' ? 'bad' : ''}" title="${esc(job.error || '')}">${esc(stateLabel(job.reclaim_state))}</span></td><td>${bytes(job.reclaimed_bytes)}</td><td>${esc(job.operator)}</td></tr>`).join('')}</tbody></table></div></div>`;
  $('#clear-cache').addEventListener('click', async () => { if (!confirm(L('Invalidate every existing cache namespace?'))) return; try { const result = await api('/cache', {method: 'DELETE'}); notice(L('Logical purge completed; physical reclaim is %s.', result.physical_reclaim)); await loadCache(); } catch (error) { notice(error.message, true); } });
}

async function loadHealth() {
  const health = await api('/health');
  const endpointLabel = `${health.upstream_network || 'unix'} · ${health.upstream_address || ''}`;
  const frontendLabel = `${health.frontend_network || 'unix'} · ${health.frontend_address || ''}`;
  $('#page-health').innerHTML = `<div class="cards">${card('MirrorRelay', health.mirrorrelay, health.mirrorrelay === 'healthy')}${card(`${L('Frontend endpoint')} (${frontendLabel})`, health.frontend_endpoint || health.frontend_socket, health.frontend_endpoint === 'healthy')}${card(L('External Shared Nginx'), health.external_shared_nginx)}${card('Go Router', health.go_router)}${card(L('Managed Upstream Nginx'), stateLabel(health.managed_upstream_nginx), health.managed_upstream_nginx === 'running')}${card(`${L('Upstream endpoint')} (${endpointLabel})`, health.upstream_endpoint || health.upstream_socket, health.upstream_endpoint === 'healthy')}</div><div class="panel"><h2>${L('Repositories')}</h2>${(health.repositories || []).map(repository => `<div class="kv"><span>${esc(repository.name)}</span><span class="badge ${repository.health_state === 'healthy' ? 'ok' : repository.health_state === 'unhealthy' ? 'bad' : ''}">${esc(stateLabel(repository.health_state))}</span></div>`).join('')}</div>`;
}
async function loadAccess() { const lines = await api('/access'); $('#page-access').innerHTML = `<div class="panel"><div class="toolbar"><h2>access.log</h2><button id="refresh-access">${L('Refresh')}</button></div><pre class="config-preview">${esc((lines || []).join('\n') || L('No access records.'))}</pre></div>`; $('#refresh-access').addEventListener('click', loadAccess); }
async function loadAudit() { const entries = (await api('/audit')) || []; $('#page-audit').innerHTML = `<div class="table-wrap"><table><thead><tr><th>${L('Time')}</th><th>${L('User')}</th><th>${L('Client')}</th><th>${L('Action')}</th><th>${L('Object / detail')}</th><th>${L('Result')}</th></tr></thead><tbody>${entries.map(entry => `<tr><td>${date(entry.time)}</td><td>${esc(entry.username)}</td><td>${esc(entry.client_ip)}</td><td>${esc(entry.action)}</td><td>${esc(entry.object)} ${esc(entry.detail)}</td><td><span class="badge ${entry.succeeded ? 'ok' : 'bad'}">${entry.succeeded ? L('Success') : L('Failed')}</span></td></tr>`).join('')}</tbody></table></div>`; }

async function triggerRestart() {
  if (!confirm(L('Restart MirrorRelay service now? The application will reconnect automatically once ready.'))) return;
  try {
    notice(L('Requesting service restart...'));
    await api('/system/restart', {method: 'POST'});
  } catch (error) {
    $('#settings-error').textContent = error.message;
  }
  notice(L('MirrorRelay is restarting, reconnecting...'));
  document.querySelectorAll('#restart-header, #restart-sidebar, #restart-service-btn, #restart-settings-btn, #restart-system-btn').forEach(btn => {
    btn.disabled = true;
    btn.textContent = L('Restarting...');
  });
  let retries = 0;
  const maxRetries = 30;
  const poll = setInterval(async () => {
    retries++;
    try {
      const ping = await api('/system');
      if (ping && ping.version) {
        clearInterval(poll);
        notice(L('MirrorRelay restarted successfully.'));
        document.querySelectorAll('#restart-header, #restart-sidebar, #restart-service-btn, #restart-settings-btn, #restart-system-btn').forEach(btn => {
          btn.disabled = false;
          if (btn.id === 'restart-sidebar') btn.textContent = L('Restart');
          else if (btn.id === 'restart-service-btn') btn.textContent = L('Restart now');
          else if (btn.id === 'restart-settings-btn') btn.textContent = L('Restart MirrorRelay');
          else btn.textContent = L('Restart service');
        });
        await renderCurrentPage();
      }
    } catch (_) {
      if (retries >= maxRetries) {
        clearInterval(poll);
        notice(L('Restart timed out. Please check server status.'), true);
        document.querySelectorAll('#restart-header, #restart-sidebar, #restart-service-btn, #restart-settings-btn, #restart-system-btn').forEach(btn => {
          btn.disabled = false;
          if (btn.id === 'restart-sidebar') btn.textContent = L('Restart');
          else if (btn.id === 'restart-service-btn') btn.textContent = L('Restart now');
          else if (btn.id === 'restart-settings-btn') btn.textContent = L('Restart MirrorRelay');
          else btn.textContent = L('Restart service');
        });
      }
    }
  }, 1000);
}

async function loadSystem() {
  const [system, dashboard] = await Promise.all([api('/system'), api('/stats')]); const runtime = dashboard.stats.runtime || {}, upstreamNginx = system.upstream_nginx || {};
  $('#page-system').innerHTML = `<div class="grid2"><div class="panel"><div class="toolbar"><h2>MirrorRelay</h2><button type="button" class="secondary" id="restart-system-btn">${L('Restart service')}</button></div>${kv(L('Program version'), system.version)}${kv(L('Build ID'), system.build_id)}${kv(L('Architecture'), `${system.target_os}/${system.architecture}`)}${kv(L('Go version'), system.go_version)}${kv(L('Uptime'), duration(system.uptime_seconds))}${kv(L('Public base URL'), system.public_base_url || L('Not configured'))}</div><div class="panel"><h2>${L('Runtime resources')}</h2>${kv(L('Go heap allocated'), bytes(runtime.heap_alloc_bytes))}${kv(L('Go heap in use'), bytes(runtime.heap_inuse_bytes))}${kv(L('Go heap objects'), number(runtime.heap_objects))}${kv(L('Total allocations'), bytes(runtime.total_alloc_bytes))}${kv('Mallocs / Frees', `${number(runtime.mallocs)} / ${number(runtime.frees)}`)}${kv('RSS', bytes(runtime.rss_bytes))}${kv(L('Goroutines'), number(runtime.goroutines))}${kv(L('Open file descriptors'), number(runtime.open_fds))}${kv(L('GC cycles'), number(runtime.gc_count))}${kv(L('GC pause total'), `${((runtime.gc_pause_total_ns || 0) / 1e9).toFixed(3)} s`)}${kv(L('GC CPU fraction'), `${((runtime.gc_cpu_fraction || 0) * 100).toFixed(3)}%`)}</div></div><div class="grid2"><div class="panel"><h2>${L('TLS / Ingress')}</h2>${kv(L('Ingress mode'), system.ingress_mode)}${kv(L('HTTPS listen'), system.https_listen)}${kv(L('Minimum TLS'), system.tls_min_version)}${system.ingress_mode === 'managed-standalone' ? kv(L('Certificate'), system.tls_certificate) + kv(L('Private key'), system.tls_private_key) : ''}${kv(L('Frontend endpoint'), `${system.frontend_network} · ${system.frontend_address}`)}${kv(L('Upstream endpoint'), `${system.upstream_network} · ${system.upstream_address}`)}</div><div class="panel"><h2>${L('Managed Upstream Nginx')}</h2>${kv(L('Mode'), upstreamNginx.mode)}${kv(L('State'), stateLabel(upstreamNginx.state))}${kv(L('Version'), upstreamNginx.version || '—')}${kv(L('Build ID'), upstreamNginx.build_id || '—')}${kv(L('Architecture'), upstreamNginx.architecture || '—')}${kv('SHA-256', upstreamNginx.sha256 || '—')}${kv(L('Uptime'), duration(upstreamNginx.uptime_seconds || 0))}${kv(L('Last exit'), exitSummary(upstreamNginx))}<p class="muted">${L('Repository changes become active only after candidate generation, nginx -t, atomic publication and graceful reload.')}</p></div></div>`;
  const restartSysBtn = $('#restart-system-btn');
  if (restartSysBtn) restartSysBtn.addEventListener('click', triggerRestart);
}

function nestedValue(object, path) {
  return path.split('.').reduce((value, part) => value?.[part], object);
}

function setNestedValue(object, path, value) {
  const parts = path.split('.');
  const final = parts.pop();
  const parent = parts.reduce((value, part) => value[part], object);
  parent[final] = value;
}

function settingsInput(field, settings) {
  const value = nestedValue(settings, field.path);
  const label = field.label;
  const attributes = `data-setting-path="${esc(field.path)}" data-setting-type="${esc(field.valueType || field.type)}"`;
  if (field.type === 'boolean') return `<label class="check"><input type="checkbox" ${attributes}${value ? ' checked' : ''}><span>${esc(label)}</span></label>`;
  if (field.type === 'select') {
    const options = field.options.map(option => `<option value="${esc(option[0])}"${String(option[0]) === String(value) ? ' selected' : ''}>${esc(option[1])}</option>`).join('');
    return `<label>${esc(label)}<select ${attributes}>${options}</select></label>`;
  }
  if (field.type === 'list') return `<label class="wide">${esc(label)}<textarea rows="3" ${attributes}>${esc((value || []).join('\n'))}</textarea></label>`;
  const limits = `${field.min !== undefined ? ` min="${field.min}"` : ''}${field.max !== undefined ? ` max="${field.max}"` : ''}`;
  return `<label>${esc(label)}<input type="${field.type}" value="${esc(value ?? '')}" placeholder="${esc(field.placeholder || '')}" ${attributes}${limits}></label>`;
}

async function loadSettings() {
  const response = await api('/settings');
  const settings = response.settings;
  const loc = getLocale();
  const settingsGroups = loc.settingsGroups || [];
  const restart = response.restart_required
    ? `<div class="notice error"><span>${L('Saved values differ from the running process. Restart MirrorRelay to apply them.')}</span> <div class="actions" class="notice-actions"><button type="button" class="secondary" id="restart-service-btn" style="padding:4px 10px;font-size:13px;">${L('Restart now')}</button> <code>sudo systemctl restart mirrorrelay</code></div></div>`
    : `<div class="notice">${L('The running process matches the saved settings.')}</div>`;
  const groups = settingsGroups.map(group => `<fieldset><legend>${esc(group.title)}</legend><div class="form-grid">${group.fields.map(field => settingsInput(field, settings)).join('')}</div></fieldset>`).join('');
  $('#page-settings').innerHTML = `${restart}<div class="panel"><p>${L('These operational settings are stored in SQLite, strictly validated, and override the matching YAML values after restart. Repository changes continue to use the immediate Desired/Active validation workflow.')}</p>${kv(L('Source'), response.source === 'web_ui' ? L('Web UI override') : L('Configuration file'))}<p class="muted">${L('File-only bootstrap settings:')} <code>${esc((response.file_only || []).join(', '))}</code></p></div>
    <form id="settings-form" class="settings-form">${groups}<footer><button type="button" class="secondary" id="reset-settings">${L('Reset to YAML after restart')}</button><button type="button" class="secondary" id="restart-settings-btn">${L('Restart MirrorRelay')}</button><button type="submit">${L('Validate and save')}</button></footer><div id="settings-error" class="error"></div></form>`;
  const restartNoticeBtn = $('#restart-service-btn');
  if (restartNoticeBtn) restartNoticeBtn.addEventListener('click', triggerRestart);
  const restartFooterBtn = $('#restart-settings-btn');
  if (restartFooterBtn) restartFooterBtn.addEventListener('click', triggerRestart);
  $('#settings-form').addEventListener('submit', async event => {
    event.preventDefault();
    const next = JSON.parse(JSON.stringify(settings));
    event.target.querySelectorAll('[data-setting-path]').forEach(input => {
      let value;
      if (input.dataset.settingType === 'boolean') value = input.checked;
      else if (input.dataset.settingType === 'number') value = Number(input.value);
      else if (input.dataset.settingType === 'list') value = parseList(input.value);
      else value = input.value.trim();
      setNestedValue(next, input.dataset.settingPath, value);
    });
    try {
      const saved = await api('/settings', {method: 'PUT', body: JSON.stringify(next)});
      notice(saved.restart_required ? L('Settings saved; restart MirrorRelay to apply them.') : L('Settings already match the running process.'));
      await loadSettings();
    } catch (error) { $('#settings-error').textContent = error.message; }
  });
  $('#reset-settings').addEventListener('click', async () => {
    if (!confirm(L('Discard the Web UI override and restore YAML values after restart?'))) return;
    try { await api('/settings', {method: 'DELETE'}); notice(L('Web UI override removed; restart MirrorRelay.')); await loadSettings(); } catch (error) { $('#settings-error').textContent = error.message; }
  });
}

async function loadCluster() {
  const [overview, nodes] = await Promise.all([
    api('/cluster/overview').catch(() => ({role: 'standalone', enabled: false})),
    api('/cluster/nodes').catch(() => [])
  ]);

  const overviewHtml = `<div class="cards">
    ${card(L('Cluster role'), overview.role || 'standalone')}
    ${card(L('Cluster status'), overview.enabled ? L('Enabled') : L('Disabled'), overview.enabled)}
    ${card(L('Total nodes'), overview.total_nodes || 0)}
    ${card(L('Healthy nodes'), overview.healthy_nodes || 0, (overview.healthy_nodes || 0) > 0)}
    ${card(L('Routable nodes'), overview.routable_nodes || 0, (overview.routable_nodes || 0) > 0)}
    ${card(L('Routing mode'), overview.routing_mode || 'hybrid')}
  </div>
  <div class="panel">
    <h2>${L('Cluster Fingerprint')}</h2>
    <p><code>${esc(overview.cluster_fingerprint || L('Not initialized'))}</code></p>
  </div>`;

  const nodeRows = (nodes || []).map(node => {
    const isHealthy = node.health_status === 'healthy';
    const isMatch = node.config_status === 'match';
    return `<tr>
      <td><strong>${esc(node.name)}</strong></td>
      <td><code>${esc(node.url)}</code></td>
      <td>${esc(node.region)}${node.country ? ` (${esc(node.country)})` : ''}</td>
      <td>${node.priority} / ${node.weight}</td>
      <td><span class="badge ${isHealthy ? 'ok' : 'bad'}">${esc(node.health_status || 'unknown')}</span></td>
      <td><span class="badge ${isMatch ? 'ok' : 'bad'}">${esc(node.config_status || 'unknown')}</span></td>
      <td><code title="${esc(node.config_fingerprint)}">${esc((node.config_fingerprint || '').slice(0, 15))}...</code></td>
      <td>${node.last_check ? date(node.last_check) : '—'}</td>
      <td>
        <button class="small secondary" data-action="check-node" data-id="${node.id}">${L('Check')}</button>
        <button class="small secondary" data-action="edit-node" data-id="${node.id}">${L('Edit')}</button>
        <button class="small secondary" data-action="toggle-node" data-id="${node.id}" data-enabled="${node.enabled}">${node.enabled ? L('Disable') : L('Enable')}</button>
        <button class="small danger" data-action="delete-node" data-id="${node.id}">${L('Delete')}</button>
      </td>
    </tr>`;
  }).join('');

  const tableHtml = `<div class="panel">
    <h2>${L('Edge nodes')}</h2>
    <div class="table-wrap"><table><thead><tr>
      <th>${L('Name')}</th>
      <th>${L('URL')}</th>
      <th>${L('Region')}</th>
      <th>${L('Priority / Weight')}</th>
      <th>${L('Health')}</th>
      <th>${L('Config')}</th>
      <th>${L('Fingerprint')}</th>
      <th>${L('Last check')}</th>
      <th>${L('Actions')}</th>
    </tr></thead><tbody>${nodeRows || `<tr><td colspan="9" class="empty">${L('No edge nodes registered yet.')}</td></tr>`}</tbody></table></div>
  </div>`;

  $('#cluster-overview').innerHTML = overviewHtml;
  $('#cluster-node-list').innerHTML = tableHtml;
}

$('#add-node')?.addEventListener('click', () => {
  $('#node-form').reset();
  $('#node-id').value = '';
  $('#node-form-title').textContent = L('Add edge node');
  $('#node-enabled').checked = true;
  $('#node-priority').value = '100';
  $('#node-weight').value = '100';
  $('#node-error').textContent = '';
  $('#node-dialog').showModal();
});
$('#close-node-dialog')?.addEventListener('click', () => $('#node-dialog').close());
$('#cancel-node-dialog')?.addEventListener('click', () => $('#node-dialog').close());

$('#node-form')?.addEventListener('submit', async event => {
  event.preventDefault();
  $('#node-error').textContent = '';
  const id = $('#node-id').value;
  const payload = {
    name: $('#node-name').value.trim(),
    url: $('#node-url').value.trim(),
    region: $('#node-region').value.trim(),
    country: $('#node-country').value.trim().toUpperCase(),
    priority: Number($('#node-priority').value) || 100,
    weight: Number($('#node-weight').value) || 100,
    enabled: $('#node-enabled').checked
  };
  try {
    if (id) {
      await api(`/cluster/nodes/${id}`, {method: 'PUT', body: JSON.stringify(payload)});
      notice(L('Node updated.'));
    } else {
      await api('/cluster/nodes', {method: 'POST', body: JSON.stringify(payload)});
      notice(L('Node added.'));
    }
    $('#node-dialog').close();
    await loadCluster();
  } catch (error) {
    $('#node-error').textContent = error.message;
  }
});

$('#reset-cluster-fp')?.addEventListener('click', async () => {
  if (!confirm(L('Reset the cluster configuration fingerprint? It will reinitialize from active nodes.'))) return;
  try {
    await api('/cluster/fingerprint/reset', {method: 'POST'});
    notice(L('Cluster fingerprint reset.'));
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
});

window.checkNode = async id => {
  try {
    await api(`/cluster/nodes/${id}/check`, {method: 'POST'});
    notice(L('Node probe completed.'));
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
};

window.editNode = async id => {
  try {
    const nodes = await api('/cluster/nodes');
    const node = (nodes || []).find(n => n.id === id);
    if (!node) return;
    $('#node-id').value = node.id;
    $('#node-name').value = node.name || '';
    $('#node-url').value = node.url || '';
    $('#node-region').value = node.region || '';
    $('#node-country').value = node.country || '';
    $('#node-priority').value = node.priority || 100;
    $('#node-weight').value = node.weight || 100;
    $('#node-enabled').checked = node.enabled;
    $('#node-form-title').textContent = L('Edit edge node');
    $('#node-error').textContent = '';
    $('#node-dialog').showModal();
  } catch (error) {
    notice(error.message, true);
  }
};

window.toggleNode = async (id, currentEnabled) => {
  try {
    const action = currentEnabled ? 'disable' : 'enable';
    await api(`/cluster/nodes/${id}/${action}`, {method: 'POST'});
    notice(currentEnabled ? L('Node disabled.') : L('Node enabled.'));
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
};

window.deleteNode = async id => {
  if (!confirm(L('Delete this edge node?'))) return;
  try {
    await api(`/cluster/nodes/${id}`, {method: 'DELETE'});
    notice(L('Node deleted.'));
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
};

async function loadUsers() {
  const users = (await api('/users')) || [];
  $('#page-users').innerHTML = `<form class="panel narrow" id="user-form"><h2>${L('Add administrator')}</h2><div class="form-grid"><label>${L('Username')}<input id="new-user" minlength="3" maxlength="64" required></label><label>${L('Initial password')}<input id="new-user-pass" type="password" minlength="10" required></label></div><button>${L('Create user')}</button><div id="user-error" class="error"></div></form><div class="panel"><h2>${L('User list')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Username')}</th><th>${L('Created')}</th><th>${L('Updated')}</th><th></th></tr></thead><tbody>${users.map(user => `<tr><td>${esc(user.username)}</td><td>${date(user.created_at)}</td><td>${date(user.updated_at)}</td><td><button class="danger" data-action="delete-user" data-id="${user.id}">${L('Delete')}</button></td></tr>`).join('')}</tbody></table></div></div>`;
  $('#user-form').addEventListener('submit', async event => { event.preventDefault(); try { await api('/users', {method: 'POST', body: JSON.stringify({username: $('#new-user').value, password: $('#new-user-pass').value})}); notice(L('User created.')); await loadUsers(); } catch (error) { $('#user-error').textContent = error.message; } });
}
window.deleteUser = async id => { if (!confirm(L('Delete this administrator account?'))) return; try { await api(`/users/${id}`, {method: 'DELETE'}); notice(L('User deleted.')); await loadUsers(); } catch (error) { notice(error.message, true); } };
async function loadAccount() { $('#page-account').innerHTML = `<form class="panel narrow" id="password-form"><h2>${L('Change password')}</h2><label>${L('Current password')}<input id="old-pass" type="password" required></label><label>${L('New password (at least 10 characters)')}<input id="new-pass" type="password" minlength="10" required></label><button>${L('Update password')}</button><div class="error" id="pass-error"></div></form>`; $('#password-form').addEventListener('submit', async event => { event.preventDefault(); try { await api('/auth/password', {method: 'PUT', body: JSON.stringify({current_password: $('#old-pass').value, new_password: $('#new-pass').value})}); notice(L('Password updated.')); event.target.reset(); } catch (error) { $('#pass-error').textContent = error.message; } }); }

async function runAction(button) {
  const id = Number(button.dataset.id);
  switch (button.dataset.action) {
    case 'show-repository': return window.showRepository(id);
    case 'copy-repository-url': return window.copyRepositoryURL(id);
    case 'check-mirror': return window.checkMirror(id);
    case 'preview-repository-config': return window.previewRepositoryConfig(id);
    case 'purge-repository': return window.purgeRepository(id);
    case 'edit-mirror': return window.editMirror(id);
    case 'edit-mirror-from-detail':
      $('#detail-dialog').close();
      return window.editMirror(id);
    case 'toggle-mirror': return window.toggleMirror(id, button.dataset.enabled === 'true');
    case 'delete-mirror': return window.deleteMirror(id);
    case 'preview-profile-upgrade': return window.previewProfileUpgrade(id, button.dataset.name, button.dataset.version);
    case 'view-effective-config': return window.viewEffectiveConfig();
    case 'rollback-config': return window.rollbackConfig(Number(button.dataset.version));
    case 'edit-custom': return window.editCustom(id);
    case 'delete-custom': return window.deleteCustom(id);
    case 'check-node': return window.checkNode(id);
    case 'edit-node': return window.editNode(id);
    case 'toggle-node': return window.toggleNode(id, button.dataset.enabled === 'true');
    case 'delete-node': return window.deleteNode(id);
    case 'delete-user': return window.deleteUser(id);
    default: throw new Error(L('Unknown action.'));
  }
}

document.addEventListener('click', event => {
  const button = event.target.closest('button[data-action]');
  if (!button || button.disabled) return;
  event.preventDefault();
  button.disabled = true;
  void runAction(button)
    .catch(error => notice(error.message, true))
    .finally(() => { if (button.isConnected) button.disabled = false; });
});

$('#restart-header')?.addEventListener('click', triggerRestart);
$('#restart-sidebar')?.addEventListener('click', triggerRestart);

applyLanguage(language);
boot();

