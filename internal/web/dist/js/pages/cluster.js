// Cluster page: overview cards, edge node table and the node dialog.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { card, disclosure, kv } from '../components.js';
import { $, esc, notice } from '../dom.js';
import { date } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';
import { state } from '../state.js';

export async function loadCluster() {
  const [overview, nodes] = await Promise.all([
    api('/cluster/overview').catch(() => ({role: 'standalone', enabled: false})),
    api('/cluster/nodes').catch(() => [])
  ]);

  const overviewDetails = [
    kv(L('Cluster role'), overview.role || 'standalone'),
    kv(L('Routing mode'), overview.routing_mode || 'hybrid'),
    kv(L('Cluster Fingerprint'), overview.cluster_fingerprint || L('Not initialized'))
  ].join('');
  const canOperate = state.role === 'admin' || state.role === 'operator';
  const canManageNodes = state.role === 'admin';
  const overviewHtml = `<div class="cards compact-cards">
    ${card(L('Cluster status'), overview.enabled ? L('Enabled') : L('Disabled'), overview.enabled, 'cluster', overview.role || 'standalone')}
    ${card(L('Healthy nodes'), `${overview.healthy_nodes || 0} / ${overview.total_nodes || 0}`, (overview.healthy_nodes || 0) > 0, 'activity', L('healthy / total'))}
    ${card(L('Routable nodes'), overview.routable_nodes || 0, (overview.routable_nodes || 0) > 0, 'network')}
  </div>
  ${disclosure(L('Cluster configuration details'), overviewDetails, {
    iconName: 'shield',
    description: L('Role, routing mode and active fingerprint')
  })}`;

  const nodeRows = (nodes || []).map(node => {
    const isHealthy = node.health_status === 'healthy';
    const isMatch = node.config_status === 'match';
    return `<tr>
      <td><strong>${esc(node.name)}</strong></td>
      <td><code>${esc(node.url)}</code></td>
      <td>${esc(node.region)}${node.country ? ` <span class="badge">${esc(node.country)}</span>` : ''}</td>
      <td><code>${node.priority} / ${node.weight}</code></td>
      <td><span class="badge ${isHealthy ? 'ok' : 'bad'}"><span class="pulse-dot ${isHealthy ? 'green' : 'red'}"></span>${esc(node.health_status || 'unknown')}</span></td>
      <td><span class="badge ${isMatch ? 'ok' : 'bad'}">${esc(node.config_status || 'unknown')}</span></td>
      <td><code title="${esc(node.config_fingerprint)}">${esc((node.config_fingerprint || '').slice(0, 15))}...</code></td>
      <td>${node.last_check ? date(node.last_check) : '—'}</td>
      ${canOperate ? `<td>
        <div class="actions">
          <button class="small secondary requires-operator" data-action="sync-node" data-id="${node.id}">${icon('refresh', 12)} ${L('Sync')}</button>
          <button class="small secondary requires-operator" data-action="check-node" data-id="${node.id}">${icon('play', 12)} ${L('Check')}</button>
          ${canManageNodes ? `
            <button class="small secondary requires-admin" data-action="edit-node" data-id="${node.id}">${icon('edit', 12)} ${L('Edit')}</button>
            <button class="small secondary requires-admin" data-action="toggle-node" data-id="${node.id}" data-enabled="${node.enabled}">${node.enabled ? L('Disable') : L('Enable')}</button>
            <button class="small danger requires-admin" data-action="delete-node" data-id="${node.id}" title="${L('Delete')}" aria-label="${L('Delete')}">${icon('trash', 12)}</button>
          ` : ''}
        </div>
      </td>` : ''}
    </tr>`;
  }).join('');

  const tableHtml = `<div class="panel">
    <h2>${icon('share', 18)} ${L('Edge nodes')}</h2>
    <div class="table-wrap"><table><thead><tr>
      <th>${L('Name')}</th>
      <th>${L('URL')}</th>
      <th>${L('Region')}</th>
      <th>${L('Priority / Weight')}</th>
      <th>${L('Health')}</th>
      <th>${L('Config')}</th>
      <th>${L('Fingerprint')}</th>
      <th>${L('Last check')}</th>
      ${canOperate ? `<th>${L('Actions')}</th>` : ''}
    </tr></thead><tbody>${nodeRows || `<tr><td colspan="${canOperate ? 9 : 8}" class="empty">${L('No edge nodes registered yet.')}</td></tr>`}</tbody></table></div>
  </div>`;

  $('#cluster-overview').innerHTML = overviewHtml;
  $('#cluster-node-list').innerHTML = tableHtml;
}

export function initCluster() {
  $('#add-node')?.addEventListener('click', () => {
    $('#node-form').reset();
    $('#node-id').value = '';
    $('#node-form-title').textContent = L('Add edge node');
    $('#node-mutation-token').required = true;
    $('#node-mutation-token').placeholder = L('Required for a new Edge');
    $('#node-enabled').checked = true;
    $('#node-priority').value = '100';
    $('#node-weight').value = '100';
    $('#node-error').textContent = '';
    $('#node-dialog').showModal();
  });
  $('#close-node-dialog')?.addEventListener('click', () => $('#node-dialog').close());
  $('#cancel-node-dialog')?.addEventListener('click', () => $('#node-dialog').close());

  $('#sync-all-nodes')?.addEventListener('click', async () => {
    try {
      notice(L('Synchronizing all cluster nodes...'));
      const results = await api('/cluster/sync', {method: 'POST'});
      const successCount = (results || []).filter(r => r.success).length;
      notice(L('Cluster sync completed: %s/%s nodes in sync.', successCount, (results || []).length));
      await loadCluster();
    } catch (error) {
      notice(error.message, true);
    }
  });

  $('#node-form')?.addEventListener('submit', async event => {
    event.preventDefault();
    $('#node-error').textContent = '';
    const id = $('#node-id').value;
    const payload = {
      name: $('#node-name').value.trim(),
      url: $('#node-url').value.trim(),
      mutation_token: $('#node-mutation-token').value,
      region: $('#node-region').value.trim(),
      country: $('#node-country').value.trim().toUpperCase(),
      priority: Number($('#node-priority').value) || 100,
      weight: Number($('#node-weight').value) || 100,
      enabled: $('#node-enabled').checked
    };
    try {
      if (id) {
        await api(`/cluster/nodes/${id}`, {method: 'PUT', body: JSON.stringify(payload)});
        notice(L('Node updated.'));
      } else {
        await api('/cluster/nodes', {method: 'POST', body: JSON.stringify(payload)});
        notice(L('Node added.'));
      }
      $('#node-dialog').close();
      await loadCluster();
    } catch (error) {
      $('#node-error').textContent = error.message;
    }
  });

  $('#reset-cluster-fp')?.addEventListener('click', async () => {
    if (!confirm(L('Reset the cluster configuration fingerprint? It will reinitialize from active nodes.'))) return;
    try {
      await api('/cluster/fingerprint/reset', {method: 'POST'});
      notice(L('Cluster fingerprint reset.'));
      await loadCluster();
    } catch (error) {
      notice(error.message, true);
    }
  });
}

registerAction('sync-node', async button => {
  try {
    const res = await api(`/cluster/nodes/${Number(button.dataset.id)}/sync`, {method: 'POST'});
    if (res.success) {
      notice(L('Node synchronized successfully (%s ms).', res.latency_ms));
    } else {
      notice(L('Sync failed: %s', res.error), true);
    }
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('check-node', async button => {
  try {
    await api(`/cluster/nodes/${Number(button.dataset.id)}/check`, {method: 'POST'});
    notice(L('Node probe completed.'));
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('edit-node', async button => {
  try {
    const nodes = await api('/cluster/nodes');
    const node = (nodes || []).find(n => n.id === Number(button.dataset.id));
    if (!node) return;
    $('#node-id').value = node.id;
    $('#node-name').value = node.name || '';
    $('#node-url').value = node.url || '';
    $('#node-mutation-token').value = '';
    $('#node-mutation-token').required = false;
    $('#node-mutation-token').placeholder = L('Leave blank to keep the current token');
    $('#node-region').value = node.region || '';
    $('#node-country').value = node.country || '';
    $('#node-priority').value = node.priority || 100;
    $('#node-weight').value = node.weight || 100;
    $('#node-enabled').checked = node.enabled;
    $('#node-form-title').textContent = L('Edit edge node');
    $('#node-error').textContent = '';
    $('#node-dialog').showModal();
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('toggle-node', async button => {
  const currentEnabled = button.dataset.enabled === 'true';
  try {
    const action = currentEnabled ? 'disable' : 'enable';
    await api(`/cluster/nodes/${Number(button.dataset.id)}/${action}`, {method: 'POST'});
    notice(currentEnabled ? L('Node disabled.') : L('Node enabled.'));
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
});

registerAction('delete-node', async button => {
  if (!confirm(L('Delete this edge node?'))) return;
  try {
    await api(`/cluster/nodes/${Number(button.dataset.id)}`, {method: 'DELETE'});
    notice(L('Node deleted.'));
    await loadCluster();
  } catch (error) {
    notice(error.message, true);
  }
});
