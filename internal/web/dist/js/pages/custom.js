// Custom Managed Upstream Nginx configuration page and dialog.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { $, esc, notice } from '../dom.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';
import { state } from '../state.js';

export async function loadCustom() {
  state.customConfigs = (await api('/custom-configs')) || [];
  $('#custom-list').innerHTML = `
    <div class="panel">
      <p class="muted">${L('These directives apply only to Managed Upstream Nginx. Dangerous process, filesystem and context-escape directives are rejected.')}</p>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>${L('Name')}</th>
            <th>${L('Context')}</th>
            <th>${L('Repository')}</th>
            <th>${L('State')}</th>
            <th>${L('Last validation')}</th>
            <th>${L('Actions')}</th>
          </tr>
        </thead>
        <tbody>
          ${state.customConfigs.map(value => `<tr>
            <td><strong>${esc(value.name)}</strong></td>
            <td><span class="badge blue">${esc(value.context)}</span></td>
            <td>${value.repository_id ? `<span class="badge">${value.repository_id}</span>` : `<span class="badge ok">${L('Global')}</span>`}</td>
            <td><span class="badge ${value.enabled ? 'ok' : ''}">${value.enabled ? L('Enabled') : L('Disabled')}</span></td>
            <td><code>${esc(value.last_validation_result || '—')}</code></td>
            <td>
              <div class="actions">
                <button data-action="edit-custom" data-id="${value.id}">${icon('edit', 12)} ${L('Edit')}</button>
                <button class="danger" data-action="delete-custom" data-id="${value.id}">${icon('trash', 12)} ${L('Delete')}</button>
              </div>
            </td>
          </tr>`).join('')}
        </tbody>
      </table>
    </div>`;
}

function openCustom(value = null) {
  $('#custom-form').reset();
  $('#custom-id').value = value?.id || '';
  $('#custom-title').textContent = value ? L('Edit custom Managed Upstream Nginx configuration') : L('Add custom Managed Upstream Nginx configuration');
  $('#custom-name').value = value?.name || '';
  $('#custom-context').value = value?.context || 'http';
  $('#custom-repository').value = value?.repository_id || 0;
  $('#custom-enabled').checked = value?.enabled ?? true;
  $('#custom-content').value = value?.content || '';
  $('#custom-error').textContent = '';
  $('#custom-dialog').showModal();
}

export function initCustom() {
  $('#add-custom')?.addEventListener('click', () => openCustom());
  $('#close-custom')?.addEventListener('click', () => $('#custom-dialog')?.close());
  $('#cancel-custom')?.addEventListener('click', () => $('#custom-dialog')?.close());
  $('#custom-form')?.addEventListener('submit', async event => {
    event.preventDefault();
    const id = $('#custom-id').value;
    const body = {
      name: $('#custom-name').value,
      context: $('#custom-context').value,
      repository_id: Number($('#custom-repository').value),
      enabled: $('#custom-enabled').checked,
      content: $('#custom-content').value
    };
    try {
      await api(id ? `/custom-configs/${id}` : '/custom-configs', {method: id ? 'PUT' : 'POST', body: JSON.stringify(body)});
      $('#custom-dialog').close();
      notice(L('Custom configuration validated and activated.'));
      await loadCustom();
    } catch (error) {
      $('#custom-error').textContent = error.message;
    }
  });
}

registerAction('edit-custom', button => openCustom(state.customConfigs.find(value => value.id === Number(button.dataset.id))));

registerAction('delete-custom', async button => {
  if (!confirm(L('Delete this custom configuration and reload Managed Upstream Nginx?'))) return;
  try {
    await api(`/custom-configs/${Number(button.dataset.id)}`, {method: 'DELETE'});
    notice(L('Custom configuration deleted.'));
    await loadCustom();
  } catch (error) {
    notice(error.message, true);
  }
});
