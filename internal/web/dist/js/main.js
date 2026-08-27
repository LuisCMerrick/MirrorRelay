// Entry point: session bootstrap, global event wiring and startup.
import { api } from './api.js';
import { dispatchAction } from './actions.js';
import { $, notice } from './dom.js';
import { applyLanguage, currentLanguage, getLocale, L, onLanguageChange } from './i18n.js';
import { onRestartCompleted, triggerRestart } from './restart.js';
import { initRouter, renderCurrentPage, updatePageHeading } from './router.js';
import { state } from './state.js';
import { initThemeControls, refreshThemeControls } from './theme.js';
import { loadDashboard } from './pages/dashboard.js';
import { loadMirrors } from './pages/mirrors.js';
import { initMirrorForm } from './pages/mirrorForm.js';
import { initMirrorDetail } from './pages/mirrorDetail.js';
import { loadProfilesData } from './pages/profiles.js';
import { initCustom } from './pages/custom.js';
import { initCluster } from './pages/cluster.js';
import { applyInstanceAppearance } from './instance-appearance.js';

import { loginWithPasskey } from './passkey.js';

let initialRegistrationRequired = false;
let recoveryMode = false;
let instanceAppearanceLoaded = false;

async function loadInstanceAppearance() {
  if (instanceAppearanceLoaded) return;
  const appearance = await api('/auth/appearance');
  applyInstanceAppearance(appearance);
  instanceAppearanceLoaded = true;
}

function refreshLoginMode() {
  if (initialRegistrationRequired) recoveryMode = false;
  const dictionary = getLocale().dictionary || {};
  const text = key => dictionary[key] || key;
  $('#login-mode-title').textContent = text(initialRegistrationRequired ? 'initialAdminTitle' : 'signInTitle');
  $('#login-mode-description').textContent = text(initialRegistrationRequired ? 'initialAdminDescription' : 'signInDescription');
  $('#login-submit').textContent = text(initialRegistrationRequired ? 'createAdministrator' : 'signIn');
  $('#login-password-confirmation-group').classList.toggle('hidden', !initialRegistrationRequired);
  $('#login-password-confirmation').required = initialRegistrationRequired;
  $('#login-password').required = initialRegistrationRequired || !recoveryMode;
  $('#login-recovery-code').required = !initialRegistrationRequired && recoveryMode;
  $('#login-password').autocomplete = initialRegistrationRequired ? 'new-password' : 'current-password';
  const recoveryToggleLabel = $('#login-recovery-toggle span');
  if (recoveryToggleLabel) recoveryToggleLabel.textContent = text(recoveryMode ? 'usePassword' : 'useRecoveryCode');
  if (initialRegistrationRequired) {
    $('#login-password-group').classList.remove('hidden');
    $('#login-recovery-group').classList.add('hidden');
    $('#login-alt-actions')?.classList.add('hidden');
  }
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
  if (!initialRegistrationRequired) {
    $('#login-alt-actions')?.classList.remove('hidden');
    try {
      const passkeyStatus = await api('/auth/passkey/status');
      $('#login-passkey-btn')?.classList.toggle('hidden', !passkeyStatus?.enabled);
    } catch (_) {
      // Recovery codes remain usable independently of the Passkey feature.
      // Keep that path visible if the status probe fails or Passkeys are off.
      $('#login-passkey-btn')?.classList.add('hidden');
    }
  }
}

async function boot() {
  try { await loadInstanceAppearance(); } catch (_) {}
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
      $('#login-error').focus();
    }
    return;
  }
  state.csrf = session.csrf_token;
  state.role = session.role || 'admin';
  refreshRoleUI();
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

$('#login-passkey-btn')?.addEventListener('click', async () => {
  $('#login-error').textContent = '';
  const button = $('#login-passkey-btn');
  try {
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    const username = $('#login-user').value.trim();
    const session = await loginWithPasskey(username);
    state.csrf = session.csrf_token;
    await boot();
  } catch (error) {
    $('#login-error').textContent = error.message;
    $('#login-error').focus();
  } finally {
    button.disabled = false;
    button.removeAttribute('aria-busy');
  }
});

$('#login-recovery-toggle')?.addEventListener('click', () => {
  recoveryMode = !recoveryMode;
  $('#login-password-group').classList.toggle('hidden', recoveryMode);
  $('#login-recovery-group').classList.toggle('hidden', !recoveryMode);
  refreshLoginMode();
  if (recoveryMode) {
    $('#login-recovery-code').focus();
  } else {
    $('#login-password').focus();
  }
});

$('#login-form').addEventListener('submit', async event => {
  event.preventDefault();
  $('#login-error').textContent = '';
  const submitButton = $('#login-submit');
  try {
    submitButton.disabled = true;
    submitButton.setAttribute('aria-busy', 'true');
    const username = $('#login-user').value.trim();
    let session;
    if (initialRegistrationRequired) {
      const password = $('#login-password').value;
      const confirmation = $('#login-password-confirmation').value;
      if (password !== confirmation) {
        const dictionary = getLocale().dictionary || {};
        throw new Error(dictionary.passwordConfirmationMismatch || 'Password confirmation does not match.');
      }
      session = await api('/auth/bootstrap', {
        method: 'POST',
        body: JSON.stringify({username, password, password_confirmation: confirmation})
      });
    } else if (recoveryMode) {
      const recovery_code = $('#login-recovery-code').value;
      session = await api('/auth/recovery/login', {
        method: 'POST',
        body: JSON.stringify({username, recovery_code})
      });
    } else {
      const password = $('#login-password').value;
      session = await api('/auth/login', {method: 'POST', body: JSON.stringify({username, password})});
    }
    state.csrf = session.csrf_token;
    $('#login-password').value = '';
    $('#login-password-confirmation').value = '';
    $('#login-recovery-code').value = '';
    await boot();
  } catch (error) {
    $('#login-error').textContent = error.message;
    $('#login-error').focus();
    if (initialRegistrationRequired) {
      try { await loadInitialRegistrationStatus(); } catch (_) {}
    }
  } finally {
    submitButton.disabled = false;
    submitButton.removeAttribute('aria-busy');
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
