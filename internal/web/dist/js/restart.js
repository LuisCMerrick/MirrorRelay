// Service restart flow with reconnection polling. The completion callback is
// injected by the entry point so this module does not depend on the router.
import { api } from './api.js';
import { $, notice } from './dom.js';
import { L } from './i18n.js';

let restartCompleted = async () => {};
let restartInProgress = false;


export function onRestartCompleted(handler) {
  restartCompleted = handler;
}

const restartButtons = () => document.querySelectorAll('#restart-header, #restart-sidebar, #restart-service-btn, #restart-settings-btn, #restart-system-btn');

function setButtonLabel(btn, text) {
  const span = btn.querySelector('span');
  if (span) {
    span.textContent = text;
    return;
  }
  for (const child of btn.childNodes) {
    if (child.nodeType === 3 && child.textContent.trim()) {
      child.textContent = ' ' + text;
      return;
    }
  }
  btn.textContent = text;
}

function resetRestartButtons() {
  restartButtons().forEach(btn => {
    btn.disabled = false;
    if (btn.id === 'restart-sidebar') setButtonLabel(btn, L('Restart'));
    else if (btn.id === 'restart-service-btn') setButtonLabel(btn, L('Restart now'));
    else if (btn.id === 'restart-settings-btn') setButtonLabel(btn, L('Restart MirrorRelay'));
    else setButtonLabel(btn, L('Restart service'));
  });
}

export async function triggerRestart() {
  if (restartInProgress) return;
  if (!confirm(L('Restart MirrorRelay service now? The application will reconnect automatically once ready.'))) return;
  restartInProgress = true;
  restartButtons().forEach(btn => {
    btn.disabled = true;
    setButtonLabel(btn, L('Restarting...'));
  });
  try {
    notice(L('Requesting service restart...'));
    await api('/system/restart', {method: 'POST'});
  } catch (error) {
    const errorTarget = $('#settings-error');
    if (errorTarget) {
      errorTarget.textContent = error.message;
      errorTarget.focus();
    }
    notice(error.message, true);
    restartInProgress = false;
    resetRestartButtons();
    return;
  }
  notice(L('MirrorRelay is restarting, reconnecting...'));
  const maxRetries = 30;
  let restarted = false;
  for (let retries = 0; retries < maxRetries; retries++) {
    await new Promise(resolve => setTimeout(resolve, 1000));
    try {
      const ping = await api('/system');
      if (ping && ping.version) {
        restarted = true;
        break;
      }
    } catch (_) {}
  }

  try {
    if (!restarted) {
      notice(L('Restart timed out. Please check server status.'), true);
      return;
    }
    notice(L('MirrorRelay restarted successfully.'));
    await restartCompleted();
  } finally {
    restartInProgress = false;
    resetRestartButtons();
  }
}
