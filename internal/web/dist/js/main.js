// Entry point: session bootstrap, global event wiring and startup.
import { api } from './api.js';
import { dispatchAction } from './actions.js';
import { $, notice } from './dom.js';
import { applyLanguage, currentLanguage, getLocale, L, onLanguageChange } from './i18n.js';
import { onRestartCompleted, triggerRestart } from './restart.js';
import { initRouter, renderCurrentPage, updatePageHeading } from './router.js';
import { state } from './state.js';
import { applyInstanceTheme, initThemeControls, refreshThemeControls } from './theme.js';
import { loadDashboard } from './pages/dashboard.js';
import { loadMirrors } from './pages/mirrors.js';
import { initMirrorForm } from './pages/mirrorForm.js';
import { initMirrorDetail } from './pages/mirrorDetail.js';
import { loadProfilesData } from './pages/profiles.js';
import { initCustom } from './pages/custom.js';
import { initCluster } from './pages/cluster.js';

let initialRegistrationRequired = false;

function refreshLoginMode() {
  const dictionary = getLocale().dictionary || {};
  const text = key => dictionary[key] || key;
  $('#login-mode-title').textContent = text(initialRegistrationRequired ? 'initialAdminTitle' : 'signInTitle');
  $('#login-mode-description').textContent = text(initialRegistrationRequired ? 'initialAdminDescription' : 'signInDescription');
  $('#login-submit').textContent = text(initialRegistrationRequired ? 'createAdministrator' : 'signIn');
  $('#login-password-confirmation-group').classList.toggle('hidden', !initialRegistrationRequired);
  $('#login-password-confirmation').required = initialRegistrationRequired;
  $('#login-password').autocomplete = initialRegistrationRequired ? 'new-password' : 'current-password';
}

function refreshRoleUI() {
  const labels = {
    admin: L('Admin'),
    operator: L('Operator'),
    viewer: L('Viewer')
  };
  document.documentElement.dataset.role = state.role || 'admin';
  const roleLabel = $('#user-role');
  if (roleLabel) roleLabel.textContent = labels[state.role] || labels.admin;
}

async function loadInitialRegistrationStatus() {
  const status = await api('/auth/bootstrap');
  initialRegistrationRequired = Boolean(status?.required);
  refreshLoginMode();
}

async function boot() {
  let session;
  try {
    session = await api('/auth/session');
  } catch (_) {
    state.signedIn = false;
    $('#app').classList.add('hidden');
    $('#login').classList.remove('hidden');
    try {
      await loadInitialRegistrationStatus();
    } catch (error) {
      $('#login-error').textContent = error.message;
    }
    return;
  }
  state.csrf = session.csrf_token;
  state.role = session.role || 'admin';
  refreshRoleUI();
  state.signedIn = true;
  $('#user-name').textContent = session.username;
  try {
    const appearance = await api('/appearance');
    applyInstanceTheme(appearance.theme);
  } catch (_) {}
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

document.querySelectorAll('.language-switch button').forEach(button => button.addEventListener('click', () => applyLanguage(button.dataset.lang, true)));

$('#login-form').addEventListener('submit', async event => {
  event.preventDefault();
  $('#login-error').textContent = '';
  try {
    const username = $('#login-user').value;
    const password = $('#login-password').value;
    let session;
    if (initialRegistrationRequired) {
      const confirmation = $('#login-password-confirmation').value;
      if (password !== confirmation) {
        const dictionary = getLocale().dictionary || {};
        throw new Error(dictionary.passwordConfirmationMismatch || 'Password confirmation does not match.');
      }
      session = await api('/auth/bootstrap', {
        method: 'POST',
        body: JSON.stringify({username, password, password_confirmation: confirmation})
      });
    } else {
      session = await api('/auth/login', {method: 'POST', body: JSON.stringify({username, password})});
    }
    state.csrf = session.csrf_token;
    $('#login-password').value = '';
    $('#login-password-confirmation').value = '';
    await boot();
  } catch (error) {
    $('#login-error').textContent = error.message;
    if (initialRegistrationRequired) {
      try { await loadInitialRegistrationStatus(); } catch (_) {}
    }
  }
});
$('#logout').addEventListener('click', async () => {
  try { await api('/auth/logout', {method: 'POST'}); } catch (_) {}
  state.csrf = ''; location.reload();
});

$('#close-preview').addEventListener('click', () => $('#preview-dialog').close());
$('#restart-header')?.addEventListener('click', triggerRestart);
$('#restart-sidebar')?.addEventListener('click', triggerRestart);

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
