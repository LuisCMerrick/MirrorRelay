// System page: build information, runtime resources, TLS/ingress and the
// Managed Upstream Nginx summary.
import { api } from '../api.js';
import { kv } from '../components.js';
import { $ } from '../dom.js';
import { bytes, duration, exitSummary, number, stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';
import { triggerRestart } from '../restart.js';

export async function loadSystem() {
  const [system, dashboard] = await Promise.all([api('/system'), api('/stats')]);
  const runtime = dashboard.stats.runtime || {}, upstreamNginx = system.upstream_nginx || {};

  $('#page-system').innerHTML = `
    <div class="grid2">
      <div class="panel">
        <div class="panel-header-row">
          <h2>${icon('layers', 18)} MirrorRelay</h2>
          <button type="button" class="btn-restart-inline" id="restart-system-btn">
            ${icon('restart', 13)} ${L('Restart service')}
          </button>
        </div>
        ${kv(L('Program version'), system.version)}
        ${kv(L('Build ID'), system.build_id)}
        ${kv(L('Architecture'), `${system.target_os}/${system.architecture}`)}
        ${kv(L('Go version'), system.go_version)}
        ${kv(L('Uptime'), duration(system.uptime_seconds))}
        ${kv(L('Public base URL'), system.public_base_url || L('Not configured'))}
      </div>
      <div class="panel">
        <h2>${icon('cpu', 18)} ${L('Runtime resources')}</h2>
        ${kv(L('Go heap allocated'), bytes(runtime.heap_alloc_bytes))}
        ${kv(L('Go heap in use'), bytes(runtime.heap_inuse_bytes))}
        ${kv(L('Go heap objects'), number(runtime.heap_objects))}
        ${kv(L('Total allocations'), bytes(runtime.total_alloc_bytes))}
        ${kv('Mallocs / Frees', `${number(runtime.mallocs)} / ${number(runtime.frees)}`)}
        ${kv('RSS', bytes(runtime.rss_bytes))}
        ${kv(L('Goroutines'), number(runtime.goroutines))}
        ${kv(L('Open file descriptors'), number(runtime.open_fds))}
        ${kv(L('GC cycles'), number(runtime.gc_count))}
        ${kv(L('GC pause total'), `${((runtime.gc_pause_total_ns || 0) / 1e9).toFixed(3)} s`)}
        ${kv(L('GC CPU fraction'), `${((runtime.gc_cpu_fraction || 0) * 100).toFixed(3)}%`)}
      </div>
    </div>
    <div class="grid2">
      <div class="panel">
        <h2>${icon('shield', 18)} ${L('TLS / Ingress')}</h2>
        ${kv(L('Ingress mode'), system.ingress_mode)}
        ${kv(L('HTTPS listen'), system.https_listen)}
        ${kv(L('Minimum TLS'), system.tls_min_version)}
        ${system.ingress_mode === 'managed-standalone' ? kv(L('Certificate'), system.tls_certificate) + kv(L('Private key'), system.tls_private_key) : ''}
        ${kv(L('Frontend endpoint'), `${system.frontend_network} · ${system.frontend_address}`)}
        ${kv(L('Upstream endpoint'), `${system.upstream_network} · ${system.upstream_address}`)}
      </div>
      <div class="panel">
        <h2>${icon('server', 18)} ${L('Managed Upstream Nginx')}</h2>
        ${kv(L('Mode'), upstreamNginx.mode)}
        ${kv(L('State'), stateLabel(upstreamNginx.state))}
        ${kv(L('Version'), upstreamNginx.version || '—')}
        ${kv(L('Build ID'), upstreamNginx.build_id || '—')}
        ${kv(L('Architecture'), upstreamNginx.architecture || '—')}
        ${kv('SHA-256', upstreamNginx.sha256 || '—')}
        ${kv(L('Uptime'), duration(upstreamNginx.uptime_seconds || 0))}
        ${kv(L('Last exit'), exitSummary(upstreamNginx))}
        <p class="muted">${L('Repository changes become active only after candidate generation, nginx -t, atomic publication and graceful reload.')}</p>
      </div>
    </div>`;

  const restartSysBtn = $('#restart-system-btn');
  if (restartSysBtn) restartSysBtn.addEventListener('click', triggerRestart);
}
