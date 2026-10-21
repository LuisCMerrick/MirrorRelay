// Entry point: session bootstrap, global event wiring and startup.
import { api, onUnauthorized } from './api.js';
import { dispatchAction } from './actions.js';
import { initAuth, refreshLoginMode, showLogin } from './auth.js';
import { $, notice } from './dom.js';
import { applyLanguage, currentLanguage, L, onLanguageChange } from './i18n.js';
import { onRestartCompleted, triggerRestart } from './restart.js';
import { initRouter, renderCurrentPage, startRouter, updatePageHeading } from './router.js';
import { state } from './state.js';
import { initThemeControls, refreshThemeControls } from './theme.js';
import { loadProfilesData } from './pages/profiles.js';
import { initMirrorForm } from './pages/mirrorForm.js';
import { initMirrorDetail } from './pages/mirrorDetail.js';
import { initCustom } from './pages/custom.js';
import { initCluster } from './pages/cluster.js';
import { stopLogStreams } from './pages/logs.js';
import { applyInstanceAppearance } from './instance-appearance.js';

let instanceAppearanceLoaded = false;

async function loadInstanceAppearance() {
  if (instanceAppearanceLoaded) return;
  applyInstanceAppearance(await api('/auth/appearance'));
  instanceAppearanceLoaded = true;
}

function refreshRoleUI() {
  const labels = {admin: L('Admin'), operator: L('Operator'), viewer: L('Viewer')};
  document.documentElement.dataset.role = state.role || 'viewer';
  const roleLabel = $('#user-role');
  if (roleLabel) roleLabel.textContent = labels[state.role] || labels.viewer;
  $('#nav-toggle')?.setAttribute('aria-label', L('Menu'));
  const status = $('#status');
  if (status && status.dataset.live !== 'true') status.textContent = L('Online');
}

async function boot() {
  try { await loadInstanceAppearance(); } catch (_) {}
  let session;
  try {
    session = await api('/auth/session');
  } catch (_) {
    await showLogin();
    return;
  }
  state.csrf = session.csrf_token;
  // An unknown role must never unlock administrator controls.
  state.role = ['admin', 'operator', 'viewer'].includes(session.role) ? session.role : 'viewer';
  state.username = session.username || '';
  refreshRoleUI();
  state.signedIn = true;
  $('#user-name').textContent = session.username;
  $('#login').classList.add('hidden');
  $('#app').classList.remove('hidden');
  try {
    await loadProfilesData();
  } catch (error) {
    notice(error.message, true);
  }
  await startRouter();
}

// Retranslate the page heading and reload visible data after a language
// switch without a full page reload.
onLanguageChange(() => {
  refreshThemeControls();
  refreshLoginMode();
  refreshRoleUI();
  updatePageHeading();
  if (!state.signedIn) return;
  void (async () => {
    try {
      await loadProfilesData();
      await renderCurrentPage();
    } catch (error) { notice(error.message, true); }
  })();
});

onRestartCompleted(renderCurrentPage);

onUnauthorized(() => {
  if (!state.signedIn) return;
  stopLogStreams();
  void showLogin(L('Your session has expired. Sign in again to continue.'));
});

window.addEventListener('unhandledrejection', event => {
  const message = event.reason?.message || String(event.reason || '');
  if (message) notice(message, true);
});

document.querySelectorAll('.language-switch button').forEach(button => button.addEventListener('click', () => applyLanguage(button.dataset.lang, true)));

$('#logout').addEventListener('click', async () => {
  try { await api('/auth/logout', {method: 'POST'}); } catch (_) {}
  state.csrf = '';
  location.reload();
});

$('#close-preview').addEventListener('click', () => $('#preview-dialog').close());
$('#restart-header')?.addEventListener('click', triggerRestart);
$('#restart-sidebar')?.addEventListener('click', triggerRestart);

initAuth(boot);
initRouter();
initThemeControls();
initMirrorForm();
initMirrorDetail();
initCustom();
initCluster();

// Delegated dispatch for dynamically rendered button[data-action] controls.
document.addEventListener('click', event => {
  const button = event.target.closest('button[data-action]');
  if (!button || button.disabled) return;
  event.preventDefault();
  button.disabled = true;
  void dispatchAction(button)
    .catch(error => notice(error.message, true))
    .finally(() => { if (button.isConnected) button.disabled = false; });
});

applyLanguage(currentLanguage());
boot();
