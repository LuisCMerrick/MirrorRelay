'use strict';

let csrf = '';
let mirrors = [];
let profiles = [];
let customConfigs = [];
let signedIn = false;
let currentPage = 'dashboard';

const $ = selector => document.querySelector(selector);
const esc = value => String(value ?? '').replace(/[&<>"']/g, character => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[character]));
const storedLanguage = localStorage.getItem('repogate.language');
let language = storedLanguage === 'zh' || storedLanguage === 'en'
  ? storedLanguage
  : ((navigator.languages || [navigator.language]).some(value => /^zh(?:-|$)/i.test(value || '')) ? 'zh' : 'en');
const L = (english, chinese) => language === 'zh' ? chinese : english;

const dictionary = {
  tagline: ['Linux repository reverse-proxy gateway', 'Linux 软件仓库反向代理网关'],
  username: ['Username', '用户名'], password: ['Password', '密码'], signIn: ['Sign in', '登录'], signOut: ['Sign out', '退出'],
  navDashboard: ['Dashboard', '概览'], navRepositories: ['Repositories', '仓库'], navProfiles: ['Profiles', '模板'], navUpstreamNginx: ['Managed Upstream Nginx', '受管上游 Nginx'],
  navCustom: ['Custom configuration', '自定义配置'], navIngress: ['Ingress integration', '入口接入'], navCluster: ['Cluster', '集群'], navCache: ['Cache', '缓存'], navHealth: ['Health', '健康状态'],
  navAccess: ['Access log', '访问日志'], navAudit: ['Audit log', '审计日志'], navSystem: ['System', '系统'], navSettings: ['Settings', '设置'], navUsers: ['Users', '用户'], navAccount: ['My account', '我的账号'],
  addRepository: ['Add repository', '新增仓库'], addCustom: ['Add custom configuration', '新增自定义配置'], addNode: ['Add node', '新增节点'], resetFingerprint: ['Reset fingerprint', '重置指纹'],
  identityRouting: ['Identity and routing', '标识与路由'],
  profileVersion: ['Profile / version', '模板 / 版本'], name: ['Name', '名称'], repositoryType: ['Repository type', '仓库类型'], publicMode: ['Public mode', '公开模式'],
  publicHost: ['Public host', '公开 Host'], publicPath: ['Public path', '公开路径'], accessPolicy: ['Access policy', '访问策略'], description: ['Description', '备注'],
  nodeURL: ['Public base URL', '公开基础 URL'], region: ['Region', '地域'], country: ['Country', '国家/地区'], priority: ['Priority', '优先级'], weight: ['Weight', '权重'], save: ['Save', '保存'],
  upstreamsPaths: ['Upstreams and path mapping', '上游与路径映射'], upstreamList: ['Upstreams (one “priority URL” per line)', '上游列表（每行“优先级 URL”）'],
  stripPrefix: ['Strip prefix', '移除前缀'], addPrefix: ['Add prefix', '添加前缀'], hostRewrite: ['Host rewrite', 'Host 改写'], proxyMode: ['Proxy mode', '代理模式'], redirectPolicy: ['Redirect policy', '重定向策略'],
  headersTimeouts: ['Headers and timeouts', 'Header 与超时'], headerAdd: ['Add request headers (one “Name: Value” per line)', '添加请求 Header（每行“名称: 值”）'],
  headerRemove: ['Remove request headers (comma or newline separated)', '移除请求 Header（逗号或换行分隔）'], connectTimeout: ['Connect timeout (seconds)', '连接超时（秒）'],
  readTimeout: ['Read timeout (seconds)', '读取超时（秒）'], sendTimeout: ['Send timeout (seconds)', '发送超时（秒）'], cacheRewrite: ['Cache and rewrite', '缓存与改写'],
  cacheProfile: ['Cache profile', '缓存模板'], rewriteProfile: ['Rewrite profile', '改写模板'], rewriteHosts: ['Allowed rewrite/redirect hosts (comma or newline separated)', '允许改写/重定向的 Host（逗号或换行分隔）'],
  metadataLimit: ['Metadata rewrite limit (bytes, 0 = global)', 'Metadata 改写上限（字节，0 = 全局）'], metadataTTL: ['Metadata TTL (seconds, 0 = global)', 'Metadata TTL（秒，0 = 全局）'],
  packageTTL: ['Package TTL (seconds, 0 = global)', '软件包 TTL（秒，0 = 全局）'], immutableTTL: ['Immutable TTL (seconds, 0 = default)', '不可变对象 TTL（秒，0 = 默认）'],
  blobTTL: ['Blob TTL (seconds, 0 = default)', 'Blob TTL（秒，0 = 默认）'], cacheEnabled: ['Enable disk cache', '启用磁盘缓存'], rewriteEnabled: ['Enable metadata rewrite', '启用 Metadata 改写'],
  htmlRewriteEnabled: ['Rewrite same-origin URLs in browsable HTML', '改写可浏览 HTML 中的同源 URL'],
  cacheAuthenticated: ['Cache authenticated responses (public content only)', '缓存认证响应（仅用于公开内容）'], healthLimits: ['Health and limits', '健康检查与限流'],
  healthPath: ['Health-check path', '健康检查路径'], expectedStatus: ['Expected status', '预期状态码'], healthInterval: ['Check interval (seconds)', '检查间隔（秒）'],
  healthTimeout: ['Check timeout (seconds)', '检查超时（秒）'], healthMethod: ['Check method', '检查方法'], rateProfile: ['Rate-limit profile', '限流模板'],
  maxConcurrency: ['Repository max concurrency (0 = profile)', '仓库最大并发（0 = 模板）'], bandwidthLimit: ['Per-connection limit B/s (0 = unlimited)', '单连接限速 B/s（0 = 不限）'],
  healthEnabled: ['Enable health checks', '启用健康检查'], repositoryEnabled: ['Enable repository', '启用仓库'], registrySecurity: ['Registry and upstream security', 'Registry 与上游安全'],
  registryAuth: ['Registry auth', 'Registry 认证'], blobRedirect: ['Blob redirect', 'Blob 重定向'], tokenUpstream: ['Token upstream (optional)', 'Token 上游（可选）'], pullOnly: ['Pull only', '仅允许 Pull'],
  allowHTTP: ['Allow HTTP upstream (requires system policy)', '允许 HTTP 上游（需要系统总开关）'], allowPrivate: ['Allow private upstream (requires system policy)', '允许私网上游（需要系统总开关）'],
  cancel: ['Cancel', '取消'], saveActivate: ['Validate, save and activate', '验证、保存并生效'], enabled: ['Enabled', '启用'], configuration: ['Configuration', '配置内容'],
  validateApply: ['Validate and apply', '验证并应用'], slug: ['Slug', '短标识'], customOption: ['Custom', '自定义'], pathMode: ['Path mode', '路径模式'], hostMode: ['Host mode', '独立域名模式'],
  publicAccess: ['Public', '公开'], adminAccess: ['Admin CIDR only', '仅管理 CIDR'], transparentMode: ['Transparent', '透明代理'], rewriteMode: ['Rewrite adapter', '改写适配器'],
  passClient: ['Pass to client', '传给客户端'], followBroker: ['Follow through broker', '由安全代理跟随'], rewriteLocal: ['Rewrite to local URL', '改写为本地 URL'], fullProxy: ['Full proxy', '全代理'],
  standard: ['Standard', '标准'], packages: ['Packages', '软件包'], unlimitedDefault: ['Unlimited/default', '不限 / 默认'], conservative: ['Conservative (16)', '保守 (16)'], balanced: ['Balanced (64)', '均衡 (64)'], bulk: ['Bulk (256)', '大批量 (256)'],
  directAuth: ['Direct auth', '直连认证'], fullProxyAuth: ['Full-proxy auth', '全代理认证'], passRedirect: ['Pass redirect', '透传重定向'], context: ['Context', '上下文'], repositoryID: ['Repository ID (0 = global)', '仓库 ID（0 = 全局）']
};

const pageMeta = {
  dashboard: [['Dashboard', '概览'], ['Live service status', '服务实时运行状态']],
  mirrors: [['Repositories', '仓库'], ['Profiles, upstreams and active data-plane configuration', '模板、上游与已生效数据面配置']],
  profiles: [['Profiles', '模板'], ['Versioned, overridable repository defaults', '可覆盖、带版本的仓库默认配置']],
  'upstream-nginx': [['Managed Upstream Nginx', '受管上游 Nginx'], ['Status, effective configuration, history and rollback', '状态、生效配置、历史与回滚']],
  custom: [['Custom configuration', '自定义配置'], ['Controlled advanced Managed Upstream Nginx directives', '受控的受管上游 Nginx 指令']],
  ingress: [['Ingress integration', '入口接入'], ['External Shared Nginx connection details', '外部共享 Nginx 接入信息']],
  cluster: [['Cluster', '集群'], ['Distributed edge nodes, routing and configuration consistency', '分布式边缘节点、路由策略与配置一致性']],
  cache: [['Cache', '缓存'], ['Generation invalidation and asynchronous physical reclaim', 'Generation 逻辑失效与异步物理回收']],
  health: [['Health', '健康状态'], ['RepoGate, local transports, Managed Upstream Nginx and repositories', 'RepoGate、本地传输、受管上游 Nginx 与仓库']],
  access: [['Access log', '访问日志'], ['Latest 200 Managed Upstream Nginx access records', '受管上游 Nginx 最近 200 条访问记录']],
  audit: [['Audit log', '审计日志'], ['Administrative actions and client addresses', '管理操作与客户端地址留痕']],
  system: [['System', '系统'], ['Runtime, memory and build information', '运行时、内存与构建信息']],
  settings: [['Settings', '设置'], ['Validated operational configuration saved through the Web UI', '通过 Web UI 保存并严格验证运行配置']],
  users: [['Users', '用户'], ['Administrator account management', '管理员账号管理']],
  account: [['My account', '我的账号'], ['Change the current password', '修改当前密码']]
};

function applyLanguage(next, persist = false) {
  language = next === 'zh' ? 'zh' : 'en';
  if (persist) localStorage.setItem('repogate.language', language);
  document.documentElement.lang = language === 'zh' ? 'zh-CN' : 'en';
  document.querySelectorAll('[data-i18n]').forEach(element => {
    const value = dictionary[element.dataset.i18n];
    if (value) element.textContent = value[language === 'zh' ? 1 : 0];
  });
  document.querySelectorAll('.language-switch button').forEach(button => button.classList.toggle('active', button.dataset.lang === language));
  updatePageHeading();
  if (signedIn) {
    void (async () => {
      await loadProfilesData();
      await renderCurrentPage();
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

function locale() { return language === 'zh' ? 'zh-CN' : 'en-US'; }
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
  return language === 'zh' ? `${days}天 ${hours}小时 ${minutes}分` : `${days}d ${hours}h ${minutes}m`;
}
function stateLabel(value) {
  const labels = {active: ['Active', '已生效'], pending: ['Pending', '待生效'], failed: ['Failed', '失败'], healthy: ['Healthy', '正常'], unhealthy: ['Unhealthy', '异常'], unknown: ['Unknown', '未知'], running: ['Running', '运行中'], completed: ['Completed', '已完成'], disabled: ['Disabled', '已禁用'], restarting: ['Restarting', '重启中']};
  const pair = labels[String(value || '').toLowerCase()];
  return pair ? pair[language === 'zh' ? 1 : 0] : String(value || '—');
}
function exitSummary(status = {}) {
  if (!status.last_exit_at) return '—';
  const code = status.last_exit_code === -1
    ? L('exit code unknown', '退出码未知')
    : L(`exit code ${status.last_exit_code ?? '—'}`, `退出码 ${status.last_exit_code ?? '—'}`);
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
$('#logout').addEventListener('click', async () => { await api('/auth/logout', {method: 'POST'}); csrf = ''; location.reload(); });

function updatePageHeading() {
  const metadata = pageMeta[currentPage] || pageMeta.dashboard;
  $('#page-title').textContent = metadata[0][language === 'zh' ? 1 : 0];
  $('#page-subtitle').textContent = metadata[1][language === 'zh' ? 1 : 0];
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
  $('#status').textContent = `${L('Managed Upstream Nginx', '受管上游 Nginx')} ${stateLabel(upstreamNginx.state)}`;
  $('#status').className = `status ${upstreamNginx.state === 'running' ? 'online' : ''}`;
  const perRepository = dashboard.stats.by_mirror || {};
  const repositoryRows = repositories.map(repository => {
    const counters = perRepository[repository.id] || {};
    const upstream = activeUpstreamFor(repository);
    const health = healthFor(repository);
    return `<tr><td><button class="text-button" data-action="show-repository" data-id="${repository.id}"><strong>${esc(repository.name)}</strong></button></td><td><span class="badge ${health === 'healthy' ? 'ok' : health === 'unhealthy' ? 'bad' : ''}">${esc(stateLabel(health))}</span></td><td>${number(counters.requests || 0)}</td><td>${bytes(counters.bytes || 0)}</td><td>${upstream.latency_ms ? `${number(upstream.latency_ms)} ms` : '—'}</td><td>${number(counters.cache_hits || 0)} / ${number(counters.cache_misses || 0)}</td><td>${number(counters.status_2xx || 0)} / ${number(counters.status_3xx || 0)} / ${number(counters.status_4xx || 0)} / ${number(counters.status_5xx || 0)}</td><td>${number(counters.upstream_errors || 0)}</td></tr>`;
  }).join('');
  $('#page-dashboard').innerHTML = `<div class="cards">
    ${card(L('Repositories / enabled', '仓库 / 启用'), `${dashboard.mirrors} / ${dashboard.enabled_mirrors}`)}
    ${card(L('Healthy / unhealthy', '正常 / 异常'), `${dashboard.healthy_mirrors || 0} / ${dashboard.unhealthy_mirrors || 0}`, dashboard.unhealthy_mirrors === 0)}
    ${card(L('Managed Upstream Nginx', '受管上游 Nginx'), stateLabel(upstreamNginx.state), upstreamNginx.state === 'running')}
    ${card(L('Active requests', '活动请求'), number(dashboard.stats.active_requests))}
    ${card(L('Requests today', '今日请求'), number(today.requests))}
    ${card(L('Traffic today', '今日流量'), bytes(today.bytes))}
    ${card(L('Traffic / 24 h', '24 小时流量'), bytes(last24.bytes))}
    ${card(L('Traffic / 7 d', '7 天流量'), bytes(last7.bytes))}
    ${card(L('Cache hit rate', '缓存命中率'), `${hitRate.toFixed(1)}%`)}
  </div><div class="grid2"><div class="panel"><h2>${L('Cache usage', '缓存占用')}</h2>
    <div class="bar-row"><span>${number(cache.files)} ${L('files', '个文件')}</span><div class="bar"><i style="width:${maximum ? Math.min(100, 100 * cache.bytes / maximum) : 0}%"></i></div><span>${maximum ? (100 * cache.bytes / maximum).toFixed(1) : '0.0'}%</span></div>
    <p class="muted">${bytes(cache.bytes)} / ${bytes(maximum)}</p></div>
    <div class="panel"><h2>${L('RepoGate and Managed Upstream Nginx', 'RepoGate 与受管上游 Nginx')}</h2>${kv(L('Managed Upstream Nginx PID', '受管上游 Nginx PID'), upstreamNginx.pid || '—')}${kv(L('Managed Upstream Nginx version', '受管上游 Nginx 版本'), upstreamNginx.version || '—')}${kv(L('Managed Upstream Nginx build ID', '受管上游 Nginx 构建 ID'), upstreamNginx.build_id || '—')}${kv(L('Managed Upstream Nginx architecture', '受管上游 Nginx 架构'), upstreamNginx.architecture || '—')}${kv(L('Managed Upstream Nginx uptime', '受管上游 Nginx 运行时间'), duration(upstreamNginx.uptime_seconds || 0))}${kv(L('RepoGate version', 'RepoGate 版本'), dashboard.version || '—')}${kv(L('RepoGate build ID', 'RepoGate 构建 ID'), dashboard.build_id || '—')}${kv(L('RepoGate architecture', 'RepoGate 架构'), dashboard.architecture || '—')}${kv(L('Active config', '生效配置'), `v${upstreamNginx.current_config_version || '—'}`)}${kv(L('RepoGate uptime', 'RepoGate 运行时间'), duration(dashboard.uptime_seconds))}</div></div>
    <div class="panel"><h2>${L('Repository statistics today', '仓库今日统计')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Repository', '仓库')}</th><th>${L('Health', '健康')}</th><th>${L('Requests', '请求')}</th><th>${L('Traffic', '流量')}</th><th>${L('Latency', '延迟')}</th><th>${L('Cache HIT / MISS', '缓存命中 / 未命中')}</th><th>2xx / 3xx / 4xx / 5xx</th><th>${L('Upstream errors', '上游错误')}</th></tr></thead><tbody>${repositoryRows || `<tr><td colspan="8" class="empty">${L('No repositories yet.', '尚未创建仓库。')}</td></tr>`}</tbody></table></div></div>`;
}
function card(label, value, accent = false) { return `<div class="card"><small>${esc(label)}</small><strong class="${accent ? 'accent' : ''}">${esc(value)}</strong></div>`; }
function kv(label, value) { return `<div class="kv"><span>${esc(label)}</span><span>${esc(value)}</span></div>`; }

async function loadProfilesData() {
  profiles = (await api('/profiles')) || [];
  $('#template').innerHTML = `<option value="">${L('Custom', '自定义')}</option>` + profiles.map((profile, index) => `<option value="${index}">${esc(profile.name)} · ${esc(profile.version)}${profile.latest_stable ? ` · ${L('latest', '最新')}` : ''}</option>`).join('');
}

async function loadProfiles() {
  if (!profiles.length) await loadProfilesData();
  $('#page-profiles').innerHTML = `<div class="panel"><p class="muted">${L('Profiles are versioned defaults. Every field remains editable after applying a profile, and existing repositories stay pinned until an explicit upgrade.', '模板是带版本的默认值。应用后每个字段仍可编辑；已有仓库会保持固定版本，直到管理员明确升级。')}</p></div>
  <div class="table-wrap"><table><thead><tr><th>${L('Profile', '模板')}</th><th>${L('Version', '版本')}</th><th>${L('Type', '类型')}</th><th>${L('Upstream', '上游')}</th><th>${L('Mode', '模式')}</th><th>${L('Cache / rewrite', '缓存 / 改写')}</th></tr></thead><tbody>
  ${profiles.map(profile => `<tr><td><strong>${esc(profile.name)}</strong>${profile.latest_stable ? ` <span class="badge ok">${L('Latest stable', '最新稳定版')}</span>` : ''}</td><td>${esc(profile.version)}</td><td>${esc(profile.type)}</td><td><code>${esc(profile.upstream)}</code></td><td>${esc(profile.public_mode)} / ${esc(profile.proxy_mode)}</td><td>${profile.cache_enabled ? L('Cache', '缓存') : '—'} ${profile.rewrite_enabled ? `· ${L('Rewrite', '改写')}` : ''}</td></tr>`).join('')}</tbody></table></div>`;
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
      <td class="actions"><button data-action="show-repository" data-id="${repository.id}">${L('Details', '详情')}</button><button data-action="copy-repository-url" data-id="${repository.id}">${L('Copy URL', '复制地址')}</button><button data-action="check-mirror" data-id="${repository.id}">${L('Test', '测试')}</button><button data-action="preview-repository-config" data-id="${repository.id}">${L('Config', '配置')}</button><button data-action="purge-repository" data-id="${repository.id}">${L('Purge', '清缓存')}</button><button data-action="edit-mirror" data-id="${repository.id}">${L('Edit', '编辑')}</button><button data-action="toggle-mirror" data-id="${repository.id}" data-enabled="${!repository.enabled}">${repository.enabled ? L('Disable', '禁用') : L('Enable', '启用')}</button><button class="danger" data-action="delete-mirror" data-id="${repository.id}">${L('Delete', '删除')}</button></td></tr>`;
  }).join('');
  $('#mirror-list').innerHTML = `<div class="table-wrap"><table><thead><tr><th>${L('Name', '名称')}</th><th>${L('Type / profile', '类型 / 模板')}</th><th>${L('Public URL', '公开地址')}</th><th>${L('Active upstream', '活动上游')}</th><th>${L('Health / latency', '健康 / 延迟')}</th><th>${L('Desired state', '期望状态')}</th><th>${L('Cache', '缓存')}</th><th>${L('Actions', '操作')}</th></tr></thead><tbody>${rows || `<tr><td colspan="8" class="empty">${L('No repositories yet.', '尚未创建仓库。')}</td></tr>`}</tbody></table></div>`;
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
  $('#form-title').textContent = repository ? L('Edit repository', '编辑仓库') : L('Add repository', '新增仓库');
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
    if (index <= 0) throw new Error(L(`Invalid header line: ${line}`, `Header 格式错误：${line}`));
    result[line.slice(0, index).trim()] = line.slice(index + 1).trim();
  }
  return result;
}
function parseUpstreams(value) {
  return value.split(/\n+/).filter(line => line.trim()).map(line => {
    const match = line.trim().match(/^(\d+)\s+(https?:\/\/\S+)$/);
    if (!match) throw new Error(L(`Invalid upstream line: ${line}`, `上游格式错误：${line}`));
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
    notice(L('Candidate validated and activated with a graceful reload.', '候选配置验证通过，已 Graceful Reload 生效。'));
    await Promise.all([loadMirrors(), loadDashboard()]);
  } catch (error) { $('#form-error').textContent = error.message; }
});

window.editMirror = id => openMirrorForm(mirrors.find(repository => repository.id === id));
window.copyRepositoryURL = async id => {
  const repository = mirrors.find(value => value.id === id);
  if (!repository) return;
  try { await copyText(publicURL(repository)); notice(L('Repository URL copied.', '仓库地址已复制。')); } catch (error) { notice(error.message, true); }
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
  if (!copied) throw new Error(L('Clipboard access is unavailable.', '浏览器无法访问剪贴板。'));
}
window.checkMirror = async id => {
  notice(L('Checking upstreams…', '正在检查上游…'));
  try {
    const results = await api(`/mirrors/${id}/check`, {method: 'POST'});
    const healthy = results.length > 0 && results.every(result => result.healthy);
    notice(healthy ? L('All upstreams are healthy.', '全部上游正常。') : L('One or more upstreams are unhealthy.', '一个或多个上游异常。'), !healthy);
    await loadMirrors();
  } catch (error) { notice(error.message, true); }
};
window.toggleMirror = async (id, enabled) => {
  try {
    await api(`/mirrors/${id}/${enabled ? 'enable' : 'disable'}`, {method: 'POST'});
    notice(enabled ? L('Repository enabled.', '仓库已启用。') : L('Repository disabled.', '仓库已禁用。'));
    await loadMirrors();
  } catch (error) { notice(error.message, true); }
};
window.deleteMirror = async id => {
  if (!confirm(L('Delete this repository and logically invalidate its cache? This cannot be undone.', '删除此仓库并逻辑失效其缓存？此操作不可撤销。'))) return;
  try { await api(`/mirrors/${id}`, {method: 'DELETE'}); notice(L('Repository deleted.', '仓库已删除。')); await loadMirrors(); } catch (error) { notice(error.message, true); }
};

window.showRepository = async id => {
  try {
    const [state, examples] = await Promise.all([api(`/mirrors/${id}/state`), api(`/mirrors/${id}/client-config`)]);
    const desired = state.desired, active = state.active_found ? state.active : null, statistics = state.statistics || {};
    const latest = profiles.find(profile => profile.name === desired.profile_name && profile.latest_stable);
    const upgrade = latest && latest.version !== desired.profile_version ? `<button data-action="preview-profile-upgrade" data-id="${id}" data-name="${esc(latest.name)}" data-version="${esc(latest.version)}">${L(`Preview upgrade to ${latest.version}`, `预览升级到 ${latest.version}`)}</button>` : '';
    $('#detail-title').textContent = desired.name;
    $('#detail-content').innerHTML = `<div class="cards detail-cards">${card(L('Desired state', '期望状态'), stateLabel(desired.config_state), desired.config_state === 'active')}${card(L('Active state', '生效状态'), active ? L('Published', '已发布') : L('Not active', '未生效'), Boolean(active))}${card(L('Effective config', '生效配置'), `v${state.effective_config_version || '—'}`)}${card(L('Requests today', '今日请求'), number(statistics.requests || 0))}${card(L('Traffic today', '今日流量'), bytes(statistics.bytes || 0))}${card(L('Observed cache traffic', '观测到的缓存流量'), bytes(statistics.cache_bytes || 0))}${card(L('Cache HIT / MISS', '缓存命中 / 未命中'), `${number(statistics.cache_hits || 0)} / ${number(statistics.cache_misses || 0)}`)}${card('2xx / 3xx / 4xx / 5xx', `${number(statistics.status_2xx || 0)} / ${number(statistics.status_3xx || 0)} / ${number(statistics.status_4xx || 0)} / ${number(statistics.status_5xx || 0)}`)}${card(L('Upstream errors', '上游错误'), number(statistics.upstream_errors || 0))}</div>
      <div class="toolbar"><div class="actions"><button data-action="copy-repository-url" data-id="${id}">${L('Copy URL', '复制地址')}</button><button data-action="edit-mirror-from-detail" data-id="${id}">${L('Edit', '编辑')}</button><button data-action="check-mirror" data-id="${id}">${L('Test', '测试')}</button><button data-action="preview-repository-config" data-id="${id}">${L('Preview config', '预览配置')}</button><button data-action="view-effective-config">${L('Effective config', '生效配置')}</button><button data-action="purge-repository" data-id="${id}">${L('Purge cache', '清除缓存')}</button>${upgrade}</div></div>
      <div class="grid2"><div class="panel"><h2>${L('Desired configuration', '期望配置')}</h2>${repositorySummary(desired)}</div><div class="panel"><h2>${L('Active routing snapshot', '已生效路由快照')}</h2>${active ? repositorySummary(active) : `<p class="muted">${L('No active version. The desired configuration may have failed validation or activation.', '没有生效版本；期望配置可能验证或激活失败。')}</p>`}</div></div>
      <div class="panel"><h2>${L('Upstreams', '上游')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Priority', '优先级')}</th><th>URL</th><th>${L('Health', '健康')}</th><th>${L('Latency', '延迟')}</th><th>${L('Last check', '最后检查')}</th></tr></thead><tbody>${(desired.upstreams || []).map(upstream => `<tr><td>${upstream.priority}</td><td><code>${esc(upstream.url)}</code></td><td>${esc(stateLabel(upstream.health_status))}</td><td>${number(upstream.latency_ms)} ms</td><td>${date(upstream.last_check)}</td></tr>`).join('')}</tbody></table></div></div>
      <div class="panel"><h2>${L('Client configuration examples', '客户端配置示例')}</h2>${examples.map((example, index) => `<div class="example"><div class="toolbar"><strong>${esc(example.name)}</strong><button class="copy-example" data-index="${index}">${L('Copy', '复制')}</button></div><pre>${esc(example.command)}</pre></div>`).join('')}</div>`;
    $('#detail-content').querySelectorAll('.copy-example').forEach(button => button.addEventListener('click', async () => { await copyText(examples[Number(button.dataset.index)].command); notice(L('Copied.', '已复制。')); }));
    $('#detail-dialog').showModal();
  } catch (error) { notice(error.message, true); }
};

function repositorySummary(repository) {
  return `${kv(L('Public URL', '公开地址'), publicURL(repository))}${kv(L('Type / mode', '类型 / 模式'), `${repository.type} / ${repository.proxy_mode}`)}${kv(L('Profile', '模板'), `${repository.profile_name || 'Custom'} ${repository.profile_version || ''}`)}${kv(L('Cache', '缓存'), repository.cache_enabled ? `${repository.cache_profile} · ${repository.cache_authenticated ? L('authenticated enabled', '认证缓存已启用') : L('anonymous only', '仅匿名')}` : L('Disabled', '禁用'))}${kv(L('Browsable HTML URL rewrite', '可浏览 HTML URL 改写'), repository.html_rewrite_enabled ? L('Enabled', '启用') : L('Disabled', '禁用'))}${kv(L('Rewrite hosts', '改写 Host'), (repository.rewrite_hosts || []).join(', ') || '—')}${repository.config_error ? `<div class="notice error">${esc(repository.config_error)}</div>` : ''}`;
}

window.previewRepositoryConfig = async id => {
  try {
    const value = await api(`/mirrors/${id}/config`);
    showPreview(L('Generated repository configuration', '生成的仓库配置'), `<pre class="config-preview">${esc(value.configuration)}</pre>`);
  } catch (error) { notice(error.message, true); }
};
window.viewEffectiveConfig = async () => {
  try {
    const value = await api('/upstream-nginx/config');
    showPreview(L('Effective Managed Upstream Nginx configuration', '受管上游 Nginx 生效配置'), `<pre class="config-preview">${esc(value.configuration)}</pre>`);
  } catch (error) { notice(error.message, true); }
};
window.previewProfileUpgrade = async (id, name, version) => {
  try {
    const value = await api(`/mirrors/${id}/profile/preview`, {method: 'POST', body: JSON.stringify({name, version})});
    const rows = Object.entries(value.diff || {}).map(([field, change]) => `<tr><td>${esc(field)}</td><td><code>${esc(JSON.stringify(change.before))}</code></td><td><code>${esc(JSON.stringify(change.after))}</code></td></tr>`).join('');
    showPreview(L('Profile upgrade preview', '模板升级预览'), `<div class="table-wrap"><table><thead><tr><th>${L('Field', '字段')}</th><th>${L('Before', '升级前')}</th><th>${L('After', '升级后')}</th></tr></thead><tbody>${rows}</tbody></table></div><div class="toolbar end"><button id="apply-profile-upgrade">${L('Apply upgrade', '应用升级')}</button></div><pre class="config-preview">${esc(value.configuration)}</pre>`);
    $('#apply-profile-upgrade').addEventListener('click', async () => {
      try { await api(`/mirrors/${id}/profile/apply`, {method: 'POST', body: JSON.stringify({name, version})}); $('#preview-dialog').close(); $('#detail-dialog').close(); notice(L('Profile upgrade activated.', '模板升级已生效。')); await loadMirrors(); } catch (error) { notice(error.message, true); }
    });
  } catch (error) { notice(error.message, true); }
};
window.purgeRepository = async id => {
  const path = prompt(L('Optional object path. Leave empty to purge the whole repository cache.', '可选：输入单对象路径；留空则清除整个仓库缓存。'), '');
  if (path === null) return;
  try {
    const result = path ? await api(`/mirrors/${id}/cache/purge`, {method: 'POST', body: JSON.stringify({path, query: ''})}) : await api(`/mirrors/${id}/cache`, {method: 'DELETE'});
    notice(L(`Logical purge completed; physical reclaim: ${result.physical_reclaim}.`, `逻辑失效已完成；物理回收：${result.physical_reclaim}。`));
  } catch (error) { notice(error.message, true); }
};
function showPreview(title, content) { $('#preview-title').textContent = title; $('#preview-content').innerHTML = content; $('#preview-dialog').showModal(); }

async function loadUpstreamNginx() {
  try {
    const [status, config, history] = await Promise.all([api('/upstream-nginx/status'), api('/upstream-nginx/config'), api('/upstream-nginx/history')]);
    $('#page-upstream-nginx').innerHTML = `<div class="cards">${card(L('State', '状态'), stateLabel(status.state), status.state === 'running')}${card('PID', status.pid || '—')}${card(L('Uptime', '运行时间'), duration(status.uptime_seconds || 0))}${card(L('Config version', '配置版本'), `v${status.current_config_version || '—'}`)}${card(L('Managed Upstream Nginx version', '受管上游 Nginx 版本'), (status.version || '—').replace(/^nginx version:\s*/, ''))}${card(L('Build ID', '构建 ID'), status.build_id || '—')}${card(L('Architecture', '架构'), status.architecture || '—')}</div>
      ${status.last_error ? `<div class="notice error">${esc(status.last_error)}</div>` : ''}<div class="toolbar"><div>${status.integration_snippet ? `<span class="muted">${L('Integration snippet', '接入片段')}: ${esc(status.integration_snippet)} · ${esc(status.integration_result || '')}</span>` : ''}</div><button id="reload-upstream-nginx">${L('Regenerate, validate and reload', '重新生成、验证并 Reload')}</button></div>
      <div class="grid2"><div class="panel"><h2>${L('Configuration history', '配置历史')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Version', '版本')}</th><th>${L('Time', '时间')}</th><th>${L('Operator', '操作人')}</th><th>${L('Description', '说明')}</th><th>${L('State', '状态')}</th><th></th></tr></thead><tbody>${history.map(item => `<tr><td>v${item.version}</td><td>${date(item.created_at)}</td><td>${esc(item.operator)}</td><td>${esc(item.description)}</td><td><span class="badge ${item.active ? 'ok' : ''}">${item.active ? L('Active', '生效') : L('History', '历史')}</span></td><td>${item.active ? '' : `<button data-action="rollback-config" data-version="${item.version}">${L('Rollback', '回滚')}</button>`}</td></tr>`).join('')}</tbody></table></div></div><div class="panel"><h2>${L('Runtime and build', '运行与构建')}</h2>${kv(L('Last reload', '最后 Reload'), status.last_reload ? date(status.last_reload) : '—')}${kv(L('Reload result', 'Reload 结果'), status.last_reload_result || '—')}${kv(L('Last exit', '最后退出'), exitSummary(status))}<pre class="config-preview">${esc(status.build_options || L('Build options unavailable.', '无构建参数。'))}</pre></div><div class="panel"><h2>${L('Effective configuration', '生效配置')}</h2><pre class="config-preview">${esc(config.configuration)}</pre></div></div>`;
    $('#reload-upstream-nginx').addEventListener('click', async () => { try { await api('/upstream-nginx/reload', {method: 'POST'}); notice(L('Validation passed and Managed Upstream Nginx reloaded.', '验证通过，受管上游 Nginx 已 Reload。')); await loadUpstreamNginx(); } catch (error) { notice(error.message, true); } });
  } catch (error) { $('#page-upstream-nginx').innerHTML = `<div class="notice error">${esc(error.message)}</div>`; }
}
window.rollbackConfig = async version => {
  if (!confirm(L(`Rollback repositories and custom configuration to v${version}?`, `将仓库和自定义配置回滚到 v${version}？`))) return;
  try { await api(`/upstream-nginx/history/${version}/rollback`, {method: 'POST'}); notice(L(`Rolled back through a validated graceful reload.`, '已通过验证并 Graceful Reload 完成回滚。')); await Promise.all([loadUpstreamNginx(), loadMirrors()]); } catch (error) { notice(error.message, true); }
};

async function loadCustom() {
  customConfigs = (await api('/custom-configs')) || [];
  $('#custom-list').innerHTML = `<div class="panel"><p class="muted">${L('These directives apply only to Managed Upstream Nginx. Dangerous process, filesystem and context-escape directives are rejected.', '这些指令仅应用于受管上游 Nginx；危险的进程、文件系统和上下文逃逸指令会被拒绝。')}</p></div><div class="table-wrap"><table><thead><tr><th>${L('Name', '名称')}</th><th>${L('Context', '上下文')}</th><th>${L('Repository', '仓库')}</th><th>${L('State', '状态')}</th><th>${L('Last validation', '最后验证')}</th><th>${L('Actions', '操作')}</th></tr></thead><tbody>${customConfigs.map(value => `<tr><td><strong>${esc(value.name)}</strong></td><td>${esc(value.context)}</td><td>${value.repository_id || L('Global', '全局')}</td><td><span class="badge ${value.enabled ? 'ok' : ''}">${value.enabled ? L('Enabled', '启用') : L('Disabled', '禁用')}</span></td><td>${esc(value.last_validation_result || '—')}</td><td class="actions"><button data-action="edit-custom" data-id="${value.id}">${L('Edit', '编辑')}</button><button class="danger" data-action="delete-custom" data-id="${value.id}">${L('Delete', '删除')}</button></td></tr>`).join('')}</tbody></table></div>`;
}
function openCustom(value = null) {
  $('#custom-form').reset(); $('#custom-id').value = value?.id || ''; $('#custom-title').textContent = value ? L('Edit custom Managed Upstream Nginx configuration', '编辑自定义受管上游 Nginx 配置') : L('Add custom Managed Upstream Nginx configuration', '新增自定义受管上游 Nginx 配置');
  $('#custom-name').value = value?.name || ''; $('#custom-context').value = value?.context || 'http'; $('#custom-repository').value = value?.repository_id || 0; $('#custom-enabled').checked = value?.enabled ?? true; $('#custom-content').value = value?.content || ''; $('#custom-error').textContent = ''; $('#custom-dialog').showModal();
}
$('#add-custom').addEventListener('click', () => openCustom()); $('#close-custom').addEventListener('click', () => $('#custom-dialog').close()); $('#cancel-custom').addEventListener('click', () => $('#custom-dialog').close());
window.editCustom = id => openCustom(customConfigs.find(value => value.id === id));
window.deleteCustom = async id => { if (!confirm(L('Delete this custom configuration and reload Managed Upstream Nginx?', '删除此自定义配置并 Reload 受管上游 Nginx？'))) return; try { await api(`/custom-configs/${id}`, {method: 'DELETE'}); notice(L('Custom configuration deleted.', '自定义配置已删除。')); await loadCustom(); } catch (error) { notice(error.message, true); } };
$('#custom-form').addEventListener('submit', async event => { event.preventDefault(); const id = $('#custom-id').value; const body = {name: $('#custom-name').value, context: $('#custom-context').value, repository_id: Number($('#custom-repository').value), enabled: $('#custom-enabled').checked, content: $('#custom-content').value}; try { await api(id ? `/custom-configs/${id}` : '/custom-configs', {method: id ? 'PUT' : 'POST', body: JSON.stringify(body)}); $('#custom-dialog').close(); notice(L('Custom configuration validated and activated.', '自定义配置验证并生效。')); await loadCustom(); } catch (error) { $('#custom-error').textContent = error.message; } });

async function loadIngress() {
  const integration = await api('/ingress/snippet');
  $('#page-ingress').innerHTML = `<div class="cards">${card(L('Ingress mode', '入口模式'), integration.mode)}${card(L('Frontend network', '前端网络'), integration.frontend_network)}${card(L('Frontend address', '前端地址'), integration.frontend_address)}</div><div class="panel"><h2>${L('External Shared Nginx integration snippet', 'External Shared Nginx 接入片段')}</h2><p class="muted">${L('The generated file is a scoped deployment aid. RepoGate does not own or reload the External Shared Nginx process.', '生成文件是限定范围的部署辅助；RepoGate 不接管或 Reload 共享 External Shared Nginx 进程。')}</p><pre class="config-preview">${esc(integration.configuration)}</pre></div>`;
}

async function loadCache() {
  const [cache, dashboard, repositories] = await Promise.all([api('/cache'), api('/stats'), api('/mirrors')]);
  const jobs = cache.purge_jobs || [], maximum = cache.maximum_bytes || cache.max_bytes || 0, byRepository = dashboard.stats.by_mirror || {};
  $('#page-cache').innerHTML = `<div class="cards">${card(L('Cache files', '缓存文件'), number(cache.files))}${card(L('Used space', '已用空间'), bytes(cache.bytes))}${card(L('Maximum space', '最大空间'), bytes(maximum))}${card(L('Global generation', '全局 Generation'), cache.global_generation)}</div>
    <div class="panel"><h2>${L('Cache storage', '缓存存储')}</h2>${kv(L('Path', '路径'), cache.path)}${kv(L('Maximum files', '最大文件数'), number(cache.maximum_files))}${kv(L('Minimum free space', '最小空闲空间'), bytes(cache.minimum_free_bytes))}${kv(L('Inactive window', 'Inactive 窗口'), duration(cache.inactive_seconds))}<button class="danger" id="clear-cache">${L('Global logical purge', '全局逻辑失效')}</button><p class="muted">${L('Logical invalidation is immediate. Physical files remain until the asynchronous Nginx cache manager completes its inactive/max_size cleanup window.', '逻辑失效立即完成；物理文件会保留到异步 Nginx Cache Manager 完成 inactive/max_size 回收窗口。')}</p></div>
    <div class="panel"><h2>${L('Repository cache traffic today', '今日仓库缓存流量')}</h2><p class="muted">${L('Nginx cache files are content-keyed; this table reports observed cache-served traffic, not guessed physical ownership.', 'Nginx Cache 文件按内容 Key 存储；此表展示观测到的缓存响应流量，不猜测物理文件归属。')}</p><div class="table-wrap"><table><thead><tr><th>${L('Repository', '仓库')}</th><th>HIT</th><th>MISS</th><th>${L('Cache-served bytes', '缓存响应字节')}</th></tr></thead><tbody>${repositories.map(repository => { const value = byRepository[repository.id] || {}; return `<tr><td>${esc(repository.name)}</td><td>${number(value.cache_hits)}</td><td>${number(value.cache_misses)}</td><td>${bytes(value.cache_bytes)}</td></tr>`; }).join('')}</tbody></table></div></div>
    <div class="panel"><h2>${L('Purge / reclaim jobs', 'Purge / 回收任务')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Time', '时间')}</th><th>${L('Scope', '范围')}</th><th>Generation</th><th>${L('Logical purge', '逻辑失效')}</th><th>${L('Physical reclaim', '物理回收')}</th><th>${L('Reclaimed', '已回收')}</th><th>${L('Operator', '操作人')}</th></tr></thead><tbody>${jobs.map(job => `<tr><td>${date(job.created_at)}</td><td>${esc(job.scope)} ${job.repository_id || ''}</td><td>${job.old_generation} → ${job.new_generation}</td><td><span class="badge ok">${L('Completed', '已完成')}</span></td><td><span class="badge ${job.reclaim_state === 'completed' ? 'ok' : job.reclaim_state === 'failed' ? 'bad' : ''}" title="${esc(job.error || '')}">${esc(stateLabel(job.reclaim_state))}</span></td><td>${bytes(job.reclaimed_bytes)}</td><td>${esc(job.operator)}</td></tr>`).join('')}</tbody></table></div></div>`;
  $('#clear-cache').addEventListener('click', async () => { if (!confirm(L('Invalidate every existing cache namespace?', '让全部现有缓存命名空间立即逻辑失效？'))) return; try { const result = await api('/cache', {method: 'DELETE'}); notice(L(`Logical purge completed; physical reclaim is ${result.physical_reclaim}.`, `逻辑失效已完成；物理回收状态为 ${result.physical_reclaim}。`)); await loadCache(); } catch (error) { notice(error.message, true); } });
}

async function loadHealth() {
  const health = await api('/health');
  const endpointLabel = `${health.upstream_network || 'unix'} · ${health.upstream_address || ''}`;
  const frontendLabel = `${health.frontend_network || 'unix'} · ${health.frontend_address || ''}`;
  $('#page-health').innerHTML = `<div class="cards">${card('RepoGate', health.repogate, health.repogate === 'healthy')}${card(`${L('Frontend endpoint', '前端端点')} (${frontendLabel})`, health.frontend_endpoint || health.frontend_socket, health.frontend_endpoint === 'healthy')}${card(L('External Shared Nginx', '外部共享 Nginx'), health.external_shared_nginx)}${card('Go Router', health.go_router)}${card('Managed Upstream Nginx', stateLabel(health.managed_upstream_nginx), health.managed_upstream_nginx === 'running')}${card(`${L('Upstream endpoint', '上游端点')} (${endpointLabel})`, health.upstream_endpoint || health.upstream_socket, health.upstream_endpoint === 'healthy')}</div><div class="panel"><h2>${L('Repositories', '仓库')}</h2>${(health.repositories || []).map(repository => `<div class="kv"><span>${esc(repository.name)}</span><span class="badge ${repository.health_state === 'healthy' ? 'ok' : repository.health_state === 'unhealthy' ? 'bad' : ''}">${esc(stateLabel(repository.health_state))}</span></div>`).join('')}</div>`;
}
async function loadAccess() { const lines = await api('/access'); $('#page-access').innerHTML = `<div class="panel"><div class="toolbar"><h2>access.log</h2><button id="refresh-access">${L('Refresh', '刷新')}</button></div><pre class="config-preview">${esc((lines || []).join('\n') || L('No access records.', '暂无访问记录。'))}</pre></div>`; $('#refresh-access').addEventListener('click', loadAccess); }
async function loadAudit() { const entries = (await api('/audit')) || []; $('#page-audit').innerHTML = `<div class="table-wrap"><table><thead><tr><th>${L('Time', '时间')}</th><th>${L('User', '用户')}</th><th>${L('Client', '客户端')}</th><th>${L('Action', '操作')}</th><th>${L('Object / detail', '对象 / 详情')}</th><th>${L('Result', '结果')}</th></tr></thead><tbody>${entries.map(entry => `<tr><td>${date(entry.time)}</td><td>${esc(entry.username)}</td><td>${esc(entry.client_ip)}</td><td>${esc(entry.action)}</td><td>${esc(entry.object)} ${esc(entry.detail)}</td><td><span class="badge ${entry.succeeded ? 'ok' : 'bad'}">${entry.succeeded ? L('Success', '成功') : L('Failed', '失败')}</span></td></tr>`).join('')}</tbody></table></div>`; }

async function loadSystem() {
  const [system, dashboard] = await Promise.all([api('/system'), api('/stats')]); const runtime = dashboard.stats.runtime || {}, upstreamNginx = system.upstream_nginx || {};
  $('#page-system').innerHTML = `<div class="grid2"><div class="panel"><h2>RepoGate</h2>${kv(L('Program version', '程序版本'), system.version)}${kv(L('Build ID', '构建 ID'), system.build_id)}${kv(L('Architecture', '架构'), `${system.target_os}/${system.architecture}`)}${kv(L('Go version', 'Go 版本'), system.go_version)}${kv(L('Uptime', '运行时间'), duration(system.uptime_seconds))}${kv(L('Public base URL', '公开基础地址'), system.public_base_url || L('Not configured', '未配置'))}</div><div class="panel"><h2>${L('Runtime resources', '运行时资源')}</h2>${kv(L('Go heap allocated', 'Go Heap 已分配'), bytes(runtime.heap_alloc_bytes))}${kv(L('Go heap in use', 'Go Heap 使用中'), bytes(runtime.heap_inuse_bytes))}${kv(L('Go heap objects', 'Go Heap 对象数'), number(runtime.heap_objects))}${kv(L('Total allocations', '累计分配'), bytes(runtime.total_alloc_bytes))}${kv('Mallocs / Frees', `${number(runtime.mallocs)} / ${number(runtime.frees)}`)}${kv('RSS', bytes(runtime.rss_bytes))}${kv(L('Goroutines', 'Goroutine'), number(runtime.goroutines))}${kv(L('Open file descriptors', '打开文件描述符'), number(runtime.open_fds))}${kv(L('GC cycles', 'GC 次数'), number(runtime.gc_count))}${kv(L('GC pause total', 'GC 总暂停'), `${((runtime.gc_pause_total_ns || 0) / 1e9).toFixed(3)} s`)}${kv(L('GC CPU fraction', 'GC 占比'), `${((runtime.gc_cpu_fraction || 0) * 100).toFixed(3)}%`)}</div></div><div class="grid2"><div class="panel"><h2>TLS / Ingress</h2>${kv(L('Ingress mode', '入口模式'), system.ingress_mode)}${kv(L('HTTPS listen', 'HTTPS 监听'), system.https_listen)}${kv(L('Minimum TLS', '最低 TLS'), system.tls_min_version)}${system.ingress_mode === 'managed-standalone' ? kv(L('Certificate', '证书'), system.tls_certificate) + kv(L('Private key', '私钥'), system.tls_private_key) : ''}${kv(L('Frontend endpoint', '前端端点'), `${system.frontend_network} · ${system.frontend_address}`)}${kv(L('Upstream endpoint', '上游端点'), `${system.upstream_network} · ${system.upstream_address}`)}</div><div class="panel"><h2>Managed Upstream Nginx</h2>${kv(L('Mode', '模式'), upstreamNginx.mode)}${kv(L('State', '状态'), stateLabel(upstreamNginx.state))}${kv(L('Version', '版本'), upstreamNginx.version || '—')}${kv(L('Build ID', '构建 ID'), upstreamNginx.build_id || '—')}${kv(L('Architecture', '架构'), upstreamNginx.architecture || '—')}${kv('SHA-256', upstreamNginx.sha256 || '—')}${kv(L('Uptime', '运行时间'), duration(upstreamNginx.uptime_seconds || 0))}${kv(L('Last exit', '最后退出'), exitSummary(upstreamNginx))}<p class="muted">${L('Repository changes become active only after candidate generation, nginx -t, atomic publication and graceful reload.', '仓库变更仅在候选生成、nginx -t、原子发布和 Graceful Reload 全部成功后生效。')}</p></div></div>`;
}

const settingsGroups = [
  {title: ['Local endpoints and ingress', '本地端点与入口'], fields: [
    {path: 'server.unix_socket_enabled', label: ['Frontend Unix socket', '前端 Unix Socket'], type: 'boolean'},
    {path: 'server.local_port', label: ['Frontend loopback port', '前端回环端口'], type: 'number', min: 1, max: 65535},
    {path: 'ingress.mode', label: ['Ingress mode', '入口模式'], type: 'select', options: [['external', 'External Shared Nginx', '外部共享 Nginx'], ['managed-standalone', 'Managed standalone', '独立受管入口']]},
    {path: 'ingress.generate_snippet', label: ['Generate ingress snippet', '生成入口配置片段'], type: 'boolean'},
    {path: 'http.listen', label: ['Standalone HTTP listen', '独立 HTTP 监听'], type: 'text'},
    {path: 'http.https_listen', label: ['Standalone HTTPS listen', '独立 HTTPS 监听'], type: 'text'},
    {path: 'http.public_base_url', label: ['Public base URL', '公开基础地址'], type: 'text', placeholder: 'https://mirror.example.com'},
    {path: 'tls.min_version', label: ['Minimum TLS version', '最低 TLS 版本'], type: 'select', options: [['1.2', 'TLS 1.2', 'TLS 1.2'], ['1.3', 'TLS 1.3', 'TLS 1.3']]},
    {path: 'http.read_timeout', label: ['HTTP read timeout', 'HTTP 读取超时'], type: 'text'},
    {path: 'http.write_timeout', label: ['HTTP write timeout', 'HTTP 写入超时'], type: 'text'},
    {path: 'http.idle_timeout', label: ['HTTP idle timeout', 'HTTP 空闲超时'], type: 'text'}
  ]},
  {title: ['Performance, metadata and redirects', '性能、Metadata 与重定向'], fields: [
    {path: 'performance.stream_buffer_size_bytes', label: ['Streaming buffer bytes', '流式缓冲字节'], type: 'select', valueType: 'number', options: [[32768, '32 KiB', '32 KiB'], [65536, '64 KiB', '64 KiB'], [131072, '128 KiB', '128 KiB']]},
    {path: 'performance.go_memory_limit_bytes', label: ['Go memory limit bytes (0 = environment/default)', 'Go 内存限制字节（0 = 环境/默认）'], type: 'number', min: 0},
    {path: 'performance.gogc', label: ['GOGC (-1..10000)', 'GOGC（-1..10000）'], type: 'number', min: -1, max: 10000},
    {path: 'metadata.rewrite_buffer_limit_bytes', label: ['Metadata rewrite limit bytes', 'Metadata 改写限制字节'], type: 'number', min: 1},
    {path: 'metadata.output_compression', label: ['Metadata output compression', 'Metadata 输出压缩'], type: 'select', options: [['auto', 'Automatic', '自动'], ['identity', 'Identity', '不压缩'], ['gzip', 'Gzip', 'Gzip']]},
    {path: 'metadata.gzip_min_length_bytes', label: ['Gzip minimum bytes', 'Gzip 最小字节'], type: 'number', min: 0},
    {path: 'metadata.validator_entries', label: ['Validator entries', 'Validator 条目数'], type: 'number', min: 1},
    {path: 'redirect.max_hops', label: ['Maximum redirect hops', '最大重定向次数'], type: 'number', min: 1, max: 20},
    {path: 'redirect.reject_mixed_dns_result', label: ['Reject mixed permitted/forbidden DNS results', '拒绝许可与禁止地址混合的 DNS 结果'], type: 'boolean'}
  ]},
  {title: ['Cache defaults', '缓存默认值'], fields: [
    {path: 'cache.max_size_bytes', label: ['Maximum cache bytes', '最大缓存字节'], type: 'number', min: 1},
    {path: 'cache.max_files', label: ['Maximum observed files', '最大观测文件数'], type: 'number', min: 1},
    {path: 'cache.minimum_free_bytes', label: ['Minimum free bytes', '最小保留空间字节'], type: 'number', min: 0},
    {path: 'cache.inactive', label: ['Inactive window', 'Inactive 窗口'], type: 'text'},
    {path: 'cache.metadata_ttl', label: ['Metadata TTL', 'Metadata TTL'], type: 'text'},
    {path: 'cache.package_ttl', label: ['Package TTL', '软件包 TTL'], type: 'text'},
    {path: 'cache.cleanup_interval', label: ['Cleanup observation interval', '清理观测间隔'], type: 'text'},
    {path: 'cache.wait_for_fill', label: ['Cache fill wait window', '缓存填充等待窗口'], type: 'text'}
  ]},
  {title: ['Security and administration', '安全与管理'], fields: [
    {path: 'security.allow_http_upstream', label: ['Allow HTTP upstream globally', '全局允许 HTTP 上游'], type: 'boolean'},
    {path: 'security.allow_private_upstream', label: ['Allow private upstream globally', '全局允许私网上游'], type: 'boolean'},
    {path: 'security.expose_client_ip', label: ['Expose validated client IP internally', '在内部暴露已验证客户端 IP'], type: 'boolean'},
    {path: 'security.session_timeout', label: ['Session timeout', '会话超时'], type: 'text'},
    {path: 'security.login_window', label: ['Login throttle window', '登录限流窗口'], type: 'text'},
    {path: 'security.login_max_failures', label: ['Maximum login failures', '最大登录失败次数'], type: 'number', min: 1},
    {path: 'security.admin_cidrs', label: ['Admin CIDRs (one per line)', '管理 CIDR（每行一个）'], type: 'list'}
  ]},
  {title: ['Transport and limits', '传输与限流'], fields: [
    {path: 'transport.dial_timeout', label: ['Dial timeout', '连接超时'], type: 'text'},
    {path: 'transport.keep_alive', label: ['TCP keepalive', 'TCP Keepalive'], type: 'text'},
    {path: 'transport.tls_handshake_timeout', label: ['TLS handshake timeout', 'TLS 握手超时'], type: 'text'},
    {path: 'transport.response_header_timeout', label: ['Response header timeout', '响应头超时'], type: 'text'},
    {path: 'transport.idle_connection_timeout', label: ['Idle connection timeout', '空闲连接超时'], type: 'text'},
    {path: 'transport.max_idle_connections', label: ['Maximum idle connections', '最大空闲连接数'], type: 'number', min: 1},
    {path: 'transport.max_idle_connections_per_host', label: ['Maximum idle connections per host', '每个 Host 最大空闲连接数'], type: 'number', min: 1},
    {path: 'limits.max_total_concurrency', label: ['Global concurrency (0 = unlimited)', '全局并发（0 = 不限）'], type: 'number', min: 0},
    {path: 'limits.max_ip_concurrency', label: ['Per-IP concurrency (0 = unlimited)', '每 IP 并发（0 = 不限）'], type: 'number', min: 0},
    {path: 'limits.bandwidth_limit_bps', label: ['Global bandwidth B/s (0 = unlimited)', '全局带宽 B/s（0 = 不限）'], type: 'number', min: 0}
  ]},
  {title: ['Logging and lifecycle', '日志与生命周期'], fields: [
    {path: 'logging.queue_size', label: ['Log queue size', '日志队列大小'], type: 'number', min: 1},
    {path: 'logging.max_size_mb', label: ['Log file maximum MiB', '单日志最大 MiB'], type: 'number', min: 1},
    {path: 'logging.keep_days', label: ['Log retention days', '日志保留天数'], type: 'number', min: 1},
    {path: 'health.worker_interval', label: ['Health worker interval', '健康检查调度间隔'], type: 'text'},
    {path: 'shutdown.grace_period', label: ['Graceful shutdown window', '优雅退出窗口'], type: 'text'}
  ]},
  {title: ['Managed Upstream Nginx', '受管上游 Nginx'], fields: [
    {path: 'upstream_nginx.mode', label: ['Mode', '模式'], type: 'select', options: [['managed', 'Managed', '受管'], ['external', 'External advanced mode', '外部高级模式'], ['disabled', 'Disabled', '禁用']]},
    {path: 'upstream_nginx.upstream_unix_socket_enabled', label: ['Use upstream Unix socket', '使用上游 Unix Socket'], type: 'boolean'},
    {path: 'upstream_nginx.upstream_local_port', label: ['Upstream loopback port', '上游回环端口'], type: 'number', min: 1, max: 65535},
    {path: 'upstream_nginx.tls_verify_depth', label: ['TLS verification depth', 'TLS 验证深度'], type: 'number', min: 1, max: 20},
    {path: 'upstream_nginx.resolver', label: ['DNS resolvers (space separated)', 'DNS Resolver（空格分隔）'], type: 'text'},
    {path: 'upstream_nginx.resolver_refresh', label: ['Resolver refresh', 'Resolver 刷新间隔'], type: 'text'},
    {path: 'upstream_nginx.history_limit', label: ['Configuration history limit', '配置历史数量'], type: 'number', min: 1},
    {path: 'upstream_nginx.restart_max_failures', label: ['Restart maximum failures', '最大重启失败次数'], type: 'number', min: 1},
    {path: 'upstream_nginx.restart_window', label: ['Restart failure window', '重启失败窗口'], type: 'text'},
    {path: 'upstream_nginx.restart_initial_backoff', label: ['Initial restart backoff', '初始重启退避'], type: 'text'},
    {path: 'upstream_nginx.restart_max_backoff', label: ['Maximum restart backoff', '最大重启退避'], type: 'text'},
    {path: 'upstream_nginx.worker_processes', label: ['Worker processes', 'Worker 进程数'], type: 'text'},
    {path: 'upstream_nginx.worker_user', label: ['Worker user (empty is allowed)', 'Worker 用户（可留空）'], type: 'text'},
    {path: 'upstream_nginx.worker_connections', label: ['Worker connections', 'Worker 连接数'], type: 'number', min: 1},
    {path: 'upstream_nginx.stop_on_repogate_exit', label: ['Stop Nginx when RepoGate exits', 'RepoGate 退出时停止 Nginx'], type: 'boolean'}
  ]}
];

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
  const label = L(field.label[0], field.label[1]);
  const attributes = `data-setting-path="${esc(field.path)}" data-setting-type="${esc(field.valueType || field.type)}"`;
  if (field.type === 'boolean') return `<label class="check"><input type="checkbox" ${attributes}${value ? ' checked' : ''}><span>${esc(label)}</span></label>`;
  if (field.type === 'select') {
    const options = field.options.map(option => `<option value="${esc(option[0])}"${String(option[0]) === String(value) ? ' selected' : ''}>${esc(L(option[1], option[2]))}</option>`).join('');
    return `<label>${esc(label)}<select ${attributes}>${options}</select></label>`;
  }
  if (field.type === 'list') return `<label class="wide">${esc(label)}<textarea rows="3" ${attributes}>${esc((value || []).join('\n'))}</textarea></label>`;
  const limits = `${field.min !== undefined ? ` min="${field.min}"` : ''}${field.max !== undefined ? ` max="${field.max}"` : ''}`;
  return `<label>${esc(label)}<input type="${field.type}" value="${esc(value ?? '')}" placeholder="${esc(field.placeholder || '')}" ${attributes}${limits}></label>`;
}

async function loadSettings() {
  const response = await api('/settings');
  const settings = response.settings;
  const restart = response.restart_required
    ? `<div class="notice error">${L('Saved values differ from the running process. Restart RepoGate to apply them.', '已保存值与当前进程不同；请重启 RepoGate 后生效。')} <code>sudo systemctl restart repogate</code></div>`
    : `<div class="notice">${L('The running process matches the saved settings.', '当前进程与已保存设置一致。')}</div>`;
  const groups = settingsGroups.map(group => `<fieldset><legend>${esc(L(group.title[0], group.title[1]))}</legend><div class="form-grid">${group.fields.map(field => settingsInput(field, settings)).join('')}</div></fieldset>`).join('');
  $('#page-settings').innerHTML = `${restart}<div class="panel"><p>${L('These operational settings are stored in SQLite, strictly validated, and override the matching YAML values after restart. Repository changes continue to use the immediate Desired/Active validation workflow.', '这些运行配置保存在 SQLite 中，经过严格校验，并在重启后覆盖对应 YAML 值。仓库变更仍使用即时的 Desired/Active 验证流程。')}</p>${kv(L('Source', '来源'), response.source === 'web_ui' ? L('Web UI override', 'Web UI 覆盖') : L('Configuration file', '配置文件'))}<p class="muted">${L('File-only bootstrap settings:', '仅配置文件可管理的启动项：')} <code>${esc((response.file_only || []).join(', '))}</code></p></div>
    <form id="settings-form" class="settings-form">${groups}<footer><button type="button" class="secondary" id="reset-settings">${L('Reset to YAML after restart', '重启后恢复 YAML')}</button><button type="submit">${L('Validate and save', '验证并保存')}</button></footer><div id="settings-error" class="error"></div></form>`;
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
      notice(saved.restart_required ? L('Settings saved; restart RepoGate to apply them.', '设置已保存；重启 RepoGate 后生效。') : L('Settings already match the running process.', '设置已与当前进程一致。'));
      await loadSettings();
    } catch (error) { $('#settings-error').textContent = error.message; }
  });
  $('#reset-settings').addEventListener('click', async () => {
    if (!confirm(L('Discard the Web UI override and restore YAML values after restart?', '删除 Web UI 覆盖并在重启后恢复 YAML 值？'))) return;
    try { await api('/settings', {method: 'DELETE'}); notice(L('Web UI override removed; restart RepoGate.', 'Web UI 覆盖已删除；请重启 RepoGate。')); await loadSettings(); } catch (error) { $('#settings-error').textContent = error.message; }
  });
}

async function loadCluster() {
  const [overview, nodes] = await Promise.all([
    api('/cluster/overview').catch(() => ({role: 'standalone', enabled: false})),
    api('/cluster/nodes').catch(() => [])
  ]);

  const overviewHtml = `<div class="cards">
    ${card(L('Cluster role', '集群角色'), overview.role || 'standalone')}
    ${card(L('Cluster status', '集群状态'), overview.enabled ? L('Enabled', '已启用') : L('Disabled', '未启用'), overview.enabled)}
    ${card(L('Total nodes', '总节点数'), overview.total_nodes || 0)}
    ${card(L('Healthy nodes', '健康节点数'), overview.healthy_nodes || 0, (overview.healthy_nodes || 0) > 0)}
    ${card(L('Routable nodes', '可路由节点数'), overview.routable_nodes || 0, (overview.routable_nodes || 0) > 0)}
    ${card(L('Routing mode', '路由模式'), overview.routing_mode || 'hybrid')}
  </div>
  <div class="panel">
    <h2>${L('Cluster Fingerprint', '集群配置指纹')}</h2>
    <p><code>${esc(overview.cluster_fingerprint || L('Not initialized', '未初始化'))}</code></p>
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
        <button class="small secondary" data-action="check-node" data-id="${node.id}">${L('Check', '检查')}</button>
        <button class="small secondary" data-action="edit-node" data-id="${node.id}">${L('Edit', '编辑')}</button>
        <button class="small secondary" data-action="toggle-node" data-id="${node.id}" data-enabled="${node.enabled}">${node.enabled ? L('Disable', '禁用') : L('Enable', '启用')}</button>
        <button class="small danger" data-action="delete-node" data-id="${node.id}">${L('Delete', '删除')}</button>
      </td>
    </tr>`;
  }).join('');

  const tableHtml = `<div class="panel">
    <h2>${L('Edge nodes', '边缘节点')}</h2>
    <div class="table-wrap"><table><thead><tr>
      <th>${L('Name', '名称')}</th>
      <th>${L('URL', '基础 URL')}</th>
      <th>${L('Region', '地域')}</th>
      <th>${L('Priority / Weight', '优先级 / 权重')}</th>
      <th>${L('Health', '健康状态')}</th>
      <th>${L('Config', '配置一致性')}</th>
      <th>${L('Fingerprint', '指纹')}</th>
      <th>${L('Last check', '最后检查')}</th>
      <th>${L('Actions', '操作')}</th>
    </tr></thead><tbody>${nodeRows || `<tr><td colspan="9" class="empty">${L('No edge nodes registered yet.', '尚未注册边缘节点。')}</td></tr>`}</tbody></table></div>
  </div>`;

  $('#cluster-overview').innerHTML = overviewHtml;
  $('#cluster-node-list').innerHTML = tableHtml;
}

$('#add-node')?.addEventListener('click', () => {
  $('#node-form').reset();
  $('#node-id').value = '';
  $('#node-form-title').textContent = L('Add edge node', '新增边缘节点');
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
      notice(L('Node updated.', '节点已更新。'));
    } else {
      await api('/cluster/nodes', {method: 'POST', body: JSON.stringify(payload)});
      notice(L('Node added.', '节点已添加。'));
    }
    $('#node-dialog').close();
    await loadCluster();
  } catch (error) {
    $('#node-error').textContent = error.message;
  }
});

$('#reset-cluster-fp')?.addEventListener('click', async () => {
  if (!confirm(L('Reset the cluster configuration fingerprint? It will reinitialize from active nodes.', '重置集群配置指纹？它将从活动节点重新初始化。'))) return;
  try {
    await api('/cluster/fingerprint/reset', {method: 'POST'});
    notice(L('Cluster fingerprint reset.', '集群指纹已重置。'));
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
});

window.checkNode = async id => {
  try {
    await api(`/cluster/nodes/${id}/check`, {method: 'POST'});
    notice(L('Node probe completed.', '节点探测完成。'));
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
    $('#node-form-title').textContent = L('Edit edge node', '编辑边缘节点');
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
    notice(currentEnabled ? L('Node disabled.', '节点已禁用。') : L('Node enabled.', '节点已启用。'));
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
};

window.deleteNode = async id => {
  if (!confirm(L('Delete this edge node?', '删除此边缘节点？'))) return;
  try {
    await api(`/cluster/nodes/${id}`, {method: 'DELETE'});
    notice(L('Node deleted.', '节点已删除。'));
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
};

async function loadUsers() {
  const users = (await api('/users')) || [];
  $('#page-users').innerHTML = `<form class="panel narrow" id="user-form"><h2>${L('Add administrator', '新增管理员')}</h2><div class="form-grid"><label>${L('Username', '用户名')}<input id="new-user" minlength="3" maxlength="64" required></label><label>${L('Initial password', '初始密码')}<input id="new-user-pass" type="password" minlength="10" required></label></div><button>${L('Create user', '创建用户')}</button><div id="user-error" class="error"></div></form><div class="panel"><h2>${L('User list', '用户列表')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Username', '用户名')}</th><th>${L('Created', '创建时间')}</th><th>${L('Updated', '更新时间')}</th><th></th></tr></thead><tbody>${users.map(user => `<tr><td>${esc(user.username)}</td><td>${date(user.created_at)}</td><td>${date(user.updated_at)}</td><td><button class="danger" data-action="delete-user" data-id="${user.id}">${L('Delete', '删除')}</button></td></tr>`).join('')}</tbody></table></div></div>`;
  $('#user-form').addEventListener('submit', async event => { event.preventDefault(); try { await api('/users', {method: 'POST', body: JSON.stringify({username: $('#new-user').value, password: $('#new-user-pass').value})}); notice(L('User created.', '用户已创建。')); await loadUsers(); } catch (error) { $('#user-error').textContent = error.message; } });
}
window.deleteUser = async id => { if (!confirm(L('Delete this administrator account?', '删除此管理员账号？'))) return; try { await api(`/users/${id}`, {method: 'DELETE'}); notice(L('User deleted.', '用户已删除。')); await loadUsers(); } catch (error) { notice(error.message, true); } };
async function loadAccount() { $('#page-account').innerHTML = `<form class="panel narrow" id="password-form"><h2>${L('Change password', '修改密码')}</h2><label>${L('Current password', '当前密码')}<input id="old-pass" type="password" required></label><label>${L('New password (at least 10 characters)', '新密码（至少 10 位）')}<input id="new-pass" type="password" minlength="10" required></label><button>${L('Update password', '更新密码')}</button><div class="error" id="pass-error"></div></form>`; $('#password-form').addEventListener('submit', async event => { event.preventDefault(); try { await api('/auth/password', {method: 'PUT', body: JSON.stringify({current_password: $('#old-pass').value, new_password: $('#new-pass').value})}); notice(L('Password updated.', '密码已更新。')); event.target.reset(); } catch (error) { $('#pass-error').textContent = error.message; } }); }

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
    default: throw new Error(L('Unknown action.', '未知操作。'));
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

applyLanguage(language);
boot();
