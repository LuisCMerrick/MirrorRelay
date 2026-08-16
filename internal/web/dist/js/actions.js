// Central registry for button[data-action] handlers. Page modules register
// their handlers at import time; the entry point dispatches delegated clicks.
import { L } from './i18n.js';

const handlers = new Map();

export function registerAction(name, handler) {
  handlers.set(name, handler);
}

export async function dispatchAction(button) {
  const handler = handlers.get(button.dataset.action);
  if (!handler) throw new Error(L('Unknown action.'));
  return handler(button);
}
