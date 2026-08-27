// Page router: sidebar navigation, page heading and per-page load dispatch.
import { $, notice } from './dom.js';
import { getLocale } from './i18n.js';
import { state } from './state.js';
import { loadDashboard } from './pages/dashboard.js';
import { loadMirrors } from './pages/mirrors.js';
import { loadProfiles } from './pages/profiles.js';
import { loadUpstreamNginx } from './pages/upstreamNginx.js';
import { loadCustom } from './pages/custom.js';
import { loadIngress } from './pages/ingress.js';
import { loadCluster } from './pages/cluster.js';
import { loadCache } from './pages/cache.js';
import { loadHealth } from './pages/health.js';
import { loadAccess, loadAudit, stopLogStreams } from './pages/logs.js';
import { loadSystem } from './pages/system.js';
import { loadSettings } from './pages/settings.js';
import { loadAppearance } from './pages/appearance.js';
import { loadAccount, loadUsers } from './pages/users.js';

const loaders = {
  dashboard: loadDashboard,
  mirrors: loadMirrors,
  profiles: loadProfiles,
  'upstream-nginx': loadUpstreamNginx,
  custom: loadCustom,
  ingress: loadIngress,
  cluster: loadCluster,
  cache: loadCache,
  health: loadHealth,
  access: loadAccess,
  audit: loadAudit,
  system: loadSystem,
  settings: loadSettings,
  appearance: loadAppearance,
  users: loadUsers,
  account: loadAccount,
};

export function updatePageHeading() {
  const loc = getLocale();
  const pageMeta = loc.pageMeta || {};
  const metadata = pageMeta[state.currentPage] || pageMeta.dashboard || ['Dashboard', 'Live service status'];
  $('#page-title').textContent = metadata[0];
  $('#page-subtitle').textContent = metadata[1];
}

export async function renderCurrentPage() {
  stopLogStreams();
  try { await (loaders[state.currentPage] || loadDashboard)(); } catch (error) { notice(error.message, true); }
}

export function initRouter() {
  document.querySelectorAll('nav button').forEach(button => button.addEventListener('click', async () => {
    document.querySelectorAll('nav button').forEach(candidate => {
      const active = candidate === button;
      candidate.classList.toggle('active', active);
      if (active) candidate.setAttribute('aria-current', 'page');
      else candidate.removeAttribute('aria-current');
    });
    document.querySelectorAll('.page').forEach(page => page.classList.add('hidden'));
    state.currentPage = button.dataset.page;
    $('#page-' + state.currentPage).classList.remove('hidden');
    updatePageHeading();
    await renderCurrentPage();
  }));
}
