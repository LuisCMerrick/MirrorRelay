// Ingress integration page: External Shared Nginx snippet.
import { api } from '../api.js';
import { card } from '../components.js';
import { $, copyText, esc, notice } from '../dom.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';

export async function loadIngress() {
  const integration = await api('/ingress/snippet');
  $('#page-ingress').innerHTML = `
    <div class="cards">
      ${card(L('Ingress mode'), integration.mode, false, 'network')}
      ${card(L('Frontend network'), integration.frontend_network, false, 'server')}
      ${card(L('Frontend address'), integration.frontend_address, false, 'globe')}
    </div>
    <div class="panel">
      <div class="panel-header-row">
        <h2>${icon('network', 18)} ${L('External Shared Nginx integration snippet')}</h2>
        <button id="copy-ingress-snippet" class="secondary small">${icon('copy', 13)} ${L('Copy snippet')}</button>
      </div>
      <p class="muted">${L('The generated file is a scoped deployment aid. MirrorRelay does not own or reload the External Shared Nginx process.')}</p>
      <pre class="config-preview">${esc(integration.configuration)}</pre>
    </div>`;

  $('#copy-ingress-snippet')?.addEventListener('click', async () => {
    try {
      await copyText(integration.configuration);
      notice(L('Copied.'));
    } catch (err) {
      notice(err.message, true);
    }
  });
}
