// Repository domain helpers shared by the dashboard and repository pages.
export function getBasePath() {
  if (typeof window === 'undefined' || !window.location) return '';
  return window.location.pathname.replace(/\/admin\/?$/, '').replace(/\/+$/, '');
}

export function publicURL(repository) {
  if (repository.public_mode === 'host') {
    return `https://${repository.public_host}/`;
  }
  const basePath = getBasePath();
  const repoPath = (repository.public_path || `/${repository.slug}/`).startsWith('/')
    ? (repository.public_path || `/${repository.slug}/`)
    : '/' + (repository.public_path || `/${repository.slug}/`);
  const fullPath = (basePath && !repoPath.startsWith(basePath + '/')) ? `${basePath}${repoPath}` : repoPath;
  return `${location.origin}${fullPath}`;
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
