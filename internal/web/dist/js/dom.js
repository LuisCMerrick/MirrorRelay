// DOM helpers shared by every page module.
import { L } from './i18n.js';

export const $ = selector => document.querySelector(selector);

export const esc = value => String(value ?? '').replace(/[&<>"']/g, character => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[character]));

let noticeTimer;

export function notice(message, bad = false) {
  const target = $('#notice');
  if (!target) return;
  clearTimeout(noticeTimer);
  const item = document.createElement('div');
  item.className = `notice${bad ? ' error' : ''}`;
  item.textContent = String(message ?? '');
  target.replaceChildren(item);
  noticeTimer = setTimeout(() => {
    if (target.firstChild === item) target.replaceChildren();
  }, 4500);
}

export async function copyText(value) {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const input = document.createElement('textarea');
  input.value = value;
  input.setAttribute('readonly', '');
  input.className = 'clipboard-fallback';
  document.body.appendChild(input);
  input.select();
  const copied = document.execCommand('copy');
  input.remove();
  if (!copied) throw new Error(L('Clipboard access is unavailable.'));
}
