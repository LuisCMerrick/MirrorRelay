// Settings page: schema-driven form for the SQLite-backed operational
// settings that override matching YAML values after restart.
import { api } from '../api.js';
import { kv } from '../components.js';
import { $, esc, notice } from '../dom.js';
import { nestedValue, parseList, setNestedValue } from '../forms.js';
import { icon } from '../icons.js';
import { getLocale, L } from '../i18n.js';
import { triggerRestart } from '../restart.js';

const webhookTestProviders = {
  configured: {
    label: 'Running configuration',
    help: 'Uses the single webhook destination in the running process. Saved changes waiting for a restart are not used.'
  },
  dingtalk: {
    label: 'DingTalk',
    host: 'oapi.dingtalk.com',
    placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=...',
    help: 'Sends the DingTalk Markdown payload format to this one-time target.'
  },
  feishu: {
    label: 'Feishu / Lark',
    host: 'open.feishu.cn',
    placeholder: 'https://open.feishu.cn/open-apis/bot/v2/hook/...',
    help: 'Sends the Feishu rich-post payload format to this one-time target.'
  },
  wecom: {
    label: 'WeCom',
    host: 'qyapi.weixin.qq.com',
    placeholder: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...',
    help: 'Sends the WeCom Markdown payload format to this one-time target.'
  },
  slack: {
    label: 'Slack',
    host: 'hooks.slack.com',
    placeholder: 'https://hooks.slack.com/services/...',
    help: 'Sends the Slack webhook payload format to this one-time target.'
  },
  custom: {
    label: 'Custom JSON webhook',
    placeholder: 'https://alerts.example.com/hooks/mirrorrelay',
    help: 'Hosts not recognized as a built-in provider receive the standard MirrorRelay JSON payload.'
  }
};

function webhookProviderOptions() {
  return Object.entries(webhookTestProviders)
    .map(([value, provider]) => `<option value="${value}">${L(provider.label)}</option>`)
    .join('');
}

function updateWebhookTestFields() {
  const provider = webhookTestProviders[$('#webhook-test-provider').value] || webhookTestProviders.configured;
  const temporary = provider !== webhookTestProviders.configured;
  $('#webhook-test-url-field').classList.toggle('hidden', !temporary);
  $('#webhook-test-secret-field').classList.toggle('hidden', !temporary);
  $('#webhook-test-url').required = temporary;
  $('#webhook-test-url').placeholder = provider.placeholder || '';
  $('#webhook-test-provider-help').textContent = L(provider.help);
  $('#webhook-test-error').textContent = '';
}

function webhookTestPayload() {
  const providerKey = $('#webhook-test-provider').value;
  const provider = webhookTestProviders[providerKey] || webhookTestProviders.configured;
  if (providerKey === 'configured') return {};

  const value = $('#webhook-test-url').value.trim();
  let target;
  try {
    target = new URL(value);
  } catch (_) {
    throw new Error(L('Enter a valid absolute Webhook URL.'));
  }
  if (!['http:', 'https:'].includes(target.protocol)) {
    throw new Error(L('Webhook URLs must use HTTP or HTTPS.'));
  }
  if (provider.host && target.hostname.toLowerCase() !== provider.host) {
    throw new Error(L('The selected provider requires a URL on %s.', provider.host));
  }
  return { url: value, secret: $('#webhook-test-secret').value };
}

function settingsInput(field, settings) {
  const value = nestedValue(settings, field.path);
  const label = field.label;
  const attributes = `data-setting-path="${esc(field.path)}" data-setting-type="${esc(field.valueType || field.type)}"`;
  if (field.type === 'boolean') {
    return `<label class="check"><input type="checkbox" ${attributes}${value ? ' checked' : ''}><span>${esc(label)}</span></label>`;
  }
  if (field.type === 'select') {
    const options = field.options.map(option => `<option value="${esc(option[0])}"${String(option[0]) === String(value) ? ' selected' : ''}>${esc(option[1])}</option>`).join('');
    return `<label><span>${esc(label)}</span><select ${attributes}>${options}</select></label>`;
  }
  if (field.type === 'list') {
    return `<label class="wide"><span>${esc(label)}</span><textarea rows="3" ${attributes}>${esc((value || []).join('\n'))}</textarea></label>`;
  }
  const limits = `${field.min !== undefined ? ` min="${field.min}"` : ''}${field.max !== undefined ? ` max="${field.max}"` : ''}`;
  return `<label><span>${esc(label)}</span><input type="${field.type}" value="${esc(value ?? '')}" placeholder="${esc(field.placeholder || '')}" ${attributes}${limits}></label>`;
}

export async function loadSettings() {
  const response = await api('/settings');
  const settings = response.settings;
  const loc = getLocale();
  const settingsGroups = loc.settingsGroups || [];
  const restart = response.restart_required
    ? `<div class="notice error">
        <span>${icon('alert', 16)} ${L('Saved values differ from the running process. Restart MirrorRelay to apply them.')}</span>
        <div class="actions notice-actions">
          <button type="button" class="btn-restart-inline" id="restart-service-btn">
            ${icon('restart', 12)} ${L('Restart now')}
          </button>
          <code>sudo systemctl restart mirrorrelay</code>
        </div>
      </div>`
    : `<div class="notice">${icon('check-circle', 16)} ${L('The running process matches the saved settings.')}</div>`;

  const groups = settingsGroups.map(group => `
    <fieldset>
      <legend>${esc(group.title)}</legend>
      <div class="form-grid">
        ${group.fields.map(field => settingsInput(field, settings)).join('')}
      </div>
    </fieldset>
  `).join('');

  $('#page-settings').innerHTML = `
    ${restart}
    <div class="panel">
      <h2>${icon('settings', 18)} ${L('Operational settings')}</h2>
      <p>${L('These operational settings are stored in SQLite, strictly validated, and override the matching YAML values after restart. Repository changes continue to use the immediate Desired/Active validation workflow.')}</p>
      ${kv(L('Source'), response.source === 'web_ui' ? L('Web UI override') : L('Configuration file'))}
      <p class="muted">${L('File-only bootstrap settings:')} <code>${esc((response.file_only || []).join(', '))}</code></p>
    </div>
    <form id="settings-form" class="settings-form">
      ${groups}
      <footer>
        <div id="settings-error" class="error"></div>
        <button type="button" class="secondary" id="reset-settings">${icon('refresh', 13)} ${L('Reset to YAML after restart')}</button>
        <button type="button" class="secondary" id="restart-settings-btn">${icon('restart', 13)} ${L('Restart MirrorRelay')}</button>
        <button type="submit" class="btn-primary">${icon('check', 13)} ${L('Validate and save')}</button>
      </footer>
    </form>
    <div class="panel" id="webhook-test-panel">
      <h2>${icon('send', 18)} ${L('Webhook notification test')}</h2>
      <div class="info-callout">
        <strong>${L('One active destination')}</strong>
        <p>${L('MirrorRelay has one configured Webhook destination at a time. Its payload format is detected from the URL. The platform choices below test one target; they do not add notification channels.')}</p>
      </div>
      <form id="webhook-test-form">
        <div class="form-grid">
          <label>
            <span>${L('Test destination')}</span>
            <select id="webhook-test-provider">${webhookProviderOptions()}</select>
            <small id="webhook-test-provider-help" class="field-help"></small>
          </label>
          <label class="wide hidden" id="webhook-test-url-field">
            <span>${L('One-time Webhook URL')}</span>
            <input id="webhook-test-url" type="url" autocomplete="off">
          </label>
          <label class="wide hidden" id="webhook-test-secret-field">
            <span>${L('Optional HMAC signing secret')}</span>
            <input id="webhook-test-secret" type="password" autocomplete="new-password">
            <small class="field-help">${L('Used only for this test in the X-MirrorRelay-Signature header; platform access tokens remain part of the Webhook URL.')}</small>
          </label>
        </div>
        <footer>
          <div id="webhook-test-error" class="error" role="alert"></div>
          <button type="submit" class="btn-primary" id="send-test-webhook-btn">${icon('send', 13)} ${L('Send test notification')}</button>
        </footer>
      </form>
    </div>`;

  const restartNoticeBtn = $('#restart-service-btn');
  if (restartNoticeBtn) restartNoticeBtn.addEventListener('click', triggerRestart);
  const restartFooterBtn = $('#restart-settings-btn');
  if (restartFooterBtn) restartFooterBtn.addEventListener('click', triggerRestart);

  const webhookTestForm = $('#webhook-test-form');
  const webhookTestBtn = $('#send-test-webhook-btn');
  $('#webhook-test-provider').addEventListener('change', updateWebhookTestFields);
  updateWebhookTestFields();
  if (webhookTestForm && webhookTestBtn) {
    webhookTestForm.addEventListener('submit', async event => {
      event.preventDefault();
      $('#webhook-test-error').textContent = '';
      try {
        webhookTestBtn.disabled = true;
        await api('/webhooks/test', {
          method: 'POST',
          body: JSON.stringify(webhookTestPayload())
        });
        notice(L('Test Webhook notification delivered successfully.'));
      } catch (err) {
        $('#webhook-test-error').textContent = err.message;
      } finally {
        webhookTestBtn.disabled = false;
      }
    });
  }

  $('#settings-form').addEventListener('submit', async event => {
    event.preventDefault();
    const next = JSON.parse(JSON.stringify(settings));
    event.target.querySelectorAll('[data-setting-path]').forEach(input => {
      let value;
      if (input.dataset.settingType === 'boolean') value = input.checked;
      else if (input.dataset.settingType === 'number') value = Number(input.value);
      else if (input.dataset.settingType === 'list') value = parseList(input.value);
      else value = input.value.trim();
      setNestedValue(next, input.dataset.settingPath, value);
    });
    try {
      const saved = await api('/settings', {method: 'PUT', body: JSON.stringify(next)});
      notice(saved.restart_required ? L('Settings saved; restart MirrorRelay to apply them.') : L('Settings already match the running process.'));
      await loadSettings();
    } catch (error) {
      $('#settings-error').textContent = error.message;
    }
  });

  $('#reset-settings').addEventListener('click', async () => {
    if (!confirm(L('Discard the Web UI override and restore YAML values after restart?'))) return;
    try {
      await api('/settings', {method: 'DELETE'});
      notice(L('Web UI override removed; restart MirrorRelay.'));
      await loadSettings();
    } catch (error) {
      $('#settings-error').textContent = error.message;
    }
  });
}
