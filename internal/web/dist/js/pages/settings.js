// Settings page: schema-driven form for the SQLite-backed operational
// settings that override matching YAML values after restart.
import { api } from '../api.js';
import { kv } from '../components.js';
import { $, esc, notice } from '../dom.js';
import { nestedValue, parseList, setNestedValue } from '../forms.js';
import { getLocale, L } from '../i18n.js';
import { triggerRestart } from '../restart.js';

function settingsInput(field, settings) {
  const value = nestedValue(settings, field.path);
  const label = field.label;
  const attributes = `data-setting-path="${esc(field.path)}" data-setting-type="${esc(field.valueType || field.type)}"`;
  if (field.type === 'boolean') return `<label class="check"><input type="checkbox" ${attributes}${value ? ' checked' : ''}><span>${esc(label)}</span></label>`;
  if (field.type === 'select') {
    const options = field.options.map(option => `<option value="${esc(option[0])}"${String(option[0]) === String(value) ? ' selected' : ''}>${esc(option[1])}</option>`).join('');
    return `<label>${esc(label)}<select ${attributes}>${options}</select></label>`;
  }
  if (field.type === 'list') return `<label class="wide">${esc(label)}<textarea rows="3" ${attributes}>${esc((value || []).join('\n'))}</textarea></label>`;
  const limits = `${field.min !== undefined ? ` min="${field.min}"` : ''}${field.max !== undefined ? ` max="${field.max}"` : ''}`;
  return `<label>${esc(label)}<input type="${field.type}" value="${esc(value ?? '')}" placeholder="${esc(field.placeholder || '')}" ${attributes}${limits}></label>`;
}

export async function loadSettings() {
  const response = await api('/settings');
  const settings = response.settings;
  const loc = getLocale();
  const settingsGroups = loc.settingsGroups || [];
  const restart = response.restart_required
    ? `<div class="notice error"><span>${L('Saved values differ from the running process. Restart MirrorRelay to apply them.')}</span> <div class="actions" class="notice-actions"><button type="button" class="secondary" id="restart-service-btn" style="padding:4px 10px;font-size:13px;">${L('Restart now')}</button> <code>sudo systemctl restart mirrorrelay</code></div></div>`
    : `<div class="notice">${L('The running process matches the saved settings.')}</div>`;
  const groups = settingsGroups.map(group => `<fieldset><legend>${esc(group.title)}</legend><div class="form-grid">${group.fields.map(field => settingsInput(field, settings)).join('')}</div></fieldset>`).join('');
  $('#page-settings').innerHTML = `${restart}<div class="panel"><p>${L('These operational settings are stored in SQLite, strictly validated, and override the matching YAML values after restart. Repository changes continue to use the immediate Desired/Active validation workflow.')}</p>${kv(L('Source'), response.source === 'web_ui' ? L('Web UI override') : L('Configuration file'))}<p class="muted">${L('File-only bootstrap settings:')} <code>${esc((response.file_only || []).join(', '))}</code></p></div>
    <form id="settings-form" class="settings-form">${groups}<footer><button type="button" class="secondary" id="reset-settings">${L('Reset to YAML after restart')}</button><button type="button" class="secondary" id="restart-settings-btn">${L('Restart MirrorRelay')}</button><button type="submit">${L('Validate and save')}</button></footer><div id="settings-error" class="error"></div></form>`;
  const restartNoticeBtn = $('#restart-service-btn');
  if (restartNoticeBtn) restartNoticeBtn.addEventListener('click', triggerRestart);
  const restartFooterBtn = $('#restart-settings-btn');
  if (restartFooterBtn) restartFooterBtn.addEventListener('click', triggerRestart);
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
    } catch (error) { $('#settings-error').textContent = error.message; }
  });
  $('#reset-settings').addEventListener('click', async () => {
    if (!confirm(L('Discard the Web UI override and restore YAML values after restart?'))) return;
    try { await api('/settings', {method: 'DELETE'}); notice(L('Web UI override removed; restart MirrorRelay.')); await loadSettings(); } catch (error) { $('#settings-error').textContent = error.message; }
  });
}
