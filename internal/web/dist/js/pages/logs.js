// Access and audit log pages.
import { api } from '../api.js';
import { $, esc } from '../dom.js';
import { date } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';

export async function loadAccess() {
  const lines = await api('/access');
  $('#page-access').innerHTML = `
    <div class="panel">
      <div class="toolbar">
        <h2>${icon('file-text', 18)} access.log</h2>
        <button id="refresh-access" class="secondary">${icon('refresh', 13)} ${L('Refresh')}</button>
      </div>
      <pre class="config-preview">${esc((lines || []).join('\n') || L('No access records.'))}</pre>
    </div>`;
  $('#refresh-access').addEventListener('click', loadAccess);
}

export async function loadAudit() {
  const entries = (await api('/audit')) || [];
  $('#page-audit').innerHTML = `
    <div class="panel">
      <h2>${icon('shield', 18)} ${L('Audit log')}</h2>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${L('Time')}</th>
              <th>${L('User')}</th>
              <th>${L('Client')}</th>
              <th>${L('Action')}</th>
              <th>${L('Object / detail')}</th>
              <th>${L('Result')}</th>
            </tr>
          </thead>
          <tbody>
            ${entries.map(entry => `<tr>
              <td><code>${date(entry.time)}</code></td>
              <td><strong>${esc(entry.username)}</strong></td>
              <td><code>${esc(entry.client_ip)}</code></td>
              <td><span class="badge blue">${esc(entry.action)}</span></td>
              <td>${esc(entry.object)} <span class="text-muted">${esc(entry.detail)}</span></td>
              <td>
                <span class="badge ${entry.succeeded ? 'ok' : 'bad'}">
                  <span class="pulse-dot ${entry.succeeded ? 'green' : 'red'}"></span>
                  ${entry.succeeded ? L('Success') : L('Failed')}
                </span>
              </td>
            </tr>`).join('')}
          </tbody>
        </table>
      </div>
    </div>`;
}
