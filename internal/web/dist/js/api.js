// Thin fetch wrapper for the management API. Paths stay relative so the UI
// works under any administrator-chosen mount point.
import { L } from './i18n.js';
import { state } from './state.js';

const DEFAULT_TIMEOUT_MS = 30000;

export class ApiError extends Error {
  constructor(message, status = 0) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

let unauthorizedHandler = () => {};

// onUnauthorized registers the callback invoked when an authenticated request
// is rejected with 401, i.e. the session expired or was revoked server-side.
export function onUnauthorized(handler) {
  unauthorizedHandler = handler;
}

function isAuthenticationEndpoint(path) {
  return path.startsWith('/auth/');
}

export async function apiResponse(path, options = {}) {
  const {timeout = DEFAULT_TIMEOUT_MS, signal, ...rest} = options;
  const request = {credentials: 'same-origin', ...rest, headers: {...(rest.headers || {})}};
  if (request.body && !request.headers['Content-Type']) request.headers['Content-Type'] = 'application/json';
  const method = (request.method || 'GET').toUpperCase();
  if (state.csrf && !['GET', 'HEAD'].includes(method)) request.headers['X-CSRF-Token'] = state.csrf;

  const controller = new AbortController();
  const abort = () => controller.abort();
  signal?.addEventListener('abort', abort, {once: true});
  const timer = timeout > 0 ? setTimeout(abort, timeout) : null;
  request.signal = controller.signal;

  let response;
  try {
    response = await fetch('api/v1' + path, request);
  } catch (error) {
    if (error?.name === 'AbortError' && !signal?.aborted) throw new ApiError(L('Request timed out.'), 0);
    throw error instanceof Error ? error : new ApiError(String(error));
  } finally {
    if (timer) clearTimeout(timer);
    signal?.removeEventListener('abort', abort);
  }

  if (!response.ok) {
    let body = null;
    try { body = await response.clone().json(); } catch (_) {}
    if (response.status === 401 && state.signedIn && !isAuthenticationEndpoint(path)) unauthorizedHandler();
    throw new ApiError(body?.error || `HTTP ${response.status}`, response.status);
  }
  return response;
}

export async function api(path, options = {}) {
  const response = await apiResponse(path, options);
  if (response.status === 204) return null;
  const text = await response.text();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch (_) {
    throw new ApiError(L('Invalid JSON response from %s.', path), response.status);
  }
}
