// Small HTML fragment builders and the shared preview dialog.
import { $, esc } from './dom.js';

export function card(label, value, accent = false) { return `<div class="card"><small>${esc(label)}</small><strong class="${accent ? 'accent' : ''}">${esc(value)}</strong></div>`; }
export function kv(label, value) { return `<div class="kv"><span>${esc(label)}</span><span>${esc(value)}</span></div>`; }

export function showPreview(title, content) {
  $('#preview-title').textContent = title;
  $('#preview-content').innerHTML = content;
  $('#preview-dialog').showModal();
}
