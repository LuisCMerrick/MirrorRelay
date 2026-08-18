// Settings page: schema-driven form for the SQLite-backed operational
// settings that override matching YAML values after restart.
import { api } from '../api.js';
import { kv } from '../components.js';
import { $, esc, notice } from '../dom.js';
import { nestedValue, parseList, setNestedValue } from '../forms.js';
import { icon } from '../icons.js';
import { getLocale, L } from '../i18n.js';
import { triggerRestart } from '../restart.js';

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
        <button type="button" class="secondary" id="reset-settings">${icon('refresh', 13)} ${L('Reset to YAML after restart')}</button>
        <button type="button" class="secondary" id="restart-settings-btn">${icon('restart', 13)} ${L('Restart MirrorRelay')}</button>
        <button type="submit" class="btn-primary">${icon('check', 13)} ${L('Validate and save')}</button>
      </footer>
      <div id="settings-error" class="error"></div>
    </form>
    <div class="panel" id="webhook-test-panel">
      <h2>${icon('send', 18)} ${L('Webhook Alerting & Test Notification')}</h2>
      <p>${L('Send a test event notification to verify your configured DingTalk, Feishu, WeCom, Slack or custom webhook endpoint.')}</p>
      <div class="form-grid">
        <label class="wide">
          <span>${L('Target Webhook URL (leave empty to use saved settings)')}</span>
          <input id="webhook-test-url" type="url" placeholder="https://oapi.dingtalk.com/... or https://open.feishu.cn/...">
        </label>
      </div>
      <footer>
        <button type="button" class="btn-primary" id="send-test-webhook-btn">${icon('send', 13)} ${L('Send Test Notification')}</button>
      </footer>
    </div>`;

  const restartNoticeBtn = $('#restart-service-btn');
  if (restartNoticeBtn) restartNoticeBtn.addEventListener('click', triggerRestart);
  const restartFooterBtn = $('#restart-settings-btn');
  if (restartFooterBtn) restartFooterBtn.addEventListener('click', triggerRestart);

  const webhookTestBtn = $('#send-test-webhook-btn');
  if (webhookTestBtn) {
    webhookTestBtn.addEventListener('click', async () => {
      try {
        webhookTestBtn.disabled = true;
        const res = await api('/webhooks/test', {
          method: 'POST',
          body: JSON.stringify({ url: $('#webhook-test-url').value.trim() })
        });
        notice(L('Test webhook notification delivered successfully!'));
      } catch (err) {
        notice(err.message, true);
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
