// Ingress integration page: concise endpoint summary and a hidden-by-default
// External Shared Nginx snippet that can be expanded and copied.
import { api } from '../api.js';
import { card, disclosure } from '../components.js';
import { $, copyText, esc, notice } from '../dom.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';

export async function loadIngress() {
  const integration = await api('/ingress/snippet');
  const snippet = disclosure(
    L('External Shared Nginx integration snippet'),
    `<div class="panel-header-row">
      <p class="muted">${L('The generated file is a scoped deployment aid. MirrorRelay does not own or reload the External Shared Nginx process.')}</p>
      <button id="copy-ingress-snippet" class="secondary small" type="button">
        ${icon('copy', 13)} ${L('Copy snippet')}
      </button>
    </div>
    <pre class="config-preview">${esc(integration.configuration)}</pre>`,
    {
      id: 'ingress-snippet-disclosure',
      iconName: 'code',
      description: L('Hidden by default. Expand to inspect and copy.')
    }
  );

  $('#page-ingress').innerHTML = `
    <div class="cards compact-cards">
      ${card(L('Ingress mode'), integration.mode, false, 'network')}
      ${card(L('Frontend endpoint'), `${integration.frontend_network} · ${integration.frontend_address}`, false, 'server')}
    </div>
    ${snippet}`;

  $('#copy-ingress-snippet')?.addEventListener('click', async () => {
    try {
      await copyText(integration.configuration);
      notice(L('Copied.'));
    } catch (error) {
      notice(error.message, true);
    }
  });
}
