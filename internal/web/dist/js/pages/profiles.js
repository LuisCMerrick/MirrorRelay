// Profiles page plus the shared profile/help-template reference data used by
// the repository form.
import { api } from '../api.js';
import { $, esc } from '../dom.js';
import { L } from '../i18n.js';
import { state } from '../state.js';

let helpTemplates = [];

async function loadHelpTemplates() {
  helpTemplates = (await api('/help/templates').catch(() => [])) || [];
  const options = `<option value="">${L('None / 无')}</option>` + helpTemplates.map(t => `<option value="${esc(t.id)}">${esc(t.title)} (${esc(t.type)})</option>`).join('');
  const el = $('#help-template');
  if (el) el.innerHTML = options;
}

export async function loadProfilesData() {
  const [profileList] = await Promise.all([
    api('/profiles').catch(() => []),
    loadHelpTemplates()
  ]);
  state.profiles = profileList || [];
  $('#template').innerHTML = `<option value="">${L('Custom')}</option>` + state.profiles.map((profile, index) => `<option value="${index}">${esc(profile.name)} · ${esc(profile.version)}${profile.latest_stable ? ` · ${L('latest')}` : ''}</option>`).join('');
}

export async function loadProfiles() {
  if (!state.profiles.length) await loadProfilesData();
  $('#page-profiles').innerHTML = `<div class="panel"><p class="muted">${L('Profiles are versioned defaults. Every field remains editable after applying a profile, and existing repositories stay pinned until an explicit upgrade.')}</p></div>
  <div class="table-wrap"><table><thead><tr><th>${L('Profile')}</th><th>${L('Version')}</th><th>${L('Type')}</th><th>${L('Upstream')}</th><th>${L('Mode')}</th><th>${L('Cache / rewrite')}</th></tr></thead><tbody>
  ${state.profiles.map(profile => `<tr><td><strong>${esc(profile.name)}</strong>${profile.latest_stable ? ` <span class="badge ok">${L('Latest stable')}</span>` : ''}</td><td>${esc(profile.version)}</td><td>${esc(profile.type)}</td><td><code>${esc(profile.upstream)}</code></td><td>${esc(profile.public_mode)} / ${esc(profile.proxy_mode)}</td><td>${profile.cache_enabled ? L('Cache') : '—'} ${profile.rewrite_enabled ? `· ${L('Rewrite')}` : ''}</td></tr>`).join('')}</tbody></table></div>`;
}
