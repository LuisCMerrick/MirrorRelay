// DOM helpers shared by every page module.
import { L } from './i18n.js';

export const $ = selector => document.querySelector(selector);

export const esc = value => String(value ?? '').replace(/[&<>"']/g, character => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[character]));

export function notice(message, bad = false) {
  $('#notice').innerHTML = `<div class="notice${bad ? ' error' : ''}">${esc(message)}</div>`;
  setTimeout(() => { $('#notice').innerHTML = ''; }, 4500);
}

export async function copyText(value) {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const input = document.createElement('textarea');
  input.value = value;
  input.setAttribute('readonly', '');
  input.style.position = 'fixed';
  input.style.opacity = '0';
  document.body.appendChild(input);
  input.select();
  const copied = document.execCommand('copy');
  input.remove();
  if (!copied) throw new Error(L('Clipboard access is unavailable.'));
}
