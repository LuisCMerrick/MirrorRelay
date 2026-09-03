// Repository create/edit dialog: profile application, field population and
// submission through the Desired/Active validation workflow.
import { api } from '../api.js';
import { registerAction } from '../actions.js';
import { $, notice } from '../dom.js';
import { parseHeaders, parseLines, parseList, parseUpstreams } from '../forms.js';
import { L } from '../i18n.js';
import { state } from '../state.js';
import { loadDashboard } from './dashboard.js';
import { loadMirrors } from './mirrors.js';

function applyProfile(profile) {
  $('#mirror-name').value = profile.name;
  $('#mirror-slug').value = profile.name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
  $('#repository-type').value = profile.type;
  $('#proxy-mode').value = profile.proxy_mode;
  $('#public-mode').value = profile.public_mode;
  $('#upstream-list').value = `100 ${profile.upstream}`;
  $('#health-path').value = profile.health_path || '';
  $('#cache-enabled').checked = Boolean(profile.cache_enabled);
  $('#cache-profile').value = profile.cache_profile || 'standard';
  $('#cache-authenticated').checked = Boolean(profile.cache_authenticated);
  $('#rewrite-enabled').checked = Boolean(profile.rewrite_enabled);
  $('#html-rewrite-enabled').checked = Boolean(profile.html_rewrite_enabled);
  $('#rewrite-profile').value = profile.rewrite_profile || '';
  $('#rewrite-hosts').value = (profile.rewrite_hosts || []).join('\n');
  $('#auth-mode').value = profile.auth_mode || 'direct';
  $('#blob-redirect').value = profile.blob_redirect_mode || 'full_proxy';
  $('#connect-timeout').value = profile.connect_timeout_sec || 10;
  $('#read-timeout').value = profile.read_timeout_sec || 3600;
  $('#send-timeout').value = profile.send_timeout_sec || 3600;
  $('#metadata-limit').value = profile.metadata_rewrite_limit_bytes || 0;
  $('#metadata-ttl').value = profile.metadata_ttl_sec || 0;
  $('#package-ttl').value = profile.package_ttl_sec || 0;
  $('#immutable-ttl').value = profile.immutable_ttl_sec || 0;
  $('#blob-ttl').value = profile.blob_ttl_sec || 0;
  if (profile.help) {
    $('#help-enabled').checked = Boolean(profile.help.enabled);
    $('#help-template').value = profile.help.template || '';
    $('#help-title').value = profile.help.title || '';
    $('#help-summary').value = profile.help.summary || '';
  } else {
    $('#help-enabled').checked = false;
    $('#help-template').value = '';
    $('#help-title').value = '';
    $('#help-summary').value = '';
  }
}

function openMirrorForm(repository = null) {
  $('#mirror-form').reset();
  const set = (selector, value) => { $(selector).value = value ?? ''; };
  set('#mirror-id', repository?.id || '');
  $('#form-title').textContent = repository ? L('Edit repository') : L('Add repository');
  set('#mirror-name', repository?.name); set('#mirror-slug', repository?.slug); set('#repository-type', repository?.type || 'generic');
  set('#public-mode', repository?.public_mode || 'path'); set('#public-host', repository?.public_host); set('#public-path', repository?.public_path); set('#access-policy', repository?.access_policy || 'public');
  set('#mirror-description', repository?.description); set('#proxy-mode', repository?.proxy_mode || 'transparent'); set('#redirect-mode', repository?.redirect_mode || 'rewrite');
  set('#upstream-list', (repository?.upstreams || []).map(upstream => `${upstream.priority || 100} ${upstream.url}`).join('\n'));
  set('#strip-prefix', repository?.strip_prefix); set('#add-prefix', repository?.add_prefix); set('#host-rewrite', repository?.host_rewrite);
  set('#header-add', Object.entries(repository?.header_add || {}).map(([name, value]) => `${name}: ${value}`).join('\n'));
  set('#header-remove', (repository?.header_remove || []).join('\n'));
  set('#connect-timeout', repository?.connect_timeout_sec || 10); set('#read-timeout', repository?.read_timeout_sec || 3600); set('#send-timeout', repository?.send_timeout_sec || 3600);
  set('#cache-profile', repository?.cache_profile || 'standard'); set('#rewrite-profile', repository?.rewrite_profile || ''); set('#rewrite-hosts', (repository?.rewrite_hosts || []).join('\n'));
  set('#metadata-limit', repository?.metadata_rewrite_limit_bytes || 0); set('#metadata-ttl', repository?.metadata_ttl_sec || 0); set('#package-ttl', repository?.package_ttl_sec || 0);
  set('#immutable-ttl', repository?.immutable_ttl_sec || 0); set('#blob-ttl', repository?.blob_ttl_sec || 0);
  set('#health-path', repository?.health_check_path); set('#health-expected', repository?.health_expected || 200); set('#health-interval', repository?.health_interval_sec || 60);
  set('#health-timeout', repository?.health_timeout_sec || 5); set('#health-method', repository?.health_method || 'HEAD'); set('#rate-profile', repository?.rate_limit_profile || '');
  set('#max-concurrency', repository?.max_concurrency || 0); set('#bandwidth-limit', repository?.bandwidth_limit_bps || 0);
  set('#auth-mode', repository?.auth_mode || 'direct'); set('#blob-redirect', repository?.blob_redirect_mode || 'full_proxy'); set('#token-upstream', repository?.token_upstream);
  $('#mirror-enabled').checked = repository?.enabled ?? true; $('#cache-enabled').checked = repository?.cache_enabled ?? false; $('#cache-authenticated').checked = repository?.cache_authenticated ?? false;
  $('#rewrite-enabled').checked = repository?.rewrite_enabled ?? false; $('#html-rewrite-enabled').checked = repository?.html_rewrite_enabled ?? false; $('#health-enabled').checked = repository?.health_check_enabled ?? true; $('#pull-only').checked = repository?.pull_only ?? true;
  $('#allow-http').checked = repository?.allow_http_upstream ?? false; $('#allow-private').checked = repository?.allow_private_upstream ?? false;
  set('#blocked-packages', (repository?.blocked_packages || []).join('\n'));
  set('#allowed-packages', (repository?.allowed_packages || []).join('\n'));
  $('#help-enabled').checked = Boolean(repository?.help?.enabled);
  set('#help-template', repository?.help?.template || '');
  set('#help-title', repository?.help?.title || '');
  set('#help-summary', repository?.help?.summary || '');
  const profileIndex = state.profiles.findIndex(profile => profile.name === repository?.profile_name && profile.version === repository?.profile_version);
  $('#template').value = profileIndex >= 0 ? String(profileIndex) : '';
  $('#form-error').textContent = '';
  $('#mirror-dialog').showModal();
}

async function submitMirrorForm(event) {
  event.preventDefault();
  const submitButton = event.target.querySelector('button[type="submit"]');
  $('#form-error').textContent = '';
  const id = $('#mirror-id').value;
  const templateValue = $('#template').value;
  const selectedProfile = templateValue === '' ? null : state.profiles[Number(templateValue)];
  try {
    submitButton.disabled = true;
    submitButton.setAttribute('aria-busy', 'true');
    const body = {
      name: $('#mirror-name').value, slug: $('#mirror-slug').value, type: $('#repository-type').value,
      profile_name: selectedProfile?.name || 'Custom', profile_version: selectedProfile?.version || '1.0.0',
      enabled: $('#mirror-enabled').checked, description: $('#mirror-description').value,
      public_mode: $('#public-mode').value, public_host: $('#public-host').value, public_path: $('#public-path').value,
      access_policy: $('#access-policy').value, proxy_mode: $('#proxy-mode').value, redirect_mode: $('#redirect-mode').value,
      upstreams: parseUpstreams($('#upstream-list').value), strip_prefix: $('#strip-prefix').value, add_prefix: $('#add-prefix').value, host_rewrite: $('#host-rewrite').value,
      header_add: parseHeaders($('#header-add').value), header_remove: parseList($('#header-remove').value),
      connect_timeout_sec: Number($('#connect-timeout').value), read_timeout_sec: Number($('#read-timeout').value), send_timeout_sec: Number($('#send-timeout').value),
      cache_enabled: $('#cache-enabled').checked, cache_profile: $('#cache-profile').value, cache_authenticated: $('#cache-authenticated').checked,
      rewrite_enabled: $('#rewrite-enabled').checked, html_rewrite_enabled: $('#html-rewrite-enabled').checked, rewrite_profile: $('#rewrite-profile').value, rewrite_hosts: parseList($('#rewrite-hosts').value),
      metadata_rewrite_limit_bytes: Number($('#metadata-limit').value), metadata_ttl_sec: Number($('#metadata-ttl').value), package_ttl_sec: Number($('#package-ttl').value),
      immutable_ttl_sec: Number($('#immutable-ttl').value), blob_ttl_sec: Number($('#blob-ttl').value),
      health_check_enabled: $('#health-enabled').checked, health_check_path: $('#health-path').value, health_interval_sec: Number($('#health-interval').value),
      health_timeout_sec: Number($('#health-timeout').value), health_method: $('#health-method').value, health_expected: Number($('#health-expected').value),
      rate_limit_profile: $('#rate-profile').value, max_concurrency: Number($('#max-concurrency').value), bandwidth_limit_bps: Number($('#bandwidth-limit').value),
      auth_mode: $('#auth-mode').value, token_upstream: $('#token-upstream').value, blob_redirect_mode: $('#blob-redirect').value, pull_only: $('#pull-only').checked,
      allow_http_upstream: $('#allow-http').checked, allow_private_upstream: $('#allow-private').checked, insecure_skip_verify: false,
      blocked_packages: parseLines($('#blocked-packages').value),
      allowed_packages: parseLines($('#allowed-packages').value),
      help: {
        enabled: $('#help-enabled').checked,
        template: $('#help-template').value,
        title: $('#help-title').value,
        summary: $('#help-summary').value
      }
    };
    await api(id ? `/mirrors/${id}` : '/mirrors', {method: id ? 'PUT' : 'POST', body: JSON.stringify(body)});
    $('#mirror-dialog').close();
    notice(L('Candidate validated and activated with a graceful reload.'));
    await Promise.all([loadMirrors(), loadDashboard()]);
  } catch (error) {
    $('#form-error').textContent = error.message;
    $('#form-error').focus();
  } finally {
    if (submitButton.isConnected) {
      submitButton.disabled = false;
      submitButton.removeAttribute('aria-busy');
    }
  }
}

export function initMirrorForm() {
  $('#add-mirror').addEventListener('click', () => openMirrorForm());
  $('#close-dialog').addEventListener('click', () => $('#mirror-dialog').close());
  $('#cancel-dialog').addEventListener('click', () => $('#mirror-dialog').close());
  $('#template').addEventListener('change', () => {
    const selected = $('#template').value;
    const profile = selected === '' ? null : state.profiles[Number(selected)];
    if (!profile) return;
    applyProfile(profile);
  });
  $('#mirror-form').addEventListener('submit', submitMirrorForm);
}

registerAction('edit-mirror', button => openMirrorForm(state.mirrors.find(repository => repository.id === Number(button.dataset.id))));

registerAction('edit-mirror-from-detail', button => {
  $('#detail-dialog').close();
  return openMirrorForm(state.mirrors.find(repository => repository.id === Number(button.dataset.id)));
});
