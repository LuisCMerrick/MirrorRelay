// Thin fetch wrapper for the management API. Paths stay relative so the UI
// works under any administrator-chosen mount point.
import { state } from './state.js';

export async function api(path, options = {}) {
  const request = {...options, headers: {...(options.headers || {})}};
  if (request.body) request.headers['Content-Type'] = 'application/json';
  if (state.csrf && request.method && !['GET', 'HEAD'].includes(request.method)) request.headers['X-CSRF-Token'] = state.csrf;
  const response = await fetch('api/v1' + path, request);
  let body = null;
  try { body = await response.json(); } catch (_) {}
  if (!response.ok) throw new Error(body?.error || `HTTP ${response.status}`);
  return body;
}
