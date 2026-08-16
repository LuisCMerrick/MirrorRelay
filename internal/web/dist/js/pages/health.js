// Health page: component status cards and per-repository health.
import { api } from '../api.js';
import { card } from '../components.js';
import { $, esc } from '../dom.js';
import { stateLabel } from '../format.js';
import { L } from '../i18n.js';

export async function loadHealth() {
  const health = await api('/health');
  const endpointLabel = `${health.upstream_network || 'unix'} · ${health.upstream_address || ''}`;
  const frontendLabel = `${health.frontend_network || 'unix'} · ${health.frontend_address || ''}`;
  $('#page-health').innerHTML = `<div class="cards">${card('MirrorRelay', health.mirrorrelay, health.mirrorrelay === 'healthy')}${card(`${L('Frontend endpoint')} (${frontendLabel})`, health.frontend_endpoint || health.frontend_socket, health.frontend_endpoint === 'healthy')}${card(L('External Shared Nginx'), health.external_shared_nginx)}${card('Go Router', health.go_router)}${card(L('Managed Upstream Nginx'), stateLabel(health.managed_upstream_nginx), health.managed_upstream_nginx === 'running')}${card(`${L('Upstream endpoint')} (${endpointLabel})`, health.upstream_endpoint || health.upstream_socket, health.upstream_endpoint === 'healthy')}</div><div class="panel"><h2>${L('Repositories')}</h2>${(health.repositories || []).map(repository => `<div class="kv"><span>${esc(repository.name)}</span><span class="badge ${repository.health_state === 'healthy' ? 'ok' : repository.health_state === 'unhealthy' ? 'bad' : ''}">${esc(stateLabel(repository.health_state))}</span></div>`).join('')}</div>`;
}
