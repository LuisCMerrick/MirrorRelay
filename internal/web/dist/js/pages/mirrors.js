// Repositories page: searchable list plus row-level lifecycle actions.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { $, copyText, esc, notice } from '../dom.js';
import { number, stateLabel } from '../format.js';
import { L } from '../i18n.js';
import { activeUpstreamFor, healthFor, publicURL } from '../repositories.js';
import { state } from '../state.js';

export async function loadMirrors() {
  state.mirrors = (await api('/mirrors')) || [];
  renderMirrorsTable();
  const searchInput = $('#mirror-search-input');
  if (searchInput && !searchInput.dataset.bound) {
    searchInput.dataset.bound = 'true';
    searchInput.addEventListener('input', () => renderMirrorsTable());
  }
}

function renderMirrorsTable() {
  const query = ($('#mirror-search-input')?.value || '').toLowerCase().trim();
  const filtered = state.mirrors.filter(repository => {
    if (!query) return true;
    return (repository.name || '').toLowerCase().includes(query) ||
           (repository.slug || '').toLowerCase().includes(query) ||
           (repository.type || '').toLowerCase().includes(query) ||
           (repository.public_host || '').toLowerCase().includes(query) ||
           (repository.public_path || '').toLowerCase().includes(query);
  });
  const rows = filtered.map(repository => {
    const active = activeUpstreamFor(repository);
    const health = healthFor(repository);
    const helpBtn = repository.help?.enabled && repository.help?.template ? `<a class="button-link" href="/help/${esc(repository.slug)}/" target="_blank" style="padding:6px 10px;text-decoration:none;border-radius:6px;border:1px solid var(--line);background:var(--panel);color:var(--text);font-size:13px;display:inline-block;">${L('Help')}</a>` : '';
    return `<tr><td><button class="text-button" data-action="show-repository" data-id="${repository.id}"><strong>${esc(repository.name)}</strong></button><br><small>${esc(repository.slug)}</small></td>
      <td>${esc(repository.type)}<br><small>${esc(repository.profile_name || 'Custom')} ${esc(repository.profile_version || '')}</small></td>
      <td><code>${esc(publicURL(repository))}</code></td><td title="${esc(active.url || '')}">${esc((active.url || '').replace(/^https?:\/\//, '').slice(0, 42) || '—')}</td>
      <td><span class="badge ${health === 'healthy' ? 'ok' : health === 'unhealthy' ? 'bad' : ''}">${esc(stateLabel(health))}</span><br><small>${active.latency_ms ? `${number(active.latency_ms)} ms` : '—'}</small></td>
      <td><span class="badge ${repository.config_state === 'active' ? 'ok' : repository.config_state === 'failed' ? 'bad' : ''}" title="${esc(repository.config_error || '')}">${esc(stateLabel(repository.config_state))}</span></td>
      <td>${repository.cache_enabled ? `<span class="badge ok">${esc(repository.cache_profile)}</span>` : stateLabel('disabled')}</td>
      <td class="actions">${helpBtn}<button data-action="show-repository" data-id="${repository.id}">${L('Details')}</button><button data-action="copy-repository-url" data-id="${repository.id}">${L('Copy URL')}</button><button data-action="check-mirror" data-id="${repository.id}">${L('Test')}</button><button data-action="preview-repository-config" data-id="${repository.id}">${L('Config')}</button><button data-action="purge-repository" data-id="${repository.id}">${L('Purge')}</button><button data-action="edit-mirror" data-id="${repository.id}">${L('Edit')}</button><button data-action="toggle-mirror" data-id="${repository.id}" data-enabled="${!repository.enabled}">${repository.enabled ? L('Disable') : L('Enable')}</button><button class="danger" data-action="delete-mirror" data-id="${repository.id}">${L('Delete')}</button></td></tr>`;
  }).join('');
  $('#mirror-list').innerHTML = `<div class="table-wrap"><table><thead><tr><th>${L('Name')}</th><th>${L('Type / profile')}</th><th>${L('Public URL')}</th><th>${L('Active upstream')}</th><th>${L('Health / latency')}</th><th>${L('Desired state')}</th><th>${L('Cache')}</th><th>${L('Actions')}</th></tr></thead><tbody>${rows || `<tr><td colspan="8" class="empty">${L('No repositories yet.')}</td></tr>`}</tbody></table></div>`;
}

registerAction('copy-repository-url', async button => {
  const repository = state.mirrors.find(value => value.id === Number(button.dataset.id));
  if (!repository) return;
  try { await copyText(publicURL(repository)); notice(L('Repository URL copied.')); } catch (error) { notice(error.message, true); }
});

registerAction('check-mirror', async button => {
  notice(L('Checking upstreams…'));
  try {
    const results = await api(`/mirrors/${Number(button.dataset.id)}/check`, {method: 'POST'});
    const healthy = results.length > 0 && results.every(result => result.healthy);
    notice(healthy ? L('All upstreams are healthy.') : L('One or more upstreams are unhealthy.'), !healthy);
    await loadMirrors();
  } catch (error) { notice(error.message, true); }
});

registerAction('toggle-mirror', async button => {
  const enabled = button.dataset.enabled === 'true';
  try {
    await api(`/mirrors/${Number(button.dataset.id)}/${enabled ? 'enable' : 'disable'}`, {method: 'POST'});
    notice(enabled ? L('Repository enabled.') : L('Repository disabled.'));
    await loadMirrors();
  } catch (error) { notice(error.message, true); }
});

registerAction('delete-mirror', async button => {
  if (!confirm(L('Delete this repository and logically invalidate its cache? This cannot be undone.'))) return;
  try { await api(`/mirrors/${Number(button.dataset.id)}`, {method: 'DELETE'}); notice(L('Repository deleted.')); await loadMirrors(); } catch (error) { notice(error.message, true); }
});
