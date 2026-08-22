// System page: concise service summary with secondary runtime and endpoint
// information grouped into native disclosure sections.
import { api } from '../api.js';
import { card, disclosure, kv } from '../components.js';
import { $ } from '../dom.js';
import { bytes, duration, exitSummary, number, stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';
import { triggerRestart } from '../restart.js';

export async function loadSystem() {
  const [system, dashboard] = await Promise.all([api('/system'), api('/stats')]);
  const runtime = dashboard.stats.runtime || {};
  const upstreamNginx = system.upstream_nginx || {};
  const nginxRunning = upstreamNginx.state === 'running';

  const mirrorRelayDetails = [
    kv(L('Program version'), system.version),
    kv(L('Build ID'), system.build_id),
    kv(L('Architecture'), `${system.target_os}/${system.architecture}`),
    kv(L('Go version'), system.go_version),
    kv(L('Uptime'), duration(system.uptime_seconds)),
    kv(L('Public base URL'), system.public_base_url || L('Not configured'))
  ].join('');

  const runtimeDetails = [
    kv(L('Go heap allocated'), bytes(runtime.heap_alloc_bytes)),
    kv(L('Go heap in use'), bytes(runtime.heap_inuse_bytes)),
    kv(L('Go heap objects'), number(runtime.heap_objects)),
    kv(L('Total allocations'), bytes(runtime.total_alloc_bytes)),
    kv('Mallocs / Frees', `${number(runtime.mallocs)} / ${number(runtime.frees)}`),
    kv('RSS', bytes(runtime.rss_bytes)),
    kv(L('Goroutines'), number(runtime.goroutines)),
    kv(L('Open file descriptors'), number(runtime.open_fds)),
    kv(L('GC cycles'), number(runtime.gc_count)),
    kv(L('GC pause total'), `${((runtime.gc_pause_total_ns || 0) / 1e9).toFixed(3)} s`),
    kv(L('GC CPU fraction'), `${((runtime.gc_cpu_fraction || 0) * 100).toFixed(3)}%`)
  ].join('');

  const ingressDetails = [
    kv(L('Ingress mode'), system.ingress_mode),
    kv(L('HTTPS listen'), system.https_listen),
    kv(L('Minimum TLS'), system.tls_min_version),
    system.ingress_mode === 'managed-standalone' ? kv(L('Certificate'), system.tls_certificate) : '',
    system.ingress_mode === 'managed-standalone' ? kv(L('Private key'), system.tls_private_key) : '',
    kv(L('Frontend endpoint'), `${system.frontend_network} · ${system.frontend_address}`),
    kv(L('Upstream endpoint'), `${system.upstream_network} · ${system.upstream_address}`),
    kv(L('Zero-Copy Acceleration'), system.zero_copy_bypass ? `${L('Adaptive Active')} (X-Accel-Redirect)` : L('Disabled'))
  ].join('');

  const nginxDetails = [
    kv(L('Mode'), upstreamNginx.mode || '—'),
    kv(L('State'), stateLabel(upstreamNginx.state)),
    kv(L('Uptime'), duration(upstreamNginx.uptime_seconds || 0)),
    kv(L('Last exit'), exitSummary(upstreamNginx)),
    `<p class="muted">${L('Binary identity and compile options are available from Technical details on the Managed Upstream Nginx page.')}</p>`,
    `<p class="muted">${L('Repository changes become active only after candidate generation, nginx -t, atomic publication and graceful reload.')}</p>`
  ].join('');

  $('#page-system').innerHTML = `
    <div class="toolbar end">
      <button type="button" class="btn-restart-inline requires-admin" id="restart-system-btn">
        ${icon('restart', 13)} ${L('Restart service')}
      </button>
    </div>
    <div class="cards compact-cards">
      ${card(L('MirrorRelay uptime'), duration(system.uptime_seconds), false, 'activity')}
      ${card('RSS', bytes(runtime.rss_bytes), false, 'cpu')}
      ${card(L('Managed Upstream Nginx'), stateLabel(upstreamNginx.state), nginxRunning, 'server')}
    </div>
    <div class="disclosure-stack">
      ${disclosure('MirrorRelay', mirrorRelayDetails, {
        iconName: 'layers',
        description: L('Version, build and public identity')
      })}
      ${disclosure(L('Runtime resources'), runtimeDetails, {
        iconName: 'cpu',
        description: L('Memory, file descriptors and Go runtime counters')
      })}
      ${disclosure(L('TLS / Ingress'), ingressDetails, {
        iconName: 'shield',
        description: L('Listener, certificate and local endpoint details')
      })}
      ${disclosure(L('Managed Upstream Nginx lifecycle'), nginxDetails, {
        iconName: 'server',
        description: L('Process state and activation guarantees')
      })}
    </div>`;

  $('#restart-system-btn')?.addEventListener('click', triggerRestart);
}
