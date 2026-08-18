// User administration page and the current-account password form.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { $, esc, notice } from '../dom.js';
import { date } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';

export async function loadUsers() {
  const users = (await api('/users')) || [];
  $('#page-users').innerHTML = `
    <form class="panel narrow" id="user-form">
      <h2>${icon('users', 18)} ${L('Add User / Administrator')}</h2>
      <div class="form-grid">
        <label>
          <span>${L('Username')}</span>
          <input id="new-user" minlength="3" maxlength="64" required placeholder="operator">
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
          <input id="new-user-pass" type="password" minlength="10" required placeholder="••••••••••••">
        </label>
      </div>
      <footer>
        <button type="submit" class="btn-primary">${icon('plus', 13)} ${L('Create user')}</button>
      </footer>
      <div id="user-error" class="error"></div>
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
    try {
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

export async function loadAccount() {
  $('#page-account').innerHTML = `
    <form class="panel narrow" id="password-form">
      <h2>${icon('user', 18)} ${L('Change password')}</h2>
      <div class="form-grid single-col">
        <label>
          <span>${L('Current password')}</span>
          <input id="old-pass" type="password" required placeholder="••••••••••••">
        </label>
        <label>
          <span>${L('New password (at least 10 characters)')}</span>
          <input id="new-pass" type="password" minlength="10" required placeholder="••••••••••••">
        </label>
      </div>
      <footer>
        <button type="submit" class="btn-primary">${icon('check', 13)} ${L('Update password')}</button>
      </footer>
      <div class="error" id="pass-error"></div>
    </form>`;

  $('#password-form').addEventListener('submit', async event => {
    event.preventDefault();
    try {
      await api('/auth/password', {method: 'PUT', body: JSON.stringify({current_password: $('#old-pass').value, new_password: $('#new-pass').value})});
      notice(L('Password updated.'));
      event.target.reset();
    } catch (error) {
      $('#pass-error').textContent = error.message;
    }
  });
}
