// Health page: primary service health first, with Admin-only component endpoint
// details separated from the per-repository health list.
import { api } from '../api.js';
import { card, disclosure, kv } from '../components.js';
import { $, esc } from '../dom.js';
import { stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { healthBadge } from '../ui.js';
import { L } from '../i18n.js';

export async function loadHealth() {
  const health = await api('/health');
  const repositories = health.repositories || [];
  const enabledRepositories = repositories.filter(repository => repository.health_state !== 'disabled');
  const healthyRepositories = enabledRepositories.filter(repository => repository.health_state === 'healthy').length;
  const repositoryHealthOK = enabledRepositories.length > 0 && healthyRepositories === enabledRepositories.length;
  const endpointLabel = health.upstream_network && health.upstream_address ? ` · ${health.upstream_network} · ${health.upstream_address}` : '';
  const frontendLabel = health.frontend_network && health.frontend_address ? ` · ${health.frontend_network} · ${health.frontend_address}` : '';
  const nginxRunning = health.managed_upstream_nginx === 'running';

  const componentDetails = [
    kv(L('Frontend endpoint'), `${stateLabel(health.frontend_endpoint || health.frontend_socket)}${frontendLabel}`),
    kv(L('External Shared Nginx'), stateLabel(health.external_shared_nginx)),
    kv('Go Router', stateLabel(health.go_router)),
    kv(L('Upstream endpoint'), `${stateLabel(health.upstream_endpoint || health.upstream_socket)}${endpointLabel}`)
  ].join('');

  $('#page-health').innerHTML = `
    <div class="cards compact-cards">
      ${card('MirrorRelay', stateLabel(health.mirrorrelay), health.mirrorrelay === 'healthy', 'activity')}
      ${card(L('Managed Upstream Nginx'), stateLabel(health.managed_upstream_nginx), nginxRunning, 'server')}
      ${card(L('Repositories'), `${healthyRepositories} / ${enabledRepositories.length}`, repositoryHealthOK, 'layers', L('healthy / enabled'))}
    </div>
    ${disclosure(L('Component and endpoint details'), componentDetails, {
      iconName: 'network',
      description: L('Local transports and request-path component health')
    })}
    <div class="panel">
      <h2>${icon('layers', 18)} ${L('Repositories')}</h2>
      <div class="health-repo-list">
        ${repositories.map(repository => {
          return `<div class="kv">
            <span><strong>${esc(repository.name)}</strong></span>
            ${healthBadge(repository.health_state)}
          </div>`;
        }).join('') || `<p class="muted">${L('No repositories yet.')}</p>`}
      </div>
    </div>`;
}
