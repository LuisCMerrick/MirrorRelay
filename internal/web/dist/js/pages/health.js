// Health page: component status cards and per-repository health.
import { api } from '../api.js';
import { card } from '../components.js';
import { $, esc } from '../dom.js';
import { stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';

export async function loadHealth() {
  const health = await api('/health');
  const endpointLabel = `${health.upstream_network || 'unix'} · ${health.upstream_address || ''}`;
  const frontendLabel = `${health.frontend_network || 'unix'} · ${health.frontend_address || ''}`;

  $('#page-health').innerHTML = `
    <div class="cards">
      ${card('MirrorRelay', health.mirrorrelay, health.mirrorrelay === 'healthy', 'activity')}
      ${card(`${L('Frontend endpoint')}`, health.frontend_endpoint || health.frontend_socket, health.frontend_endpoint === 'healthy', 'network', frontendLabel)}
      ${card(L('External Shared Nginx'), health.external_shared_nginx, health.external_shared_nginx === 'healthy', 'server')}
      ${card('Go Router', health.go_router, health.go_router === 'healthy', 'layers')}
      ${card(L('Managed Upstream Nginx'), stateLabel(health.managed_upstream_nginx), health.managed_upstream_nginx === 'running', 'server')}
      ${card(`${L('Upstream endpoint')}`, health.upstream_endpoint || health.upstream_socket, health.upstream_endpoint === 'healthy', 'globe', endpointLabel)}
    </div>
    <div class="panel">
      <h2>${icon('layers', 18)} ${L('Repositories')}</h2>
      <div class="health-repo-list">
        ${(health.repositories || []).map(repository => {
          const isHealthy = repository.health_state === 'healthy';
          return `<div class="kv">
            <span><strong>${esc(repository.name)}</strong></span>
            <span class="badge ${isHealthy ? 'ok' : 'bad'}">
              <span class="pulse-dot ${isHealthy ? 'green' : 'red'}"></span>
              ${esc(stateLabel(repository.health_state))}
            </span>
          </div>`;
        }).join('')}
      </div>
    </div>`;
}
