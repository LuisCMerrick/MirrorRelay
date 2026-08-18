// Cache page: storage summary, per-repository cache traffic, targeted object purge,
// smart cache warm-up / predictive pre-fetching, and purge jobs.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { card, kv } from '../components.js';
import { $, esc, notice } from '../dom.js';
import { bytes, date, duration, number, stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';

export async function loadCache() {
  const [cache, dashboard, repositories, warmupStatus, warmupJobs] = await Promise.all([
    api('/cache'),
    api('/stats'),
    api('/mirrors'),
    api('/warmup/status').catch(() => ({enabled: false, running_jobs: 0, max_concurrency: 4, total_warmups: 0, bytes_warmed: 0})),
    api('/warmup/jobs').catch(() => [])
  ]);

  const jobs = cache.purge_jobs || [], maximum = cache.maximum_bytes || cache.max_bytes || 0, byRepository = dashboard.stats.by_mirror || {};

  const warmupRows = (warmupJobs || []).map(job => {
    const isRunning = job.status === 'running';
    const isOk = job.status === 'completed';
    const isFailed = job.status === 'failed';
    const statusClass = isRunning ? 'badge blue' : (isOk ? 'badge ok' : (isFailed ? 'badge bad' : 'badge'));
    return `<tr>
      <td><strong>${esc(job.name)}</strong></td>
      <td><span class="badge blue">${esc(job.mirror_name || job.mirror_slug || `Repo #${job.mirror_id}`)}</span></td>
      <td><code>${esc(job.cron_expression || L('Manual only'))}</code></td>
      <td>
        <span class="${statusClass}">
          ${isRunning ? `<span class="pulse-dot blue"></span>` : ''}
          ${esc(stateLabel(job.status || 'idle'))}
        </span>
      </td>
      <td>
        <small>${number(job.completed_items || 0)} / ${number(job.total_items || 0)} ${L('items')} (${bytes(job.bytes_downloaded || 0)})</small>
      </td>
      <td>${job.last_run_at ? date(job.last_run_at) : '—'}</td>
      <td>
        <div class="actions">
          ${isRunning
            ? `<button class="small danger" data-action="cancel-warmup" data-id="${job.id}">${icon('alert', 12)} ${L('Cancel')}</button>`
            : `<button class="small secondary" data-action="run-warmup" data-id="${job.id}">${icon('play', 12)} ${L('Run now')}</button>`}
          <button class="small danger" data-action="delete-warmup" data-id="${job.id}">${icon('trash', 12)}</button>
        </div>
      </td>
    </tr>`;
  }).join('');

  $('#page-cache').innerHTML = `
    <div class="cards">
      ${card(L('Cache files'), number(cache.files), false, 'file-text')}
      ${card(L('Used space'), bytes(cache.bytes), false, 'database')}
      ${card(L('Maximum space'), bytes(maximum), false, 'hard-drive')}
      ${card(L('Global generation'), cache.global_generation, false, 'refresh')}
      ${card(L('Warm-Up Status'), warmupStatus.enabled ? L('Active') : L('Disabled (Default)'), warmupStatus.enabled, 'zap')}
      ${card(L('Warm-Up Total Items'), number(warmupStatus.total_warmups || 0), false, 'trend-up')}
    </div>

    <!-- Smart Cache Warm-Up & Predictive Pre-Fetching Section -->
    <div class="panel">
      <div class="panel-header-row">
        <h2>${icon('zap', 18)} ${L('Smart Cache Warm-Up & Predictive Pre-Fetching')}</h2>
        ${warmupStatus.enabled ? `<span class="badge ok">${L('Engine Active')}</span>` : `<span class="badge yellow">${L('Engine Disabled (Default)')}</span>`}
      </div>
      <p class="muted">${L('Proactively pre-fetch and warm up repository indexes, release manifests, and critical packages into Managed Upstream Nginx proxy cache. Features intelligent metadata parsing (APT / RPM / PyPI) to eliminate first-hit cache misses.')}</p>

      <form id="create-warmup-form" class="form-grid" style="margin-top: 16px;">
        <label>
          <span>${L('Target Repository')}</span>
          <select id="warmup-repo-select" required>
            ${repositories.map(r => `<option value="${r.id}">${esc(r.name)} (${esc(r.slug)})</option>`).join('')}
          </select>
        </label>
        <label>
          <span>${L('Warm-Up Task Name')}</span>
          <input id="warmup-name-input" required placeholder="e.g. Daily Core Bookworm Warmup" />
        </label>
        <label>
          <span>${L('Schedule (Cron expression or interval)')}</span>
          <input id="warmup-cron-input" placeholder="@hourly, @daily, or 0 2 * * *" />
        </label>
        <label class="wide">
          <span>${L('Target URL Paths / Relative Patterns (one per line, e.g. /dists/bookworm/Release)')}</span>
          <textarea id="warmup-patterns-input" rows="3" required placeholder="/dists/bookworm/Release&#10;/dists/bookworm/main/binary-amd64/Packages.gz&#10;/simple/requests/"></textarea>
        </label>
        <div class="toolbar end" style="grid-column: span 2; margin-bottom: 0; padding: 0; background: transparent; border: none;">
          <button type="submit" class="btn-primary">
            ${icon('plus', 14)} ${L('Create Warm-Up Task')}
          </button>
        </div>
      </form>

      <div class="table-wrap" style="margin-top: 20px;">
        <table>
          <thead>
            <tr>
              <th>${L('Task name')}</th>
              <th>${L('Repository')}</th>
              <th>${L('Schedule')}</th>
              <th>${L('Status')}</th>
              <th>${L('Progress / Bytes')}</th>
              <th>${L('Last run')}</th>
              <th>${L('Actions')}</th>
            </tr>
          </thead>
          <tbody>
            ${warmupRows || `<tr><td colspan="7" class="empty">${L('No warm-up tasks configured yet.')}</td></tr>`}
          </tbody>
        </table>
      </div>
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

  $('#create-warmup-form')?.addEventListener('submit', async event => {
    event.preventDefault();
    const mirrorId = Number($('#warmup-repo-select').value);
    const name = $('#warmup-name-input').value.trim();
    const cron = $('#warmup-cron-input').value.trim();
    const rawPatterns = $('#warmup-patterns-input').value.split('\n').map(p => p.trim()).filter(Boolean);

    try {
      await api('/warmup/jobs', {
        method: 'POST',
        body: JSON.stringify({
          mirror_id: mirrorId,
          name,
          cron_expression: cron,
          url_patterns: rawPatterns,
          enabled: true
        })
      });
      notice(L('Warm-up task created.'));
      await loadCache();
    } catch (error) {
      notice(error.message, true);
    }
  });

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

  $('#clear-cache')?.addEventListener('click', async () => {
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

registerAction('run-warmup', async button => {
  try {
    await api(`/warmup/jobs/${Number(button.dataset.id)}/run`, {method: 'POST'});
    notice(L('Warm-up job started.'));
    await loadCache();
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('cancel-warmup', async button => {
  try {
    await api(`/warmup/jobs/${Number(button.dataset.id)}/cancel`, {method: 'POST'});
    notice(L('Warm-up job cancelled.'));
    await loadCache();
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('delete-warmup', async button => {
  if (!confirm(L('Delete this warm-up task?'))) return;
  try {
    await api(`/warmup/jobs/${Number(button.dataset.id)}`, {method: 'DELETE'});
    notice(L('Warm-up task deleted.'));
    await loadCache();
  } catch (error) {
    notice(error.message, true);
  }
});
