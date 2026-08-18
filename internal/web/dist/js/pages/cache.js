// Cache page: storage summary, per-repository cache traffic, targeted object purge and purge jobs.
import { api } from '../api.js';
import { card, kv } from '../components.js';
import { $, esc, notice } from '../dom.js';
import { bytes, date, duration, number, stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';

export async function loadCache() {
  const [cache, dashboard, repositories] = await Promise.all([api('/cache'), api('/stats'), api('/mirrors')]);
  const jobs = cache.purge_jobs || [], maximum = cache.maximum_bytes || cache.max_bytes || 0, byRepository = dashboard.stats.by_mirror || {};

  $('#page-cache').innerHTML = `
    <div class="cards">
      ${card(L('Cache files'), number(cache.files), false, 'file-text')}
      ${card(L('Used space'), bytes(cache.bytes), false, 'database')}
      ${card(L('Maximum space'), bytes(maximum), false, 'hard-drive')}
      ${card(L('Global generation'), cache.global_generation, false, 'refresh')}
    </div>

    <!-- Targeted Object Search & Purge Explorer -->
    <div class="panel">
      <div class="panel-header-row">
        <h2>${icon('search', 18)} ${L('Targeted Cache Object Invalidation')}</h2>
        <span class="badge blue">${L('Instant Purge')}</span>
      </div>
      <p class="muted">${L('Perform precision targeted cache purging on a specific repository and object path without affecting other cached files.')}</p>
      
      <form id="targeted-purge-form" class="form-grid">
        <label>
          <span>${L('Repository')}</span>
          <select id="purge-repo-select" required>
            ${repositories.map(r => `<option value="${r.id}">${esc(r.name)} (${esc(r.slug)})</option>`).join('')}
          </select>
        </label>
        <label>
          <span>${L('Object URL Path / Prefix (leave empty to purge entire repository)')}</span>
          <input id="purge-path-input" placeholder="/debian/dists/bookworm/Release or /simple/requests/" />
        </label>
        <div class="toolbar end" style="grid-column: span 2; margin-bottom: 0; padding: 0; background: transparent; border: none;">
          <button type="submit" class="danger">
            ${icon('trash', 14)} ${L('Invalidate Target Path')}
          </button>
        </div>
      </form>
      <div id="targeted-purge-result" class="notice ok hidden" style="margin-top: 14px;"></div>
    </div>

    <div class="panel">
      <h2>${icon('database', 18)} ${L('Cache storage')}</h2>
      ${kv(L('Path'), cache.path)}
      ${kv(L('Maximum files'), number(cache.maximum_files))}
      ${kv(L('Minimum free space'), bytes(cache.minimum_free_bytes))}
      ${kv(L('Inactive window'), duration(cache.inactive_seconds))}
      <div class="panel-actions">
        <button class="danger" id="clear-cache">${icon('trash', 14)} ${L('Global logical purge')}</button>
      </div>
      <p class="muted">${L('Logical invalidation is immediate. Physical files remain until the asynchronous Nginx cache manager completes its inactive/max_size cleanup window.')}</p>
    </div>

    <div class="panel">
      <h2>${icon('layers', 18)} ${L('Repository cache traffic today')}</h2>
      <p class="muted">${L('Nginx cache files are content-keyed; this table reports observed cache-served traffic, not guessed physical ownership.')}</p>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${L('Repository')}</th>
              <th>${L('HIT')}</th>
              <th>${L('MISS')}</th>
              <th>${L('Cache-served bytes')}</th>
            </tr>
          </thead>
          <tbody>
            ${repositories.map(repository => {
              const value = byRepository[repository.id] || {};
              return `<tr>
                <td><strong>${esc(repository.name)}</strong></td>
                <td><span class="badge ok">${number(value.cache_hits)}</span></td>
                <td><span class="badge">${number(value.cache_misses)}</span></td>
                <td><code>${bytes(value.cache_bytes)}</code></td>
              </tr>`;
            }).join('')}
          </tbody>
        </table>
      </div>
    </div>

    <div class="panel">
      <h2>${icon('shield', 18)} ${L('Purge / reclaim jobs')}</h2>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${L('Time')}</th>
              <th>${L('Scope')}</th>
              <th>${L('Generation')}</th>
              <th>${L('Logical purge')}</th>
              <th>${L('Physical reclaim')}</th>
              <th>${L('Reclaimed')}</th>
              <th>${L('Operator')}</th>
            </tr>
          </thead>
          <tbody>
            ${jobs.map(job => `<tr>
              <td>${date(job.created_at)}</td>
              <td>${esc(job.scope)} ${job.repository_id || ''}</td>
              <td><code>${job.old_generation} → ${job.new_generation}</code></td>
              <td><span class="badge ok">${L('Completed')}</span></td>
              <td>
                <span class="badge ${job.reclaim_state === 'completed' ? 'ok' : job.reclaim_state === 'failed' ? 'bad' : ''}" title="${esc(job.error || '')}">
                  ${esc(stateLabel(job.reclaim_state))}
                </span>
              </td>
              <td><code>${bytes(job.reclaimed_bytes)}</code></td>
              <td><span class="badge blue">${esc(job.operator)}</span></td>
            </tr>`).join('')}
          </tbody>
        </table>
      </div>
    </div>`;

  $('#targeted-purge-form')?.addEventListener('submit', async event => {
    event.preventDefault();
    const repoId = Number($('#purge-repo-select').value);
    const path = $('#purge-path-input').value.trim();
    const resBox = $('#targeted-purge-result');

    try {
      const result = path
        ? await api(`/mirrors/${repoId}/cache/purge`, {method: 'POST', body: JSON.stringify({path, query: ''})})
        : await api(`/mirrors/${repoId}/cache`, {method: 'DELETE'});
      
      const msg = path
        ? L('Targeted purge completed for "%s" (Reclaim: %s)', path, result.physical_reclaim)
        : L('Repository cache namespace invalidated (Reclaim: %s)', result.physical_reclaim);
      
      notice(msg);
      if (resBox) {
        resBox.textContent = msg;
        resBox.classList.remove('hidden');
      }
      await loadCache();
    } catch (err) {
      notice(err.message, true);
    }
  });

  $('#clear-cache').addEventListener('click', async () => {
    if (!confirm(L('Invalidate every existing cache namespace?'))) return;
    try {
      const result = await api('/cache', {method: 'DELETE'});
      notice(L('Logical purge completed; physical reclaim is %s.', result.physical_reclaim));
      await loadCache();
    } catch (error) {
      notice(error.message, true);
    }
  });
}
