// Dashboard page: fleet overview, topology, real-time SVG charts, cache usage and per-repository stats.
import { api } from '../api.js';
import { $, esc } from '../dom.js';
import { card, kv } from '../components.js';
import { renderAreaChart, renderDonutChart } from '../charts.js';
import { bytes, duration, number, stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';
import { activeUpstreamFor, healthFor } from '../repositories.js';
import { state } from '../state.js';

export async function loadDashboard() {
  const [dashboard, upstreamNginx, repositoryValues] = await Promise.all([
    api('/stats'),
    api('/upstream-nginx/status'),
    api('/mirrors')
  ]);
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
    const isHealthy = health === 'healthy';
    return `<tr>
      <td>
        <button class="text-button" data-action="show-repository" data-id="${repository.id}">
          <strong>${esc(repository.name)}</strong>
        </button>
        <small>${esc(repository.slug)}</small>
      </td>
      <td>
        <span class="badge ${isHealthy ? 'ok' : (health === 'disabled' ? '' : (health === 'unknown' ? 'yellow' : 'bad'))}">
          ${health === 'disabled' ? '' : `<span class="pulse-dot ${isHealthy ? 'green' : 'red'}"></span>`}
          ${esc(stateLabel(health))}
        </span>
      </td>
      <td><strong>${number(counters.requests || 0)}</strong></td>
      <td>${bytes(counters.bytes || 0)}</td>
      <td><code>${upstream.latency_ms ? `${number(upstream.latency_ms)} ms` : '—'}</code></td>
      <td><span class="ratio-chip">${number(counters.cache_hits || 0)} <small>/</small> ${number(counters.cache_misses || 0)}</span></td>
      <td><span class="status-codes">${number(counters.status_2xx || 0)} / ${number(counters.status_3xx || 0)} / ${number(counters.status_4xx || 0)} / ${number(counters.status_5xx || 0)}</span></td>
      <td>${counters.upstream_errors ? `<span class="badge bad">${number(counters.upstream_errors)}</span>` : '<span class="text-muted">0</span>'}</td>
    </tr>`;
  }).join('');

  const isNginxRunning = upstreamNginx.state === 'running';

  const topoHTML = `<div class="topology-pipeline">
    <div class="topo-node active">
      <div class="node-icon-wrap">${icon('users', 20)}</div>
      <div class="node-content">
        <span class="node-title">Clients</span>
        <span class="node-sub">HTTP / HTTPS</span>
      </div>
      <span class="node-protocol">Public</span>
    </div>
    <div class="topo-arrow">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>
    </div>
    <div class="topo-node active">
      <div class="node-icon-wrap">${icon('network', 20)}</div>
      <div class="node-content">
        <span class="node-title">External Ingress</span>
        <span class="node-sub">Shared Nginx</span>
      </div>
      <span class="node-protocol">Port 80/443</span>
    </div>
    <div class="topo-arrow">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>
    </div>
    <div class="topo-node active">
      <div class="node-icon-wrap primary">${icon('layers', 20)}</div>
      <div class="node-content">
        <span class="node-title">MirrorRelay</span>
        <span class="node-sub">Control Plane v${esc(dashboard.version || '0.0.5')}</span>
      </div>
      <span class="node-protocol">UNIX Socket</span>
    </div>
    <div class="topo-arrow">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>
    </div>
    <div class="topo-node ${isNginxRunning ? 'active' : 'inactive'}">
      <div class="node-icon-wrap ${isNginxRunning ? 'success' : 'danger'}">${icon('server', 20)}</div>
      <div class="node-content">
        <span class="node-title">Managed Upstream Nginx</span>
        <span class="node-sub">PID ${esc(upstreamNginx.pid || '—')}</span>
      </div>
      <span class="node-protocol">${isNginxRunning ? 'Active' : 'Offline'}</span>
    </div>
    <div class="topo-arrow">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>
    </div>
    <div class="topo-node active">
      <div class="node-icon-wrap">${icon('globe', 20)}</div>
      <div class="node-content">
        <span class="node-title">Upstream Origins</span>
        <span class="node-sub">${dashboard.enabled_mirrors || 0} ${L('Repositories')}</span>
      </div>
      <span class="node-protocol">HTTPS / TLS</span>
    </div>
  </div>`;

  // Prepare chart series from hourly data
  const hourlyData = (dashboard.stats.hourly || []).map(b => {
    const hourStr = (b.hour || '').slice(-2) + ':00';
    return {
      label: hourStr,
      requests: b.counters?.requests || 0,
      bytes: b.counters?.bytes || 0,
      hits: b.counters?.cache_hits || 0
    };
  });

  const chartRequestsHTML = renderAreaChart({
    title: `${L('Hourly Requests (24h)')}`,
    data: hourlyData,
    xLabel: item => item.label,
    yValue: item => item.requests,
    color: '#38bdf8',
    gradientId: 'req-grad',
    unit: 'req'
  });

  const chartTrafficHTML = renderAreaChart({
    title: `${L('Hourly Traffic (24h)')}`,
    data: hourlyData,
    xLabel: item => item.label,
    yValue: item => Math.round((item.bytes || 0) / (1024 * 1024)),
    color: '#10b981',
    gradientId: 'traf-grad',
    unit: 'MB'
  });

  const statusDonutHTML = renderDonutChart({
    title: L('HTTP Status Breakdown (Today)'),
    slices: [
      { label: '2xx OK', value: today.status_2xx || 0, color: '#10b981' },
      { label: '3xx Redirect', value: today.status_3xx || 0, color: '#38bdf8' },
      { label: '4xx Client Err', value: today.status_4xx || 0, color: '#f59e0b' },
      { label: '5xx Upstream Err', value: today.status_5xx || 0, color: '#f43f5e' }
    ]
  });

  const cacheDonutHTML = renderDonutChart({
    title: L('Cache Hit Distribution (Today)'),
    slices: [
      { label: 'Cache HIT', value: today.cache_hits || 0, color: '#10b981' },
      { label: 'Cache MISS', value: today.cache_misses || 0, color: '#64748b' }
    ]
  });

  $('#page-dashboard').innerHTML = topoHTML + `<div class="cards">
    ${card(L('Repositories / enabled'), `${dashboard.mirrors} / ${dashboard.enabled_mirrors}`, false, 'layers', `${dashboard.healthy_mirrors || 0} healthy`)}
    ${card(L('Healthy / unhealthy'), `${dashboard.healthy_mirrors || 0} / ${dashboard.unhealthy_mirrors || 0}`, dashboard.unhealthy_mirrors === 0, 'activity', dashboard.unhealthy_mirrors === 0 ? 'All systems nominal' : `${dashboard.unhealthy_mirrors} degraded`)}
    ${card(L('Managed Upstream Nginx'), stateLabel(upstreamNginx.state), isNginxRunning, 'server', `PID ${upstreamNginx.pid || '—'}`)}
    ${card(L('Active requests'), number(dashboard.stats.active_requests), false, 'zap', 'Current in-flight')}
    ${card(L('Requests today'), number(today.requests), false, 'trend-up', 'Total requests')}
    ${card(L('Traffic today'), bytes(today.bytes), false, 'ingress', 'Served today')}
    ${card(L('Traffic / 24 h'), bytes(last24.bytes), false, 'access', 'Rolling 24 hours')}
    ${card(L('Traffic / 7 d'), bytes(last7.bytes), false, 'system', 'Rolling 7 days')}
    ${card(L('Cache hit rate'), `${hitRate.toFixed(1)}%`, hitRate > 50, 'cache', `${number(today.cache_hits)} hits / ${number(today.cache_misses)} misses`)}
  </div>
  <div class="grid2">
    <div class="panel">
      <h2>${icon('trend-up', 18)} ${L('Performance & Traffic Analytics (24h)')}</h2>
      <div class="charts-grid">
        ${chartRequestsHTML}
        ${chartTrafficHTML}
      </div>
    </div>
    <div class="panel">
      <h2>${icon('activity', 18)} ${L('Traffic & Cache Breakdown')}</h2>
      <div class="charts-grid">
        ${statusDonutHTML}
        ${cacheDonutHTML}
      </div>
    </div>
  </div>
  <div class="grid2">
    <div class="panel">
      <h2>${icon('cache', 18)} ${L('Cache usage')}</h2>
      <div class="bar-row">
        <span>${number(cache.files)} ${L('files')}</span>
        <div class="bar"><i style="width:${maximum ? Math.min(100, 100 * cache.bytes / maximum) : 0}%"></i></div>
        <span class="bar-percent">${maximum ? (100 * cache.bytes / maximum).toFixed(1) : '0.0'}%</span>
      </div>
      <p class="muted">${bytes(cache.bytes)} / ${bytes(maximum)}</p>
    </div>
    <div class="panel">
      <h2>${icon('server', 18)} ${L('MirrorRelay and Managed Upstream Nginx')}</h2>
      ${kv(L('Managed Upstream Nginx PID'), upstreamNginx.pid || '—')}
      ${kv(L('Managed Upstream Nginx version'), upstreamNginx.version || '—')}
      ${kv(L('Managed Upstream Nginx build ID'), upstreamNginx.build_id || '—')}
      ${kv(L('Managed Upstream Nginx architecture'), upstreamNginx.architecture || '—')}
      ${kv(L('Managed Upstream Nginx uptime'), duration(upstreamNginx.uptime_seconds || 0))}
      ${kv(L('MirrorRelay version'), dashboard.version || '—')}
      ${kv(L('MirrorRelay build ID'), dashboard.build_id || '—')}
      ${kv(L('MirrorRelay architecture'), dashboard.architecture || '—')}
      ${kv(L('Active config'), `v${upstreamNginx.current_config_version || '—'}`)}
      ${kv(L('MirrorRelay uptime'), duration(dashboard.uptime_seconds))}
    </div>
  </div>
  <div class="panel">
    <h2>${icon('layers', 18)} ${L('Repository statistics today')}</h2>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>${L('Repository')}</th>
            <th>${L('Health')}</th>
            <th>${L('Requests')}</th>
            <th>${L('Traffic')}</th>
            <th>${L('Latency')}</th>
            <th>${L('Cache HIT / MISS')}</th>
            <th>2xx / 3xx / 4xx / 5xx</th>
            <th>${L('Upstream errors')}</th>
          </tr>
        </thead>
        <tbody>
          ${repositoryRows || `<tr><td colspan="8" class="empty">${L('No repositories yet.')}</td></tr>`}
        </tbody>
      </table>
    </div>
  </div>`;
}
