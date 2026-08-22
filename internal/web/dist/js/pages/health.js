// Health page: primary service health first, with component endpoints disclosed
// separately from the per-repository health list.
import { api } from '../api.js';
import { card, disclosure, kv } from '../components.js';
import { $, esc } from '../dom.js';
import { stateLabel } from '../format.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';

export async function loadHealth() {
  const health = await api('/health');
  const repositories = health.repositories || [];
  const enabledRepositories = repositories.filter(repository => repository.health_state !== 'disabled');
  const healthyRepositories = enabledRepositories.filter(repository => repository.health_state === 'healthy').length;
  const repositoryHealthOK = healthyRepositories === enabledRepositories.length;
  const endpointLabel = `${health.upstream_network || 'unix'} · ${health.upstream_address || ''}`;
  const frontendLabel = `${health.frontend_network || 'unix'} · ${health.frontend_address || ''}`;
  const nginxRunning = health.managed_upstream_nginx === 'running';

  const componentDetails = [
    kv(L('Frontend endpoint'), `${stateLabel(health.frontend_endpoint || health.frontend_socket)} · ${frontendLabel}`),
    kv(L('External Shared Nginx'), stateLabel(health.external_shared_nginx)),
    kv('Go Router', stateLabel(health.go_router)),
    kv(L('Upstream endpoint'), `${stateLabel(health.upstream_endpoint || health.upstream_socket)} · ${endpointLabel}`)
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
          const isHealthy = repository.health_state === 'healthy';
          const isDisabled = repository.health_state === 'disabled';
          return `<div class="kv">
            <span><strong>${esc(repository.name)}</strong></span>
            <span class="badge ${isHealthy ? 'ok' : (isDisabled ? '' : (repository.health_state === 'unknown' ? 'yellow' : 'bad'))}">
              ${isDisabled ? '' : `<span class="pulse-dot ${isHealthy ? 'green' : 'red'}"></span>`}
              ${esc(stateLabel(repository.health_state))}
            </span>
          </div>`;
        }).join('') || `<p class="muted">${L('No repositories yet.')}</p>`}
      </div>
    </div>`;
}
