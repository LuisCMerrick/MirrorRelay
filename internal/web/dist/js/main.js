// Entry point: session bootstrap, global event wiring and startup.
import { api } from './api.js';
import { dispatchAction } from './actions.js';
import { $, notice } from './dom.js';
import { applyLanguage, currentLanguage, onLanguageChange } from './i18n.js';
import { onRestartCompleted, triggerRestart } from './restart.js';
import { initRouter, renderCurrentPage, updatePageHeading } from './router.js';
import { state } from './state.js';
import { loadDashboard } from './pages/dashboard.js';
import { loadMirrors } from './pages/mirrors.js';
import { initMirrorForm } from './pages/mirrorForm.js';
import { initMirrorDetail } from './pages/mirrorDetail.js';
import { loadProfilesData } from './pages/profiles.js';
import { initCustom } from './pages/custom.js';
import { initCluster } from './pages/cluster.js';

async function boot() {
  let session;
  try {
    session = await api('/auth/session');
  } catch (_) {
    state.signedIn = false;
    $('#app').classList.add('hidden');
    $('#login').classList.remove('hidden');
    return;
  }
  state.csrf = session.csrf_token;
  state.signedIn = true;
  $('#user-name').textContent = session.username;
  $('#login').classList.add('hidden');
  $('#app').classList.remove('hidden');
  try {
    await loadProfilesData();
    await Promise.all([loadDashboard(), loadMirrors()]);
  } catch (error) {
    notice(error.message, true);
  }
}

// Retranslate the page heading and reload visible data after a language
// switch without a full page reload.
onLanguageChange(() => {
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

document.querySelectorAll('.language-switch button').forEach(button => button.addEventListener('click', () => applyLanguage(button.dataset.lang, true)));

$('#login-form').addEventListener('submit', async event => {
  event.preventDefault();
  $('#login-error').textContent = '';
  try {
    const session = await api('/auth/login', {method: 'POST', body: JSON.stringify({username: $('#login-user').value, password: $('#login-password').value})});
    state.csrf = session.csrf_token;
    await boot();
  } catch (error) { $('#login-error').textContent = error.message; }
});
$('#logout').addEventListener('click', async () => {
  try { await api('/auth/logout', {method: 'POST'}); } catch (_) {}
  state.csrf = ''; location.reload();
});

$('#close-preview').addEventListener('click', () => $('#preview-dialog').close());
$('#restart-header')?.addEventListener('click', triggerRestart);
$('#restart-sidebar')?.addEventListener('click', triggerRestart);

initRouter();
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
