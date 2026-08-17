// Repositories page: searchable list plus row-level lifecycle actions.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { $, copyText, esc, notice } from '../dom.js';
import { number, stateLabel } from '../format.js';
import { icon } from '../icons.js';
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
    const isHealthy = health === 'healthy';
    const helpBtn = repository.help?.enabled && repository.help?.template
      ? `<a class="button-link" href="/help/${esc(repository.slug)}/" target="_blank" title="${L('Help documentation')}">${icon('help', 13)} ${L('Help')}</a>`
      : '';

    return `<tr>
      <td>
        <button class="text-button" data-action="show-repository" data-id="${repository.id}">
          <strong>${esc(repository.name)}</strong>
        </button>
        <small>${esc(repository.slug)}</small>
      </td>
      <td>
        <span class="badge blue">${esc(repository.type)}</span>
        <small>${esc(repository.profile_name || 'Custom')} ${esc(repository.profile_version || '')}</small>
      </td>
      <td>
        <div class="code-copy-row">
          <code>${esc(publicURL(repository))}</code>
          <button class="icon-btn" data-action="copy-repository-url" data-id="${repository.id}" title="${L('Copy URL')}">
            ${icon('copy', 13)}
          </button>
        </div>
      </td>
      <td title="${esc(active.url || '')}">
        <span class="upstream-pill">${esc((active.url || '').replace(/^https?:\/\//, '').slice(0, 36) || '—')}</span>
      </td>
      <td>
        <span class="badge ${isHealthy ? 'ok' : 'bad'}">
          <span class="pulse-dot ${isHealthy ? 'green' : 'red'}"></span>
          ${esc(stateLabel(health))}
        </span>
        <small>${active.latency_ms ? `${number(active.latency_ms)} ms` : '—'}</small>
      </td>
      <td>
        <span class="badge ${repository.config_state === 'active' ? 'ok' : 'bad'}" title="${esc(repository.config_error || '')}">
          ${esc(stateLabel(repository.config_state))}
        </span>
      </td>
      <td>
        ${repository.cache_enabled ? `<span class="badge ok">${esc(repository.cache_profile)}</span>` : `<span class="badge">${stateLabel('disabled')}</span>`}
      </td>
      <td>
        <div class="actions">
          ${helpBtn}
          <button data-action="show-repository" data-id="${repository.id}">${icon('file-text', 13)} ${L('Details')}</button>
          <button data-action="check-mirror" data-id="${repository.id}">${icon('play', 13)} ${L('Test')}</button>
          <button data-action="preview-repository-config" data-id="${repository.id}">${icon('code', 13)} ${L('Config')}</button>
          <button data-action="purge-repository" data-id="${repository.id}">${icon('database', 13)} ${L('Purge')}</button>
          <button data-action="edit-mirror" data-id="${repository.id}">${icon('edit', 13)} ${L('Edit')}</button>
          <button data-action="toggle-mirror" data-id="${repository.id}" data-enabled="${!repository.enabled}">
            ${repository.enabled ? L('Disable') : L('Enable')}
          </button>
          <button class="danger" data-action="delete-mirror" data-id="${repository.id}" title="${L('Delete')}">
            ${icon('trash', 13)}
          </button>
        </div>
      </td>
    </tr>`;
  }).join('');

  $('#mirror-list').innerHTML = `<div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>${L('Name')}</th>
          <th>${L('Type / profile')}</th>
          <th>${L('Public URL')}</th>
          <th>${L('Active upstream')}</th>
          <th>${L('Health / latency')}</th>
          <th>${L('Desired state')}</th>
          <th>${L('Cache')}</th>
          <th>${L('Actions')}</th>
        </tr>
      </thead>
      <tbody>
        ${rows || `<tr><td colspan="8" class="empty">${L('No repositories yet.')}</td></tr>`}
      </tbody>
    </table>
  </div>`;
}

registerAction('copy-repository-url', async button => {
  const repository = state.mirrors.find(value => value.id === Number(button.dataset.id));
  if (!repository) return;
  try {
    await copyText(publicURL(repository));
    notice(L('Repository URL copied.'));
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('check-mirror', async button => {
  notice(L('Checking upstreams…'));
  try {
    const results = await api(`/mirrors/${Number(button.dataset.id)}/check`, {method: 'POST'});
    const healthy = results.length > 0 && results.every(result => result.healthy);
    notice(healthy ? L('All upstreams are healthy.') : L('One or more upstreams are unhealthy.'), !healthy);
    await loadMirrors();
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('toggle-mirror', async button => {
  const enabled = button.dataset.enabled === 'true';
  try {
    await api(`/mirrors/${Number(button.dataset.id)}/${enabled ? 'enable' : 'disable'}`, {method: 'POST'});
    notice(enabled ? L('Repository enabled.') : L('Repository disabled.'));
    await loadMirrors();
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('delete-mirror', async button => {
  if (!confirm(L('Delete this repository and logically invalidate its cache? This cannot be undone.'))) return;
  try {
    await api(`/mirrors/${Number(button.dataset.id)}`, {method: 'DELETE'});
    notice(L('Repository deleted.'));
    await loadMirrors();
  } catch (error) {
    notice(error.message, true);
  }
});
