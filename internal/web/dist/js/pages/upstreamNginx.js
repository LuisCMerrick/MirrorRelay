// Managed Upstream Nginx page: status, configuration history, rollback and
// the effective configuration preview.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { card, kv, showPreview } from '../components.js';
import { $, esc, notice } from '../dom.js';
import { date, duration, exitSummary, stateLabel } from '../format.js';
import { L } from '../i18n.js';
import { loadMirrors } from './mirrors.js';

export async function loadUpstreamNginx() {
  try {
    const [status, config, history] = await Promise.all([api('/upstream-nginx/status'), api('/upstream-nginx/config'), api('/upstream-nginx/history')]);
    $('#page-upstream-nginx').innerHTML = `<div class="cards">${card(L('State'), stateLabel(status.state), status.state === 'running')}${card('PID', status.pid || '—')}${card(L('Uptime'), duration(status.uptime_seconds || 0))}${card(L('Config version'), `v${status.current_config_version || '—'}`)}${card(L('Managed Upstream Nginx version'), (status.version || '—').replace(/^nginx version:\s*/, ''))}${card(L('Build ID'), status.build_id || '—')}${card(L('Architecture'), status.architecture || '—')}</div>
      ${status.last_error ? `<div class="notice error">${esc(status.last_error)}</div>` : ''}<div class="toolbar"><div>${status.integration_snippet ? `<span class="muted">${L('Integration snippet')}: ${esc(status.integration_snippet)} · ${esc(status.integration_result || '')}</span>` : ''}</div><button id="reload-upstream-nginx">${L('Regenerate, validate and reload')}</button></div>
      <div class="grid2"><div class="panel"><h2>${L('Configuration history')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Version')}</th><th>${L('Time')}</th><th>${L('Operator')}</th><th>${L('Description')}</th><th>${L('State')}</th><th></th></tr></thead><tbody>${history.map(item => `<tr><td>v${item.version}</td><td>${date(item.created_at)}</td><td>${esc(item.operator)}</td><td>${esc(item.description)}</td><td><span class="badge ${item.active ? 'ok' : ''}">${item.active ? L('Active') : L('History')}</span></td><td>${item.active ? '' : `<button data-action="rollback-config" data-version="${item.version}">${L('Rollback')}</button>`}</td></tr>`).join('')}</tr></thead></table></div></div><div class="panel"><h2>${L('Runtime and build')}</h2>${kv(L('Last reload'), status.last_reload ? date(status.last_reload) : '—')}${kv(L('Reload result'), status.last_reload_result || '—')}${kv(L('Last exit'), exitSummary(status))}<pre class="config-preview">${esc(status.build_options || L('Build options unavailable.'))}</pre></div><div class="panel"><h2>${L('Effective configuration')}</h2><pre class="config-preview">${esc(config.configuration)}</pre></div></div>`;
    $('#reload-upstream-nginx').addEventListener('click', async () => { try { await api('/upstream-nginx/reload', {method: 'POST'}); notice(L('Validation passed and Managed Upstream Nginx reloaded.')); await loadUpstreamNginx(); } catch (error) { notice(error.message, true); } });
  } catch (error) { $('#page-upstream-nginx').innerHTML = `<div class="notice error">${esc(error.message)}</div>`; }
}

registerAction('rollback-config', async button => {
  const version = Number(button.dataset.version);
  if (!confirm(L('Rollback repositories and custom configuration to v%s?', version))) return;
  try { await api(`/upstream-nginx/history/${version}/rollback`, {method: 'POST'}); notice(L('Rolled back through a validated graceful reload.')); await Promise.all([loadUpstreamNginx(), loadMirrors()]); } catch (error) { notice(error.message, true); }
});

registerAction('view-effective-config', async () => {
  try {
    const value = await api('/upstream-nginx/config');
    showPreview(L('Effective Managed Upstream Nginx configuration'), `<pre class="config-preview">${esc(value.configuration)}</pre>`);
  } catch (error) { notice(error.message, true); }
});
