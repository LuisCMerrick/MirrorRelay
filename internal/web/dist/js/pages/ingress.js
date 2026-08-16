// Ingress integration page: External Shared Nginx snippet.
import { api } from '../api.js';
import { card } from '../components.js';
import { $, esc } from '../dom.js';
import { L } from '../i18n.js';

export async function loadIngress() {
  const integration = await api('/ingress/snippet');
  $('#page-ingress').innerHTML = `<div class="cards">${card(L('Ingress mode'), integration.mode)}${card(L('Frontend network'), integration.frontend_network)}${card(L('Frontend address'), integration.frontend_address)}</div><div class="panel"><h2>${L('External Shared Nginx integration snippet')}</h2><p class="muted">${L('The generated file is a scoped deployment aid. MirrorRelay does not own or reload the External Shared Nginx process.')}</p><pre class="config-preview">${esc(integration.configuration)}</pre></div>`;
}
