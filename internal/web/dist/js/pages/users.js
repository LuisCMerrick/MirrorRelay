// User administration page and the current-account password form.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { $, esc, notice } from '../dom.js';
import { date } from '../format.js';
import { L } from '../i18n.js';

export async function loadUsers() {
  const users = (await api('/users')) || [];
  $('#page-users').innerHTML = `<form class="panel narrow" id="user-form"><h2>${L('Add administrator')}</h2><div class="form-grid"><label>${L('Username')}<input id="new-user" minlength="3" maxlength="64" required></label><label>${L('Initial password')}<input id="new-user-pass" type="password" minlength="10" required></label></div><button>${L('Create user')}</button><div id="user-error" class="error"></div></form><div class="panel"><h2>${L('User list')}</h2><div class="table-wrap"><table><thead><tr><th>${L('Username')}</th><th>${L('Created')}</th><th>${L('Updated')}</th><th></th></tr></thead><tbody>${users.map(user => `<tr><td>${esc(user.username)}</td><td>${date(user.created_at)}</td><td>${date(user.updated_at)}</td><td><button class="danger" data-action="delete-user" data-id="${user.id}">${L('Delete')}</button></td></tr>`).join('')}</tbody></table></div></div>`;
  $('#user-form').addEventListener('submit', async event => { event.preventDefault(); try { await api('/users', {method: 'POST', body: JSON.stringify({username: $('#new-user').value, password: $('#new-user-pass').value})}); notice(L('User created.')); await loadUsers(); } catch (error) { $('#user-error').textContent = error.message; } });
}

registerAction('delete-user', async button => {
  if (!confirm(L('Delete this administrator account?'))) return;
  try { await api(`/users/${Number(button.dataset.id)}`, {method: 'DELETE'}); notice(L('User deleted.')); await loadUsers(); } catch (error) { notice(error.message, true); }
});

export async function loadAccount() {
  $('#page-account').innerHTML = `<form class="panel narrow" id="password-form"><h2>${L('Change password')}</h2><label>${L('Current password')}<input id="old-pass" type="password" required></label><label>${L('New password (at least 10 characters)')}<input id="new-pass" type="password" minlength="10" required></label><button>${L('Update password')}</button><div class="error" id="pass-error"></div></form>`;
  $('#password-form').addEventListener('submit', async event => { event.preventDefault(); try { await api('/auth/password', {method: 'PUT', body: JSON.stringify({current_password: $('#old-pass').value, new_password: $('#new-pass').value})}); notice(L('Password updated.')); event.target.reset(); } catch (error) { $('#pass-error').textContent = error.message; } });
}
