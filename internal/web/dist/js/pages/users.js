// User administration page and the current-account password form.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { $, copyText, esc, notice } from '../dom.js';
import { date } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';
import { state } from '../state.js';

export async function loadUsers() {
  const users = (await api('/users')) || [];
  $('#page-users').innerHTML = `
    <form class="panel narrow" id="user-form">
      <h2>${icon('users', 18)} ${L('Add User / Administrator')}</h2>
      <div class="form-grid">
        <label>
          <span>${L('Username')}</span>
          <input id="new-user" minlength="3" maxlength="64" required autocomplete="username" spellcheck="false" placeholder="operator">
        </label>
        <label>
          <span>${L('Role')}</span>
          <select id="new-user-role">
            <option value="operator" selected>${L('Operator (Repositories & Cache)')}</option>
            <option value="admin">${L('Administrator (Full Control)')}</option>
            <option value="viewer">${L('Viewer (Read-only)')}</option>
          </select>
        </label>
        <label class="full-span">
          <span>${L('Initial password')}</span>
          <input id="new-user-pass" type="password" minlength="10" required autocomplete="new-password" placeholder="••••••••••••">
        </label>
      </div>
      <footer>
        <div id="user-error" class="error" role="alert" tabindex="-1"></div>
        <button type="submit" class="btn-primary">${icon('plus', 13)} ${L('Create user')}</button>
      </footer>
    </form>
    <div class="panel">
      <h2>${icon('users', 18)} ${L('User list')}</h2>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>${L('Username')}</th>
              <th>${L('Role')}</th>
              <th>${L('Created')}</th>
              <th>${L('Updated')}</th>
              <th>${L('Actions')}</th>
            </tr>
          </thead>
          <tbody>
            ${users.map(user => {
              const role = user.role || 'admin';
              const roleClass = role === 'admin' ? 'status-healthy' : (role === 'operator' ? 'status-degraded' : 'status-unknown');
              const roleLabel = role === 'admin' ? L('Admin') : (role === 'operator' ? L('Operator') : L('Viewer'));
              return `<tr>
              <td>
                <div class="user-row-cell">
                  <div class="user-avatar-sm">${icon('user', 12)}</div>
                  <strong>${esc(user.username)}</strong>
                </div>
              </td>
              <td><span class="status-pill ${roleClass}">${roleLabel}</span></td>
              <td><code>${date(user.created_at)}</code></td>
              <td><code>${date(user.updated_at)}</code></td>
              <td>
                <button class="danger small" data-action="delete-user" data-id="${user.id}">
                  ${icon('trash', 12)} ${L('Delete')}
                </button>
              </td>
            </tr>`;
            }).join('')}
          </tbody>
        </table>
      </div>
    </div>`;

  $('#user-form').addEventListener('submit', async event => {
    event.preventDefault();
    const submitButton = event.target.querySelector('button[type="submit"]');
    $('#user-error').textContent = '';
    try {
      submitButton.disabled = true;
      submitButton.setAttribute('aria-busy', 'true');
      await api('/users', {
        method: 'POST',
        body: JSON.stringify({
          username: $('#new-user').value,
          password: $('#new-user-pass').value,
          role: $('#new-user-role').value
        })
      });
      notice(L('User created.'));
      await loadUsers();
    } catch (error) {
      $('#user-error').textContent = error.message;
      $('#user-error').focus();
    } finally {
      submitButton.disabled = false;
      submitButton.removeAttribute('aria-busy');
    }
  });
}

registerAction('delete-user', async button => {
  if (!confirm(L('Delete this administrator account?'))) return;
  try {
    await api(`/users/${Number(button.dataset.id)}`, {method: 'DELETE'});
    notice(L('User deleted.'));
    await loadUsers();
  } catch (error) {
    notice(error.message, true);
  }
});

import { registerPasskey } from '../passkey.js';

registerAction('rename-passkey', async button => {
  const id = button.dataset.id;
  const currentName = button.dataset.name;
  const newName = prompt(L('Enter new passkey name:'), currentName);
  if (!newName || newName.trim() === '' || newName === currentName) return;
  try {
    await api('/account/passkeys/' + id, {
      method: 'PUT',
      body: JSON.stringify({ display_name: newName.trim() })
    });
    notice(L('Passkey renamed.'));
    await loadAccount();
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('delete-passkey', async button => {
  const id = button.dataset.id;
  if (!confirm(L('Are you sure you want to delete this passkey?'))) return;
  try {
    await api('/account/passkeys/' + id, { method: 'DELETE' });
    notice(L('Passkey deleted.'));
    await loadAccount();
  } catch (error) {
    notice(error.message, true);
  }
});

export async function loadAccount() {
  let passkeyData = { passkeys: [], recovery_codes_remaining: 0, password_login_disabled: false, passkey_enabled: false };
  let passkeyError = '';
  try {
    passkeyData = await api('/account/passkeys');
  } catch (error) {
    passkeyError = error.message;
  }

  const passkeysRows = (passkeyData.passkeys || []).map(pk => `
    <tr>
      <td><strong>${esc(pk.display_name)}</strong><br><small class="muted passkey-id-preview">${esc((pk.credential_id || '').substring(0, 16))}...</small></td>
      <td>${date(pk.created_at)}</td>
      <td>${pk.last_used_at ? date(pk.last_used_at) : `<span class="muted">${L('Never')}</span>`}</td>
      <td class="action-cell">
        <button type="button" class="btn-xs secondary" data-action="rename-passkey" data-id="${pk.id}" data-name="${esc(pk.display_name)}">${icon('edit', 12)} ${L('Rename')}</button>
        <button type="button" class="btn-xs danger" data-action="delete-passkey" data-id="${pk.id}">${icon('trash', 12)} ${L('Delete')}</button>
      </td>
    </tr>
  `).join('');

  const passkeySection = passkeyData.passkey_enabled ? `
    <div class="panel account-security-panel">
      <div class="panel-header account-panel-header">
        <div>
          <h2>${icon('shield', 18)} ${L('Passkeys (WebAuthn / FIDO2)')}</h2>
          <p class="muted">${L('Manage hardware security keys and biometric passkeys for fast and secure authentication.')}</p>
        </div>
        <button type="button" class="btn-primary" id="add-passkey-btn">${icon('plus', 13)} ${L('Add Passkey')}</button>
      </div>
      <div class="table-responsive account-passkey-table"><table class="data-table">
        <thead>
          <tr>
            <th>${L('Device / Name')}</th>
            <th>${L('Created')}</th>
            <th>${L('Last Used')}</th>
            <th>${L('Actions')}</th>
          </tr>
        </thead>
        <tbody>
          ${passkeysRows || `<tr><td colspan="4" class="muted">${L('No passkeys registered yet.')}</td></tr>`}
        </tbody>
      </table></div>

      <div class="account-security-section">
        <h3>${icon('key', 16)} ${L('Emergency Recovery Codes')}</h3>
        <p class="muted">${L('Emergency recovery codes allow you to regain access if you lose all your passkeys.')} (${L('Valid codes remaining')}: <strong>${passkeyData.recovery_codes_remaining}</strong>)</p>
        <button type="button" class="secondary section-action" id="generate-recovery-btn">${icon('refresh', 13)} ${L('Generate New Recovery Codes')}</button>
        <div id="recovery-codes-display" class="recovery-codes-display hidden">
          <p><strong>${L('Store these one-time recovery codes in a safe place. They will not be shown again!')}</strong></p>
          <div id="recovery-codes-list" class="recovery-codes-list"></div>
          <button type="button" class="btn-xs secondary" id="copy-recovery-codes-btn">${icon('copy', 12)} ${L('Copy Codes')}</button>
        </div>
      </div>

      <div class="account-security-section">
        <h3>${icon('lock', 16)} ${L('Password Login Policy')}</h3>
        <label class="password-policy-toggle">
          <input type="checkbox" id="disable-password-login-cb" ${passkeyData.password_login_disabled ? 'checked' : ''}>
          <span>${L('Disable password login for this account (Passkey only)')}</span>
        </label>
        <p class="muted password-policy-help">${L('When enabled, you must use a registered Passkey or emergency recovery code to log in.')}</p>
      </div>
    </div>
  ` : `
    <div class="panel account-security-panel">
      <div class="panel-header account-panel-header">
        <div>
          <h2>${icon('shield', 18)} ${L('Passkeys (WebAuthn / FIDO2)')}</h2>
          <p class="muted">${L('Passkey authentication is disabled for this instance.')}</p>
        </div>
        ${state.role === 'admin' ? `<button type="button" class="btn-primary" id="configure-passkey-btn">${icon('settings', 13)} ${L('Configure Passkey authentication')}</button>` : ''}
      </div>
      ${state.role === 'admin' ? `<p class="muted">${L('Enable Passkey in Settings, restart MirrorRelay, then return here to register a credential.')}</p>` : `<p class="muted">${L('Ask an administrator to enable Passkey authentication in Settings.')}</p>`}
    </div>
  `;

  $('#page-account').innerHTML = `
    ${passkeyError ? `<div class="notice error" role="alert">${esc(passkeyError)}</div>` : ''}
    <form class="panel narrow" id="password-form">
      <h2>${icon('user', 18)} ${L('Change password')}</h2>
      <div class="form-grid single-col">
        <label>
          <span>${L('Current password')}</span>
          <input id="old-pass" type="password" required autocomplete="current-password" placeholder="••••••••••••">
        </label>
        <label>
          <span>${L('New password (at least 10 characters)')}</span>
          <input id="new-pass" type="password" minlength="10" required autocomplete="new-password" placeholder="••••••••••••">
        </label>
      </div>
      <footer>
        <div class="error" id="pass-error" role="alert" tabindex="-1"></div>
        <button type="submit" class="btn-primary">${icon('check', 13)} ${L('Update password')}</button>
      </footer>
    </form>
    ${passkeySection}`;

  $('#password-form').addEventListener('submit', async event => {
    event.preventDefault();
    const submitButton = event.target.querySelector('button[type="submit"]');
    $('#pass-error').textContent = '';
    try {
      submitButton.disabled = true;
      submitButton.setAttribute('aria-busy', 'true');
      await api('/auth/password', {method: 'PUT', body: JSON.stringify({current_password: $('#old-pass').value, new_password: $('#new-pass').value})});
      notice(L('Password updated.'));
      event.target.reset();
    } catch (error) {
      $('#pass-error').textContent = error.message;
      $('#pass-error').focus();
    } finally {
      submitButton.disabled = false;
      submitButton.removeAttribute('aria-busy');
    }
  });

  $('#add-passkey-btn')?.addEventListener('click', async event => {
    const name = prompt(L('Enter a name for this Passkey (e.g. MacBook Touch ID, YubiKey):'), 'Passkey');
    if (name === null) return;
    if (new TextEncoder().encode(name.trim()).length > 128) {
      notice(L('Passkey names must not exceed 128 bytes.'), true);
      return;
    }
    const button = event.currentTarget;
    try {
      button.disabled = true;
      button.setAttribute('aria-busy', 'true');
      await registerPasskey(name.trim() || 'Passkey');
      notice(L('Passkey registered successfully!'));
      await loadAccount();
    } catch (error) {
      notice(error.message, true);
    } finally {
      if (button.isConnected) {
        button.disabled = false;
        button.removeAttribute('aria-busy');
      }
    }
  });

  $('#configure-passkey-btn')?.addEventListener('click', () => {
    state.settingsSection = 'passkey';
    document.querySelector('nav button[data-page="settings"]')?.click();
  });

  let generatedCodes = [];
  $('#generate-recovery-btn')?.addEventListener('click', async event => {
    if (!confirm(L('Generating new recovery codes will invalidate any existing codes. Continue?'))) return;
    const button = event.currentTarget;
    try {
      button.disabled = true;
      button.setAttribute('aria-busy', 'true');
      const res = await api('/account/recovery/generate', { method: 'POST' });
      generatedCodes = res.recovery_codes || [];
      const container = $('#recovery-codes-list');
      if (container) {
        container.innerHTML = generatedCodes.map(c => `<code>${esc(c)}</code>`).join('');
        $('#recovery-codes-display').classList.remove('hidden');
        $('#recovery-codes-display').setAttribute('tabindex', '-1');
        $('#recovery-codes-display').focus();
      }
      notice(L('Recovery codes generated.'));
    } catch (error) {
      notice(error.message, true);
    } finally {
      if (button.isConnected) {
        button.disabled = false;
        button.removeAttribute('aria-busy');
      }
    }
  });

  $('#copy-recovery-codes-btn')?.addEventListener('click', async event => {
    if (generatedCodes.length > 0) {
      const button = event.currentTarget;
      try {
        button.disabled = true;
        button.setAttribute('aria-busy', 'true');
        await copyText(generatedCodes.join('\n'));
        notice(L('Recovery codes copied to clipboard!'));
      } catch (error) {
        notice(error.message, true);
      } finally {
        button.disabled = false;
        button.removeAttribute('aria-busy');
      }
    }
  });

  $('#disable-password-login-cb')?.addEventListener('change', async event => {
    const disabled = event.target.checked;
    event.target.disabled = true;
    try {
      await api('/account/security/password-login', {
        method: 'PUT',
        body: JSON.stringify({ disabled })
      });
      notice(disabled ? L('Password login disabled. Passkey is now required.') : L('Password login re-enabled.'));
    } catch (error) {
      event.target.checked = !disabled;
      notice(error.message, true);
    } finally {
      event.target.disabled = false;
    }
  });
}
