// Repository domain helpers shared by the dashboard and repository pages.
export function publicURL(repository) {
  return repository.public_mode === 'host' ? `https://${repository.public_host}/` : `${location.origin}${repository.public_path}`;
}

export function activeUpstreamFor(repository) {
  const healthRank = value => value === 'healthy' ? 0 : (!value || value === 'unknown') ? 1 : 2;
  return [...(repository.upstreams || [])].filter(value => value.enabled).sort((a, b) => healthRank(a.health_status) - healthRank(b.health_status) || a.priority - b.priority)[0] || {};
}

export function healthFor(repository) {
  if (!repository.enabled) return 'disabled';
  const enabled = (repository.upstreams || []).filter(value => value.enabled);
  if (enabled.some(value => value.health_status === 'healthy')) return 'healthy';
  if (!enabled.length || enabled.some(value => !value.health_status || value.health_status === 'unknown')) return 'unknown';
  return 'unhealthy';
}
