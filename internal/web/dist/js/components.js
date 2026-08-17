// Small HTML fragment builders and shared preview dialog.
import { $, esc } from './dom.js';
import { icon } from './icons.js';

export function card(label, value, accent = false, iconName = '', subtext = '') {
  const iconHtml = iconName ? `<div class="card-icon ${accent ? 'accent' : ''}">${icon(iconName, 18)}</div>` : '';
  const subHtml = subtext ? `<div class="card-sub">${esc(subtext)}</div>` : '';
  return `<div class="card">
    <div class="card-head">
      <small>${esc(label)}</small>
      ${iconHtml}
    </div>
    <strong class="${accent ? 'accent' : ''}">${esc(value)}</strong>
    ${subHtml}
  </div>`;
}

export function kv(label, value) {
  return `<div class="kv"><span>${esc(label)}</span><span>${esc(value)}</span></div>`;
}

export function showPreview(title, content) {
  $('#preview-title').textContent = title;
  $('#preview-content').innerHTML = content;
  $('#preview-dialog').showModal();
}
