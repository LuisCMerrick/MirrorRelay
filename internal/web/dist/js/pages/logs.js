// Access and audit log pages.
import { api } from '../api.js';
import { $, esc } from '../dom.js';
import { date } from '../format.js';
import { L } from '../i18n.js';

export async function loadAccess() {
  const lines = await api('/access');
  $('#page-access').innerHTML = `<div class="panel"><div class="toolbar"><h2>access.log</h2><button id="refresh-access">${L('Refresh')}</button></div><pre class="config-preview">${esc((lines || []).join('\n') || L('No access records.'))}</pre></div>`;
  $('#refresh-access').addEventListener('click', loadAccess);
}

export async function loadAudit() {
  const entries = (await api('/audit')) || [];
  $('#page-audit').innerHTML = `<div class="table-wrap"><table><thead><tr><th>${L('Time')}</th><th>${L('User')}</th><th>${L('Client')}</th><th>${L('Action')}</th><th>${L('Object / detail')}</th><th>${L('Result')}</th></tr></thead><tbody>${entries.map(entry => `<tr><td>${date(entry.time)}</td><td>${esc(entry.username)}</td><td>${esc(entry.client_ip)}</td><td>${esc(entry.action)}</td><td>${esc(entry.object)} ${esc(entry.detail)}</td><td><span class="badge ${entry.succeeded ? 'ok' : 'bad'}">${entry.succeeded ? L('Success') : L('Failed')}</span></td></tr>`).join('')}</tbody></table></div>`;
}
