// Service restart flow with reconnection polling. The completion callback is
// injected by the entry point so this module does not depend on the router.
import { api } from './api.js';
import { $, notice } from './dom.js';
import { L } from './i18n.js';

let restartCompleted = async () => {};

export function onRestartCompleted(handler) {
  restartCompleted = handler;
}

const restartButtons = () => document.querySelectorAll('#restart-header, #restart-sidebar, #restart-service-btn, #restart-settings-btn, #restart-system-btn');

function resetRestartButtons() {
  restartButtons().forEach(btn => {
    btn.disabled = false;
    if (btn.id === 'restart-sidebar') btn.textContent = L('Restart');
    else if (btn.id === 'restart-service-btn') btn.textContent = L('Restart now');
    else if (btn.id === 'restart-settings-btn') btn.textContent = L('Restart MirrorRelay');
    else btn.textContent = L('Restart service');
  });
}

export async function triggerRestart() {
  if (!confirm(L('Restart MirrorRelay service now? The application will reconnect automatically once ready.'))) return;
  try {
    notice(L('Requesting service restart...'));
    await api('/system/restart', {method: 'POST'});
  } catch (error) {
    $('#settings-error').textContent = error.message;
  }
  notice(L('MirrorRelay is restarting, reconnecting...'));
  restartButtons().forEach(btn => {
    btn.disabled = true;
    btn.textContent = L('Restarting...');
  });
  let retries = 0;
  const maxRetries = 30;
  const poll = setInterval(async () => {
    retries++;
    try {
      const ping = await api('/system');
      if (ping && ping.version) {
        clearInterval(poll);
        notice(L('MirrorRelay restarted successfully.'));
        resetRestartButtons();
        await restartCompleted();
      }
    } catch (_) {
      if (retries >= maxRetries) {
        clearInterval(poll);
        notice(L('Restart timed out. Please check server status.'), true);
        resetRestartButtons();
      }
    }
  }, 1000);
}
