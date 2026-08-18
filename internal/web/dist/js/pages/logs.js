// Access and audit log pages with live stream mode and interactive filtering.
import { api } from '../api.js';
import { $, esc } from '../dom.js';
import { date } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';

let accessStreamTimer = null;
let auditStreamTimer = null;
let rawAccessLines = [];
let rawAuditEntries = [];

export async function loadAccess() {
  stopAccessStream();
  rawAccessLines = (await api('/access')) || [];
  renderAccessPage();
}

function renderAccessPage() {
  const isStreaming = Boolean(accessStreamTimer);
  const filterQuery = ($('#access-filter-input')?.value || '').toLowerCase().trim();

  const filteredLines = rawAccessLines.filter(line => {
    if (!filterQuery) return true;
    return line.toLowerCase().includes(filterQuery);
  });

  $('#page-access').innerHTML = `
    <div class="panel">
      <div class="toolbar">
        <div class="log-toolbar-left">
          <h2>${icon('file-text', 18)} access.log</h2>
          <span class="live-stream-badge ${isStreaming ? 'active' : ''}">
            <span class="pulse-dot ${isStreaming ? 'green' : 'red'}"></span>
            ${isStreaming ? L('Live Streaming') : L('Static View')}
          </span>
        </div>
        <div class="log-toolbar-right actions">
          <div class="search-bar log-search-bar">
            <svg class="search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" x2="16.65" y1="21" y2="16.65"/></svg>
            <input type="text" id="access-filter-input" placeholder="${L('Filter log lines (IP, status, path)...')}" value="${esc(filterQuery)}" />
          </div>
          <button id="toggle-access-stream" class="${isStreaming ? 'btn-primary' : 'secondary'}">
            ${icon('zap', 13)} ${isStreaming ? L('Stop Stream') : L('Start Live Stream')}
          </button>
          <button id="refresh-access" class="secondary">${icon('refresh', 13)} ${L('Refresh')}</button>
        </div>
      </div>
      <pre id="access-log-pre" class="config-preview log-terminal">${esc(filteredLines.join('\n') || L('No access records.'))}</pre>
    </div>`;

  const pre = $('#access-log-pre');
  if (pre) pre.scrollTop = pre.scrollHeight;

  $('#access-filter-input')?.addEventListener('input', () => {
    const q = ($('#access-filter-input')?.value || '').toLowerCase().trim();
    const fl = rawAccessLines.filter(l => !q || l.toLowerCase().includes(q));
    const p = $('#access-log-pre');
    if (p) p.textContent = fl.join('\n') || L('No matching log lines.');
  });

  $('#refresh-access')?.addEventListener('click', async () => {
    rawAccessLines = (await api('/access')) || [];
    renderAccessPage();
  });

  $('#toggle-access-stream')?.addEventListener('click', () => {
    if (accessStreamTimer) {
      stopAccessStream();
      renderAccessPage();
    } else {
      startAccessStream();
      renderAccessPage();
    }
  });
}

function startAccessStream() {
  stopAccessStream();
  accessStreamTimer = setInterval(async () => {
    try {
      rawAccessLines = (await api('/access')) || [];
      const q = ($('#access-filter-input')?.value || '').toLowerCase().trim();
      const fl = rawAccessLines.filter(l => !q || l.toLowerCase().includes(q));
      const p = $('#access-log-pre');
      if (p) {
        p.textContent = fl.join('\n') || L('No access records.');
        p.scrollTop = p.scrollHeight;
      }
    } catch (_) {}
  }, 2000);
}

function stopAccessStream() {
  if (accessStreamTimer) {
    clearInterval(accessStreamTimer);
    accessStreamTimer = null;
  }
}

export async function loadAudit() {
  stopAuditStream();
  rawAuditEntries = (await api('/audit')) || [];
  renderAuditPage();
}

function renderAuditPage() {
  const isStreaming = Boolean(auditStreamTimer);
  const filterQuery = ($('#audit-filter-input')?.value || '').toLowerCase().trim();

  const filtered = rawAuditEntries.filter(entry => {
    if (!filterQuery) return true;
    return (entry.username || '').toLowerCase().includes(filterQuery) ||
           (entry.client_ip || '').toLowerCase().includes(filterQuery) ||
           (entry.action || '').toLowerCase().includes(filterQuery) ||
           (entry.object || '').toLowerCase().includes(filterQuery) ||
           (entry.detail || '').toLowerCase().includes(filterQuery);
  });

  $('#page-audit').innerHTML = `
    <div class="panel">
      <div class="toolbar">
        <div class="log-toolbar-left">
          <h2>${icon('shield', 18)} ${L('Audit log')}</h2>
          <span class="live-stream-badge ${isStreaming ? 'active' : ''}">
            <span class="pulse-dot ${isStreaming ? 'green' : 'red'}"></span>
            ${isStreaming ? L('Live Streaming') : L('Static View')}
          </span>
        </div>
        <div class="log-toolbar-right actions">
          <div class="search-bar log-search-bar">
            <svg class="search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" x2="16.65" y1="21" y2="16.65"/></svg>
            <input type="text" id="audit-filter-input" placeholder="${L('Filter audit (User, Action, IP)...')}" value="${esc(filterQuery)}" />
          </div>
          <button id="toggle-audit-stream" class="${isStreaming ? 'btn-primary' : 'secondary'}">
            ${icon('zap', 13)} ${isStreaming ? L('Stop Stream') : L('Start Live Stream')}
          </button>
          <button id="refresh-audit" class="secondary">${icon('refresh', 13)} ${L('Refresh')}</button>
        </div>
      </div>
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
            ${filtered.map(entry => `<tr>
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

  $('#audit-filter-input')?.addEventListener('input', () => renderAuditRows());

  $('#refresh-audit')?.addEventListener('click', async () => {
    rawAuditEntries = (await api('/audit')) || [];
    renderAuditPage();
  });

  $('#toggle-audit-stream')?.addEventListener('click', () => {
    if (auditStreamTimer) {
      stopAuditStream();
      renderAuditPage();
    } else {
      startAuditStream();
      renderAuditPage();
    }
  });
}

function renderAuditRows() {
  const filterQuery = ($('#audit-filter-input')?.value || '').toLowerCase().trim();
  const filtered = rawAuditEntries.filter(entry => {
    if (!filterQuery) return true;
    return (entry.username || '').toLowerCase().includes(filterQuery) ||
           (entry.client_ip || '').toLowerCase().includes(filterQuery) ||
           (entry.action || '').toLowerCase().includes(filterQuery) ||
           (entry.object || '').toLowerCase().includes(filterQuery) ||
           (entry.detail || '').toLowerCase().includes(filterQuery);
  });
  const tbody = $('#page-audit tbody');
  if (tbody) {
    tbody.innerHTML = filtered.map(entry => `<tr>
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
    </tr>`).join('');
  }
}

function startAuditStream() {
  stopAuditStream();
  auditStreamTimer = setInterval(async () => {
    try {
      rawAuditEntries = (await api('/audit')) || [];
      renderAuditRows();
    } catch (_) {}
  }, 2000);
}

function stopAuditStream() {
  if (auditStreamTimer) {
    clearInterval(auditStreamTimer);
    auditStreamTimer = null;
  }
}
