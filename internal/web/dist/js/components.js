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

export function disclosure(title, content, options = {}) {
  const iconHtml = options.iconName ? icon(options.iconName, 17) : '';
  const description = options.description
    ? `<span class="disclosure-description">${esc(options.description)}</span>`
    : '';
  const className = options.className ? ` ${esc(options.className)}` : '';
  const open = options.open ? ' open' : '';
  const id = options.id ? ` id="${esc(options.id)}"` : '';
  return `<details class="disclosure-panel${className}"${id}${open}>
    <summary>
      <span class="disclosure-heading">
        <span class="disclosure-title">${iconHtml}${esc(title)}</span>
        ${description}
      </span>
      <span class="disclosure-chevron">${icon('chevron-right', 16)}</span>
    </summary>
    <div class="disclosure-content">${content}</div>
  </details>`;
}

export function showPreview(title, content) {
  $('#preview-title').textContent = title;
  $('#preview-content').innerHTML = content;
  $('#preview-dialog').showModal();
}
