// Dashboard page: fleet overview, topology, cache usage and per-repository stats.
import { api } from '../api.js';
import { $, esc } from '../dom.js';
import { card, kv } from '../components.js';
import { bytes, duration, number, stateLabel } from '../format.js';
import { L } from '../i18n.js';
import { activeUpstreamFor, healthFor } from '../repositories.js';
import { state } from '../state.js';

export async function loadDashboard() {
  const [dashboard, upstreamNginx, repositoryValues] = await Promise.all([api('/stats'), api('/upstream-nginx/status'), api('/mirrors')]);
  const repositories = repositoryValues || [];
  state.mirrors = repositories;
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
  const topoHTML = `<div class="topology-pipeline">
    <div class="topo-node active"><span class="node-title">Clients</span><span class="node-sub">HTTP(S) Traffic</span></div>
    <div class="topo-arrow">➔</div>
    <div class="topo-node active"><span class="node-title">External Ingress</span><span class="node-sub">Shared Nginx</span></div>
    <div class="topo-arrow">➔</div>
    <div class="topo-node active"><span class="node-title">MirrorRelay</span><span class="node-sub">v${esc(dashboard.version || '0.0.1')}</span></div>
    <div class="topo-arrow">➔</div>
    <div class="topo-node ${upstreamNginx.state === 'running' ? 'active' : ''}"><span class="node-title">Managed Upstream Nginx</span><span class="node-sub">PID ${esc(upstreamNginx.pid || '—')}</span></div>
    <div class="topo-arrow">➔</div>
    <div class="topo-node active"><span class="node-title">Upstream Origins</span><span class="node-sub">${dashboard.enabled_mirrors || 0} ${L('Repositories')}</span></div>
  </div>`;
  $('#page-dashboard').innerHTML = topoHTML + `<div class="cards">
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
