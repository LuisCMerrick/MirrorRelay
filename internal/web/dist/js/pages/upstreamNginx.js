// Managed Upstream Nginx page: status, configuration history, rollback and
// the effective configuration preview.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { card, kv, showPreview } from '../components.js';
import { $, esc, notice } from '../dom.js';
import { date, duration, exitSummary, stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';
import { loadMirrors } from './mirrors.js';

export async function loadUpstreamNginx() {
  try {
    const [status, config, history] = await Promise.all([
      api('/upstream-nginx/status'),
      api('/upstream-nginx/config'),
      api('/upstream-nginx/history')
    ]);

    const isRunning = status.state === 'running';

    $('#page-upstream-nginx').innerHTML = `
      <div class="cards">
        ${card(L('State'), stateLabel(status.state), isRunning, 'server', isRunning ? 'Process active' : 'Offline')}
        ${card('PID', status.pid || '—', false, 'cpu')}
        ${card(L('Uptime'), duration(status.uptime_seconds || 0), false, 'activity')}
        ${card(L('Config version'), `v${status.current_config_version || '—'}`, false, 'code')}
        ${card(L('Managed Upstream Nginx version'), (status.version || '—').replace(/^nginx version:\s*/, ''), false, 'layers')}
        ${card(L('Build ID'), status.build_id || '—', false, 'settings')}
        ${card(L('Architecture'), status.architecture || '—', false, 'globe')}
      </div>
      ${status.last_error ? `<div class="notice error">${icon('alert', 16)} ${esc(status.last_error)}</div>` : ''}
      <div class="toolbar">
        <div>
          ${status.integration_snippet ? `<span class="muted">${L('Integration snippet')}: <code>${esc(status.integration_snippet)}</code> · ${esc(status.integration_result || '')}</span>` : ''}
        </div>
        <button id="reload-upstream-nginx" class="btn-primary">
          ${icon('refresh', 14)} ${L('Regenerate, validate and reload')}
        </button>
      </div>
      <div class="grid2">
        <div class="panel">
          <h2>${icon('shield', 18)} ${L('Configuration history')}</h2>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>${L('Version')}</th>
                  <th>${L('Time')}</th>
                  <th>${L('Operator')}</th>
                  <th>${L('Description')}</th>
                  <th>${L('State')}</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                ${history.map(item => `<tr>
                  <td><code>v${item.version}</code></td>
                  <td>${date(item.created_at)}</td>
                  <td><span class="badge blue">${esc(item.operator)}</span></td>
                  <td>${esc(item.description)}</td>
                  <td><span class="badge ${item.active ? 'ok' : ''}">${item.active ? L('Active') : L('History')}</span></td>
                  <td>${item.active ? '' : `<button class="small secondary" data-action="rollback-config" data-version="${item.version}">${icon('refresh', 12)} ${L('Rollback')}</button>`}</td>
                </tr>`).join('')}
              </tbody>
            </table>
          </div>
        </div>
        <div class="panel">
          <h2>${icon('cpu', 18)} ${L('Runtime and build')}</h2>
          ${kv(L('Last reload'), status.last_reload ? date(status.last_reload) : '—')}
          ${kv(L('Reload result'), status.last_reload_result || '—')}
          ${kv(L('Last exit'), exitSummary(status))}
          <pre class="config-preview">${esc(status.build_options || L('Build options unavailable.'))}</pre>
        </div>
      </div>
      <div class="panel">
        <h2>${icon('code', 18)} ${L('Effective configuration')}</h2>
        <pre class="config-preview">${esc(config.configuration)}</pre>
      </div>`;

    $('#reload-upstream-nginx').addEventListener('click', async () => {
      try {
        await api('/upstream-nginx/reload', {method: 'POST'});
        notice(L('Validation passed and Managed Upstream Nginx reloaded.'));
        await loadUpstreamNginx();
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
    showPreview(L('Effective Managed Upstream Nginx configuration'), `<pre class="config-preview">${esc(value.configuration)}</pre>`);
  } catch (error) {
    notice(error.message, true);
  }
});
