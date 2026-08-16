// Repository detail dialog: desired/active comparison, statistics, upstreams,
// client examples, configuration previews, cache purge and profile upgrades.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { card, kv, showPreview } from '../components.js';
import { $, copyText, esc, notice } from '../dom.js';
import { bytes, date, number, stateLabel } from '../format.js';
import { L } from '../i18n.js';
import { publicURL } from '../repositories.js';
import { state } from '../state.js';
import { loadMirrors } from './mirrors.js';

function repositorySummary(repository) {
  return `${kv(L('Public URL'), publicURL(repository))}${kv(L('Type / mode'), `${repository.type} / ${repository.proxy_mode}`)}${kv(L('Profile'), `${repository.profile_name || 'Custom'} ${repository.profile_version || ''}`)}${kv(L('Cache'), repository.cache_enabled ? `${repository.cache_profile} · ${repository.cache_authenticated ? L('authenticated enabled') : L('anonymous only')}` : L('Disabled'))}${kv(L('Browsable HTML URL rewrite'), repository.html_rewrite_enabled ? L('Enabled') : L('Disabled'))}${kv(L('Rewrite hosts'), (repository.rewrite_hosts || []).join(', ') || '—')}${repository.config_error ? `<div class="notice error">${esc(repository.config_error)}</div>` : ''}`;
}

async function showRepository(id) {
  try {
    const [repositoryState, examples] = await Promise.all([api(`/mirrors/${id}/state`), api(`/mirrors/${id}/client-config`)]);
    const desired = repositoryState.desired, active = repositoryState.active_found ? repositoryState.active : null, statistics = repositoryState.statistics || {};
    const latest = state.profiles.find(profile => profile.name === desired.profile_name && profile.latest_stable);
    const upgrade = latest && latest.version !== desired.profile_version ? `<button data-action="preview-profile-upgrade" data-id="${id}" data-name="${esc(latest.name)}" data-version="${esc(latest.version)}">${L('Preview upgrade to %s', latest.version)}</button>` : '';
    $('#detail-title').textContent = desired.name;
    $('#detail-content').innerHTML = `<div class="cards detail-cards">${card(L('Desired state'), stateLabel(desired.config_state), desired.config_state === 'active')}${card(L('Active state'), active ? L('Published') : L('Not active'), Boolean(active))}${card(L('Effective config'), `v${repositoryState.effective_config_version || '—'}`)}${card(L('Requests today'), number(statistics.requests || 0))}${card(L('Traffic today'), bytes(statistics.bytes || 0))}${card(L('Observed cache traffic'), bytes(statistics.cache_bytes || 0))}${card(L('Cache HIT / MISS'), `${number(statistics.cache_hits || 0)} / ${number(statistics.cache_misses || 0)}`)}${card('2xx / 3xx / 4xx / 5xx', `${number(statistics.status_2xx || 0)} / ${number(statistics.status_3xx || 0)} / ${number(statistics.status_4xx || 0)} / ${number(statistics.status_5xx || 0)}`)}${card(L('Upstream errors'), number(statistics.upstream_errors || 0))}</div>
      <div class="toolbar"><div class="actions"><button data-action="copy-repository-url" data-id="${id}">${L('Copy URL')}</button><button data-action="edit-mirror-from-detail" data-id="${id}">${L('Edit')}</button><button data-action="check-mirror" data-id="${id}">${L('Test')}</button><button data-action="preview-repository-config" data-id="${id}">${L('Preview config')}</button><button data-action="view-effective-config">${L('Effective config')}</button><button data-action="purge-repository" data-id="${id}">${L('Purge cache')}</button>${upgrade}</div></div>
      <div class="grid2"><div class="panel"><h2>${L('Desired configuration')}</h2>${repositorySummary(desired)}</div><div class="panel"><h2>${L('Active routing snapshot')}</h2>${active ? repositorySummary(active) : `<p class="muted">${L('No active version. The desired configuration may have failed validation or activation.')}</p>`}</div></div>
      <div class="panel"><h2>${L('Upstreams')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Priority')}</th><th>${L('URL')}</th><th>${L('Health')}</th><th>${L('Latency')}</th><th>${L('Last check')}</th></tr></thead><tbody>${(desired.upstreams || []).map(upstream => `<tr><td>${upstream.priority}</td><td><code>${esc(upstream.url)}</code></td><td>${esc(stateLabel(upstream.health_status))}</td><td>${number(upstream.latency_ms)} ms</td><td>${date(upstream.last_check)}</td></tr>`).join('')}</tbody></table></div></div>
      <div class="panel"><h2>${L('Client configuration examples')}</h2>${examples.map((example, index) => `<div class="example"><div class="toolbar"><strong>${esc(example.name)}</strong><button class="copy-example" data-index="${index}">${L('Copy')}</button></div><pre>${esc(example.command)}</pre></div>`).join('')}</div>`;
    $('#detail-content').querySelectorAll('.copy-example').forEach(button => button.addEventListener('click', async () => { await copyText(examples[Number(button.dataset.index)].command); notice(L('Copied.')); }));
    $('#detail-dialog').showModal();
  } catch (error) { notice(error.message, true); }
}

export function initMirrorDetail() {
  $('#close-detail').addEventListener('click', () => $('#detail-dialog').close());
}

registerAction('show-repository', button => showRepository(Number(button.dataset.id)));

registerAction('preview-repository-config', async button => {
  try {
    const value = await api(`/mirrors/${Number(button.dataset.id)}/config`);
    showPreview(L('Generated repository configuration'), `<pre class="config-preview">${esc(value.configuration)}</pre>`);
  } catch (error) { notice(error.message, true); }
});

registerAction('preview-profile-upgrade', async button => {
  const id = Number(button.dataset.id);
  const name = button.dataset.name, version = button.dataset.version;
  try {
    const value = await api(`/mirrors/${id}/profile/preview`, {method: 'POST', body: JSON.stringify({name, version})});
    const rows = Object.entries(value.diff || {}).map(([field, change]) => `<tr><td>${esc(field)}</td><td><code>${esc(JSON.stringify(change.before))}</code></td><td><code>${esc(JSON.stringify(change.after))}</code></td></tr>`).join('');
    showPreview(L('Profile upgrade preview'), `<div class="table-wrap"><table><thead><tr><th>${L('Field')}</th><th>${L('Before')}</th><th>${L('After')}</th></tr></thead><tbody>${rows}</tbody></table></div><div class="toolbar end"><button id="apply-profile-upgrade">${L('Apply upgrade')}</button></div><pre class="config-preview">${esc(value.configuration)}</pre>`);
    $('#apply-profile-upgrade').addEventListener('click', async () => {
      try { await api(`/mirrors/${id}/profile/apply`, {method: 'POST', body: JSON.stringify({name, version})}); $('#preview-dialog').close(); $('#detail-dialog').close(); notice(L('Profile upgrade activated.')); await loadMirrors(); } catch (error) { notice(error.message, true); }
    });
  } catch (error) { notice(error.message, true); }
});

registerAction('purge-repository', async button => {
  const id = Number(button.dataset.id);
  const path = prompt(L('Optional object path. Leave empty to purge the whole repository cache.'), '');
  if (path === null) return;
  try {
    const result = path ? await api(`/mirrors/${id}/cache/purge`, {method: 'POST', body: JSON.stringify({path, query: ''})}) : await api(`/mirrors/${id}/cache`, {method: 'DELETE'});
    notice(L('Logical purge completed; physical reclaim: %s.', result.physical_reclaim));
  } catch (error) { notice(error.message, true); }
});
