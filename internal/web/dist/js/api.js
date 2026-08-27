// Thin fetch wrapper for the management API. Paths stay relative so the UI
// works under any administrator-chosen mount point.
import { state } from './state.js';

export async function apiResponse(path, options = {}) {
  const request = {...options, headers: {...(options.headers || {})}};
  if (request.body) request.headers['Content-Type'] = 'application/json';
  if (state.csrf && request.method && !['GET', 'HEAD'].includes(request.method)) request.headers['X-CSRF-Token'] = state.csrf;
  const response = await fetch('api/v1' + path, request);
  if (!response.ok) {
    let body = null;
    try { body = await response.clone().json(); } catch (_) {}
    throw new Error(body?.error || `HTTP ${response.status}`);
  }
  return response;
}

export async function api(path, options = {}) {
  const response = await apiResponse(path, options);
  let body = null;
  try { body = await response.json(); } catch (_) {}
  return body;
}
