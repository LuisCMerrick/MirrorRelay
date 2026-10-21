// Hash-based page router. Every page has a stable, bookmarkable location
// (#/mirrors, #/cache, ...) so reloads, deep links and the browser history
// buttons keep the administrator where they were. The hash never touches the
// server path, which keeps subpath deployments (e.g. /mirrors/admin/) working.
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

const DEFAULT_PAGE = 'dashboard';
const roleRank = {viewer: 0, operator: 1, admin: 2};

// Minimum role per page. It mirrors the requires-* classes on the navigation
// and the server-side RBAC; the server remains the authority.
const routes = {
  dashboard: {load: loadDashboard, role: 'viewer'},
  mirrors: {load: loadMirrors, role: 'viewer'},
  profiles: {load: loadProfiles, role: 'viewer'},
  'upstream-nginx': {load: loadUpstreamNginx, role: 'viewer'},
  custom: {load: loadCustom, role: 'admin'},
  ingress: {load: loadIngress, role: 'admin'},
  cluster: {load: loadCluster, role: 'viewer'},
  cache: {load: loadCache, role: 'viewer'},
  health: {load: loadHealth, role: 'viewer'},
  access: {load: loadAccess, role: 'operator'},
  audit: {load: loadAudit, role: 'viewer'},
  system: {load: loadSystem, role: 'operator'},
  settings: {load: loadSettings, role: 'admin'},
  appearance: {load: loadAppearance, role: 'admin'},
  users: {load: loadUsers, role: 'admin'},
  account: {load: loadAccount, role: 'viewer'},
};

let navigationToken = 0;

export function canAccess(page, role = state.role) {
  const route = routes[page];
  if (!route) return false;
  return (roleRank[role] ?? -1) >= roleRank[route.role];
}

function pageFromHash() {
  const match = /^#\/?([a-z-]+)/.exec(location.hash || '');
  return match ? match[1] : '';
}

function resolvePage(requested) {
  return routes[requested] && canAccess(requested) ? requested : DEFAULT_PAGE;
}

export function updatePageHeading() {
  const pageMeta = getLocale().pageMeta || {};
  const metadata = pageMeta[state.currentPage] || pageMeta.dashboard || ['Dashboard', 'Live service status'];
  $('#page-title').textContent = metadata[0];
  $('#page-subtitle').textContent = metadata[1];
  const title = document.querySelector('[data-instance-title]')?.textContent || 'MirrorRelay';
  document.title = `${metadata[0]} · ${title}`;
}

function activateNavigation(page) {
  document.querySelectorAll('nav button[data-page]').forEach(button => {
    const active = button.dataset.page === page;
    button.classList.toggle('active', active);
    if (active) button.setAttribute('aria-current', 'page');
    else button.removeAttribute('aria-current');
  });
  document.querySelectorAll('.page').forEach(section => section.classList.toggle('hidden', section.id !== `page-${page}`));
}

// isCurrentNavigation lets long-running loaders detect that the administrator
// already moved on, so a stale result does not raise a notice on another page.
export function isCurrentNavigation(token) {
  return token === navigationToken;
}

export async function renderCurrentPage() {
  const token = ++navigationToken;
  stopLogStreams();
  const route = routes[state.currentPage] || routes[DEFAULT_PAGE];
  try {
    await route.load();
  } catch (error) {
    if (isCurrentNavigation(token)) notice(error.message, true);
  }
}

async function showPage(page, {focus = false} = {}) {
  state.currentPage = resolvePage(page);
  const canonical = `#/${state.currentPage}`;
  if (location.hash !== canonical) history.replaceState(null, '', canonical);
  activateNavigation(state.currentPage);
  updatePageHeading();
  document.body.classList.remove('nav-open');
  if (focus) $('#page-title')?.focus({preventScroll: true});
  await renderCurrentPage();
}

// navigate moves to a page through the history stack so Back returns to the
// previous page.
export function navigate(page) {
  const target = `#/${resolvePage(page)}`;
  if (location.hash === target) return showPage(page, {focus: true});
  location.hash = target;
  return undefined;
}

// startRouter renders the page named by the current location; it is called
// once a session is established.
export function startRouter() {
  return showPage(pageFromHash() || DEFAULT_PAGE);
}

export function initRouter() {
  document.querySelectorAll('nav button[data-page]').forEach(button => {
    button.addEventListener('click', () => navigate(button.dataset.page));
  });
  window.addEventListener('hashchange', () => {
    if (!state.signedIn) return;
    void showPage(pageFromHash(), {focus: true});
  });
  $('#nav-toggle')?.addEventListener('click', () => {
    const open = document.body.classList.toggle('nav-open');
    $('#nav-toggle').setAttribute('aria-expanded', String(open));
  });
}
