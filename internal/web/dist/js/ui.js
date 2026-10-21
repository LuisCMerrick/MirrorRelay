// Shared UI building blocks: status badges, empty rows and busy-state handling.
import { esc } from './dom.js';
import { stateLabel } from './format.js';
import { L } from './i18n.js';

// healthClass maps a health or result state to a badge colour class.
export function healthClass(health) {
  if (health === 'healthy' || health === 'ok' || health === 'match') return 'ok';
  if (health === 'disabled') return '';
  if (!health || health === 'unknown' || health === 'pending') return 'yellow';
  return 'bad';
}

// healthBadge renders the shared health pill used by repository, upstream,
// cluster and health views so every page agrees on colours and labels.
export function healthBadge(health, {title = ''} = {}) {
  const value = health || 'unknown';
  const cls = healthClass(value);
  const dot = value === 'disabled' ? '' : `<span class="pulse-dot ${cls === 'ok' ? 'green' : (cls === 'yellow' ? 'yellow' : 'red')}"></span>`;
  const titleAttribute = title ? ` title="${esc(title)}"` : '';
  return `<span class="badge ${cls}"${titleAttribute}>${dot}${esc(stateLabel(value))}</span>`;
}

export function emptyRow(columns, message) {
  return `<tr><td colspan="${Number(columns) || 1}" class="empty">${esc(message)}</td></tr>`;
}

// withBusy disables a control for the duration of an asynchronous operation and
// restores it afterwards, even if the control was re-rendered meanwhile.
export async function withBusy(control, operation) {
  if (!control) return operation();
  if (control.getAttribute('aria-busy') === 'true') return undefined;
  const wasDisabled = control.disabled;
  control.disabled = true;
  control.setAttribute('aria-busy', 'true');
  try {
    return await operation();
  } finally {
    if (control.isConnected) {
      control.disabled = wasDisabled;
      control.removeAttribute('aria-busy');
    }
  }
}

// showFormError writes an error into a form's alert region and moves focus to
// it so screen readers announce the failure.
export function showFormError(target, message) {
  if (!target) return;
  target.textContent = message || '';
  if (message) {
    if (!target.hasAttribute('tabindex')) target.setAttribute('tabindex', '-1');
    target.focus();
  }
}

// ask opens the shared in-app dialog. Unlike window.confirm/prompt it follows
// the theme and language, never blocks the event loop and works in embedded
// or automated browsers that suppress native dialogs.
function ask({title, message, input = null, confirmLabel, danger = false}) {
  const dialog = document.getElementById('ask-dialog');
  if (!dialog || dialog.open) return Promise.resolve(input ? null : false);
  const form = document.getElementById('ask-form');
  const field = document.getElementById('ask-input');
  const confirmButton = document.getElementById('ask-confirm');
  document.getElementById('ask-title').textContent = title || L('Confirm');
  document.getElementById('ask-message').textContent = message || '';
  document.getElementById('ask-input-group').classList.toggle('hidden', !input);
  document.getElementById('ask-input-label').textContent = input?.label || '';
  field.value = input?.value || '';
  field.placeholder = input?.placeholder || '';
  field.maxLength = input?.maxLength || 524288;
  confirmButton.textContent = confirmLabel || L('OK');
  confirmButton.classList.toggle('danger', danger);
  return new Promise(resolve => {
    let result = input ? null : false;
    const onSubmit = event => {
      event.preventDefault();
      result = input ? field.value : true;
      dialog.close();
    };
    const onCancel = () => dialog.close();
    const onClose = () => {
      form.removeEventListener('submit', onSubmit);
      document.getElementById('ask-cancel').removeEventListener('click', onCancel);
      dialog.removeEventListener('close', onClose);
      resolve(result);
    };
    form.addEventListener('submit', onSubmit);
    document.getElementById('ask-cancel').addEventListener('click', onCancel);
    dialog.addEventListener('close', onClose);
    dialog.showModal();
    (input ? field : (danger ? document.getElementById('ask-cancel') : confirmButton)).focus();
  });
}

export function confirmDialog(message, {title, confirmLabel, danger = false} = {}) {
  return ask({title, message, confirmLabel, danger});
}

export function promptDialog(message, {title, label, value = '', placeholder = '', maxLength, confirmLabel} = {}) {
  return ask({title, message, input: {label, value, placeholder, maxLength}, confirmLabel});
}
