// Sign-in surface: password, recovery-code and Passkey login plus the one-time
// initial administrator registration. The entry point supplies onSignedIn.
import { api } from './api.js';
import { $ } from './dom.js';
import { getLocale } from './i18n.js';
import { loginWithPasskey } from './passkey.js';
import { state } from './state.js';
import { showFormError, withBusy } from './ui.js';

let initialRegistrationRequired = false;
let recoveryMode = false;
let signedInHandler = async () => {};

function text(key) {
  const dictionary = getLocale().dictionary || {};
  return dictionary[key] || key;
}

export function refreshLoginMode() {
  if (initialRegistrationRequired) recoveryMode = false;
  $('#login-mode-title').textContent = text(initialRegistrationRequired ? 'initialAdminTitle' : 'signInTitle');
  $('#login-mode-description').textContent = text(initialRegistrationRequired ? 'initialAdminDescription' : 'signInDescription');
  $('#login-submit').textContent = text(initialRegistrationRequired ? 'createAdministrator' : 'signIn');
  $('#login-password-confirmation-group').classList.toggle('hidden', !initialRegistrationRequired);
  $('#login-password-confirmation').required = initialRegistrationRequired;
  $('#login-password-group').classList.toggle('hidden', recoveryMode);
  $('#login-recovery-group').classList.toggle('hidden', !recoveryMode);
  $('#login-password').required = initialRegistrationRequired || !recoveryMode;
  $('#login-recovery-code').required = !initialRegistrationRequired && recoveryMode;
  $('#login-password').autocomplete = initialRegistrationRequired ? 'new-password' : 'current-password';
  const recoveryToggleLabel = $('#login-recovery-toggle span');
  if (recoveryToggleLabel) recoveryToggleLabel.textContent = text(recoveryMode ? 'usePassword' : 'useRecoveryCode');
  if (initialRegistrationRequired) $('#login-alt-actions')?.classList.add('hidden');
}

async function loadInitialRegistrationStatus() {
  const status = await api('/auth/bootstrap');
  initialRegistrationRequired = Boolean(status?.required);
  refreshLoginMode();
  if (initialRegistrationRequired) return;
  $('#login-alt-actions')?.classList.remove('hidden');
  try {
    const passkeyStatus = await api('/auth/passkey/status');
    $('#login-passkey-btn')?.classList.toggle('hidden', !passkeyStatus?.enabled);
  } catch (_) {
    // Recovery codes remain usable independently of the Passkey feature.
    $('#login-passkey-btn')?.classList.add('hidden');
  }
}

// showLogin hides the application shell and presents the sign-in form,
// optionally with a message such as an expired-session notice.
export async function showLogin(message = '') {
  state.signedIn = false;
  state.csrf = '';
  document.querySelectorAll('dialog[open]').forEach(dialog => dialog.close());
  $('#app').classList.add('hidden');
  $('#login').classList.remove('hidden');
  $('#login-error').textContent = message;
  try {
    await loadInitialRegistrationStatus();
  } catch (error) {
    showFormError($('#login-error'), error.message);
  }
  $('#login-user')?.focus();
}

function clearSecrets() {
  $('#login-password').value = '';
  $('#login-password-confirmation').value = '';
  $('#login-recovery-code').value = '';
}

async function submitLogin(event) {
  event.preventDefault();
  $('#login-error').textContent = '';
  await withBusy($('#login-submit'), async () => {
    try {
      const username = $('#login-user').value.trim();
      let session;
      if (initialRegistrationRequired) {
        const password = $('#login-password').value;
        const confirmation = $('#login-password-confirmation').value;
        if (password !== confirmation) throw new Error(text('passwordConfirmationMismatch'));
        session = await api('/auth/bootstrap', {
          method: 'POST',
          body: JSON.stringify({username, password, password_confirmation: confirmation})
        });
      } else if (recoveryMode) {
        session = await api('/auth/recovery/login', {
          method: 'POST',
          body: JSON.stringify({username, recovery_code: $('#login-recovery-code').value})
        });
      } else {
        session = await api('/auth/login', {method: 'POST', body: JSON.stringify({username, password: $('#login-password').value})});
      }
      state.csrf = session.csrf_token;
      clearSecrets();
      await signedInHandler();
    } catch (error) {
      showFormError($('#login-error'), error.message);
      if (initialRegistrationRequired) {
        try { await loadInitialRegistrationStatus(); } catch (_) {}
      }
    }
  });
}

export function initAuth(onSignedIn) {
  signedInHandler = onSignedIn;
  $('#login-form').addEventListener('submit', submitLogin);
  $('#login-passkey-btn')?.addEventListener('click', async event => {
    $('#login-error').textContent = '';
    await withBusy(event.currentTarget, async () => {
      try {
        const session = await loginWithPasskey($('#login-user').value.trim());
        state.csrf = session.csrf_token;
        await signedInHandler();
      } catch (error) {
        showFormError($('#login-error'), error.message);
      }
    });
  });
  $('#login-recovery-toggle')?.addEventListener('click', () => {
    recoveryMode = !recoveryMode;
    refreshLoginMode();
    (recoveryMode ? $('#login-recovery-code') : $('#login-password')).focus();
  });
}
