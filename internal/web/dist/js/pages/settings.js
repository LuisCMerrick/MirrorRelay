// Settings page: schema-driven form for the SQLite-backed operational
// settings that override matching YAML values after restart.
import { api, apiResponse } from '../api.js';
import { kv } from '../components.js';
import { $, copyText, esc, notice } from '../dom.js';
import { nestedValue, parseList, setNestedValue } from '../forms.js';
import { icon } from '../icons.js';
import { L } from '../i18n.js';
import { triggerRestart } from '../restart.js';
import { settingsGroups } from '../settings-schema.js';
import { state } from '../state.js';

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

let previewedImportYAML = null;

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

function fileOnlySetting(path, fileOnlyPaths) {
  return fileOnlyPaths.some(rule => rule === path || (rule.endsWith('.*') && path.startsWith(rule.slice(0, -1))));
}

function settingsInput(field, settings, fileOnlyPaths) {
  const value = nestedValue(settings, field.path);
  const label = L(field.label);
  const fileOnly = fileOnlySetting(field.path, fileOnlyPaths);
  const redacted = value === '[REDACTED]';
  const attributes = `data-setting-path="${esc(field.path)}" data-setting-type="${esc(field.valueType || field.type)}"${redacted ? ' data-redacted="true"' : ''}${fileOnly ? ' disabled aria-disabled="true"' : ''}`;
  const help = [
    fileOnly ? L('Configured only through YAML or environment variables because it is needed before the settings database opens.') : '',
    field.help ? L(field.help) : ''
  ].filter(Boolean).map(message => `<small class="field-help">${esc(message)}</small>`).join('');
  if (field.type === 'boolean') {
    return `<label class="check${fileOnly ? ' setting-file-only' : ''}"><input type="checkbox" ${attributes}${value ? ' checked' : ''}><span>${esc(label)}</span>${help}</label>`;
  }
  if (field.type === 'select') {
    const options = field.options.map(option => `<option value="${esc(option[0])}"${String(option[0]) === String(value) ? ' selected' : ''}>${esc(L(option[1]))}</option>`).join('');
    return `<label class="${fileOnly ? 'setting-file-only' : ''}"><span>${esc(label)}</span><select ${attributes}>${options}</select>${help}</label>`;
  }
  if (field.type === 'list') {
    return `<label class="wide${fileOnly ? ' setting-file-only' : ''}"><span>${esc(label)}</span><textarea rows="3" ${attributes}>${esc((value || []).join('\n'))}</textarea>${help}</label>`;
  }
  const limits = `${field.min !== undefined ? ` min="${field.min}"` : ''}${field.max !== undefined ? ` max="${field.max}"` : ''}`;
  const placeholder = redacted ? L('Configured; leave blank to keep the current value') : (field.placeholder || '');
  return `<label class="${fileOnly ? 'setting-file-only' : ''}"><span>${esc(label)}</span><input type="${field.type}" value="${redacted ? '' : esc(value ?? '')}" placeholder="${esc(placeholder)}" ${attributes}${limits}>${help}</label>`;
}

function renderClientNetworksManager(networks) {
  const list = networks || [];
  const rows = list.map((net, i) => `
    <tr data-net-index="${i}">
      <td><input type="text" class="net-cidr" value="${esc(net.CIDR || net.cidr || '')}" placeholder="192.168.1.0/24"></td>
      <td><input type="text" class="net-region" value="${esc(net.Region || net.region || '')}" placeholder="ap-east"></td>
      <td class="table-actions"><button type="button" class="link danger remove-net-row">${icon('trash', 12)} ${L('Remove')}</button></td>
    </tr>
  `).join('');

  return `
    <div class="wide mapping-manager">
      <div class="mapping-manager-header">
        <strong>${L('Client network routing mappings')}</strong>
        <button type="button" class="secondary btn-sm" id="add-net-row-btn">${icon('plus', 12)} ${L('Add mapping')}</button>
      </div>
      <div class="table-responsive"><table class="data-table" id="client-networks-table">
        <thead>
          <tr>
            <th>${L('CIDR')}</th>
            <th>${L('Region Code')}</th>
            <th>${L('Actions')}</th>
          </tr>
        </thead>
        <tbody id="client-networks-tbody">
          ${rows || `<tr class="mapping-empty-row"><td colspan="3" class="muted">${L('No mappings configured')}</td></tr>`}
        </tbody>
      </table></div>
    </div>
  `;
}

function renderRegionsManager(regions) {
  const list = regions || [];
  const rows = list.map((reg, i) => `
    <tr data-reg-index="${i}">
      <td><input type="text" class="reg-code" value="${esc(reg.Code || reg.code || '')}" placeholder="ap-east"></td>
      <td><input type="text" class="reg-countries" value="${esc((reg.Countries || reg.countries || []).join(', '))}" placeholder="HK, TW, JP"></td>
      <td class="table-actions"><button type="button" class="link danger remove-reg-row">${icon('trash', 12)} ${L('Remove')}</button></td>
    </tr>
  `).join('');

  return `
    <div class="wide mapping-manager">
      <div class="mapping-manager-header">
        <strong>${L('Region country mappings')}</strong>
        <button type="button" class="secondary btn-sm" id="add-reg-row-btn">${icon('plus', 12)} ${L('Add mapping')}</button>
      </div>
      <div class="table-responsive"><table class="data-table" id="regions-table">
        <thead>
          <tr>
            <th>${L('Region Code')}</th>
            <th>${L('Country Codes (comma separated)')}</th>
            <th>${L('Actions')}</th>
          </tr>
        </thead>
        <tbody id="regions-tbody">
          ${rows || `<tr class="mapping-empty-row"><td colspan="3" class="muted">${L('No mappings configured')}</td></tr>`}
        </tbody>
      </table></div>
    </div>
  `;
}

function parseClientNetworksTable() {
  const rows = document.querySelectorAll('#client-networks-tbody tr');
  const result = [];
  rows.forEach(tr => {
    const cidrInput = tr.querySelector('.net-cidr');
    const regInput = tr.querySelector('.net-region');
    if (cidrInput && regInput) {
      const cidr = cidrInput.value.trim();
      const region = regInput.value.trim();
      if (cidr && region) {
        result.push({ cidr, region });
      }
    }
  });
  return result;
}

function parseRegionsTable() {
  const rows = document.querySelectorAll('#regions-tbody tr');
  const result = [];
  rows.forEach(tr => {
    const codeInput = tr.querySelector('.reg-code');
    const countriesInput = tr.querySelector('.reg-countries');
    if (codeInput && countriesInput) {
      const code = codeInput.value.trim();
      const rawCountries = countriesInput.value.trim();
      if (code) {
        const countries = rawCountries ? rawCountries.split(',').map(s => s.trim().toUpperCase()).filter(Boolean) : [];
        result.push({ code, countries });
      }
    }
  });
  return result;
}

export async function loadSettings() {
  const response = await api('/settings');
  const settings = response.settings;
  const fileOnlyPaths = response.file_only || [];

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

  const groups = settingsGroups.map((group, index) => {
    let extraHTML = '';
    if (group.fields.some(field => field.path.startsWith('distributed.'))) {
      const networks = settings.distributed?.routing?.client_networks || [];
      const regions = settings.distributed?.routing?.regions || [];
      extraHTML = renderClientNetworksManager(networks) + renderRegionsManager(regions);
    }
    const badgeText = group.effect === 'immediate' ? L('Immediate') : (group.effect === 'reload' ? L('Reload required') : L('Restart required'));
    const badgeClass = group.effect === 'immediate' ? 'status-pill status-healthy' : 'status-pill status-pending';
    const groupAttribute = group.id ? ` data-settings-group="${esc(group.id)}"` : '';
    const open = index === 0 || (group.id && state.settingsSection === group.id);

    return `
      <details class="disclosure-panel settings-section"${groupAttribute}${open ? ' open' : ''}>
        <summary>
          <span class="disclosure-heading">
            <span class="disclosure-title">${icon('settings', 17)} ${esc(L(group.title))}</span>
            ${group.description ? `<span class="disclosure-description">${esc(L(group.description))}</span>` : ''}
            <span class="${badgeClass} settings-effect-badge">${badgeText}</span>
          </span>
          <span class="disclosure-chevron">${icon('chevron-right', 16)}</span>
        </summary>
        <div class="disclosure-content form-grid">
          ${group.fields.map(field => settingsInput(field, settings, fileOnlyPaths)).join('')}
          ${extraHTML}
        </div>
      </details>
    `;
  }).join('');

  $('#page-settings').innerHTML = `
    ${restart}
    <div class="panel">
      <div class="settings-overview-header">
        <div>
          <h2>${icon('settings', 18)} ${L('Operational settings')}</h2>
          <p>${L('These operational settings are stored in SQLite and strictly validated. Environment variables remain highest priority, followed by the Web UI override and YAML. Saved operational changes take effect after restart; repository changes continue to use the Desired/Active validation workflow.')}</p>
          ${kv(L('Source'), response.source === 'web_ui' ? L('Web UI override') : L('Configuration file'))}
        </div>
        <div class="actions settings-overview-actions">
          <button type="button" class="secondary" id="export-settings-btn">${icon('download', 13)} ${L('Export configuration')}</button>
          <button type="button" class="secondary" id="import-settings-btn">${icon('upload', 13)} ${L('Import configuration')}</button>
          <button type="button" class="secondary" id="history-settings-btn">${icon('history', 13)} ${L('Configuration history')}</button>
        </div>
      </div>
    </div>

    <div id="history-panel" class="panel settings-history-panel hidden">
      <div class="settings-history-header">
        <h2>${icon('history', 18)} ${L('Configuration Version History')}</h2>
        <button type="button" class="secondary btn-sm" id="close-history-btn">${L('Close')}</button>
      </div>
      <div class="table-responsive">
        <table class="data-table">
          <thead>
            <tr>
              <th>${L('Version')}</th>
              <th>${L('Time')}</th>
              <th>${L('Triggered by')}</th>
              <th>${L('Source')}</th>
              <th>${L('Description')}</th>
              <th>${L('Actions')}</th>
            </tr>
          </thead>
          <tbody id="history-tbody">
            <tr><td colspan="6" class="muted">${L('Loading history...')}</td></tr>
          </tbody>
        </table>
      </div>
    </div>

    <form id="settings-form" class="settings-form">
      ${groups}
      <footer>
        <div id="settings-error" class="error" role="alert" tabindex="-1"></div>
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

  const requestedSection = state.settingsSection;
  state.settingsSection = '';
  if (requestedSection) {
    const requestedPanel = document.querySelector(`[data-settings-group="${requestedSection}"]`);
    if (requestedPanel) {
      requestedPanel.open = true;
      requestAnimationFrame(() => {
        requestedPanel.scrollIntoView({ block: 'start' });
        requestedPanel.querySelector('summary')?.focus();
      });
    }
  }

  function removeMappingPlaceholder(tbody) {
    tbody.querySelector('.mapping-empty-row')?.remove();
  }

  function restoreMappingPlaceholder(tbody) {
    if (tbody.querySelector('input')) return;
    tbody.innerHTML = `<tr class="mapping-empty-row"><td colspan="3" class="muted">${L('No mappings configured')}</td></tr>`;
  }

  // Attach table row controls for client networks and regions.
  const addNetBtn = $('#add-net-row-btn');
  if (addNetBtn) {
    addNetBtn.addEventListener('click', () => {
      const tbody = $('#client-networks-tbody');
      removeMappingPlaceholder(tbody);
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td><input type="text" class="net-cidr" placeholder="10.0.0.0/8"></td>
        <td><input type="text" class="net-region" placeholder="ap-east"></td>
        <td class="table-actions"><button type="button" class="link danger remove-net-row">${icon('trash', 12)} ${L('Remove')}</button></td>
      `;
      tbody.appendChild(tr);
    });
  }

  const addRegBtn = $('#add-reg-row-btn');
  if (addRegBtn) {
    addRegBtn.addEventListener('click', () => {
      const tbody = $('#regions-tbody');
      removeMappingPlaceholder(tbody);
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td><input type="text" class="reg-code" placeholder="ap-east"></td>
        <td><input type="text" class="reg-countries" placeholder="HK, TW, JP"></td>
        <td class="table-actions"><button type="button" class="link danger remove-reg-row">${icon('trash', 12)} ${L('Remove')}</button></td>
      `;
      tbody.appendChild(tr);
    });
  }

  $('#settings-form').addEventListener('click', e => {
    if (e.target.closest('.remove-net-row')) {
      const tbody = $('#client-networks-tbody');
      e.target.closest('tr').remove();
      restoreMappingPlaceholder(tbody);
    }
    if (e.target.closest('.remove-reg-row')) {
      const tbody = $('#regions-tbody');
      e.target.closest('tr').remove();
      restoreMappingPlaceholder(tbody);
    }
  });

  // Export Dialog handlers
  const exportDialog = $('#export-dialog');
  const exportBtn = $('#export-settings-btn');
  const closeExportDialog = $('#close-export-dialog');
  const downloadYamlBtn = $('#download-yaml-btn');
  const copyYamlBtn = $('#copy-yaml-btn');

  if (exportBtn && exportDialog) {
    exportBtn.addEventListener('click', () => {
      $('#export-mode-standard').checked = true;
      $('#export-full-warning').classList.add('hidden');
      $('#export-error').textContent = '';
      exportDialog.showModal();
    });
  }
  if (exportDialog && !exportDialog.dataset.bound) {
    exportDialog.dataset.bound = 'true';
    if (closeExportDialog) closeExportDialog.addEventListener('click', () => exportDialog.close());

    $('#export-mode-standard').addEventListener('change', () => {
      $('#export-full-warning').classList.add('hidden');
    });
    $('#export-mode-full').addEventListener('change', () => {
      $('#export-full-warning').classList.remove('hidden');
    });

    if (downloadYamlBtn) {
      downloadYamlBtn.addEventListener('click', async () => {
        const fullBackup = $('#export-mode-full').checked;
        const endpoint = `/settings/export?full_backup=${fullBackup}`;
        try {
          downloadYamlBtn.disabled = true;
          downloadYamlBtn.setAttribute('aria-busy', 'true');
          $('#export-error').textContent = '';
          const res = await apiResponse(endpoint, { method: fullBackup ? 'POST' : 'GET' });
          const blob = await res.blob();
          const url = window.URL.createObjectURL(blob);
          const a = document.createElement('a');
          a.href = url;
          a.download = fullBackup ? 'mirrorrelay-full-backup.yaml' : 'mirrorrelay-config.yaml';
          document.body.appendChild(a);
          a.click();
          a.remove();
          window.URL.revokeObjectURL(url);
          exportDialog.close();
          notice(L('Downloaded YAML configuration.'));
        } catch (err) {
          $('#export-error').textContent = err.message;
          $('#export-error').focus();
        } finally {
          downloadYamlBtn.disabled = false;
          downloadYamlBtn.removeAttribute('aria-busy');
        }
      });
    }

    if (copyYamlBtn) {
      copyYamlBtn.addEventListener('click', async () => {
        const fullBackup = $('#export-mode-full').checked;
        const endpoint = `/settings/export?full_backup=${fullBackup}`;
        try {
          copyYamlBtn.disabled = true;
          copyYamlBtn.setAttribute('aria-busy', 'true');
          $('#export-error').textContent = '';
          const res = await apiResponse(endpoint, { method: fullBackup ? 'POST' : 'GET' });
          const text = await res.text();
          await copyText(text);
          notice(L('YAML copied to clipboard.'));
          exportDialog.close();
        } catch (err) {
          $('#export-error').textContent = err.message;
          $('#export-error').focus();
        } finally {
          copyYamlBtn.disabled = false;
          copyYamlBtn.removeAttribute('aria-busy');
        }
      });
    }
  }

  // Import Dialog handlers
  const importDialog = $('#import-dialog');
  const importBtn = $('#import-settings-btn');
  const closeImportDialog = $('#close-import-dialog');
  const importFileInput = $('#import-file-input');
  const importYamlText = $('#import-yaml-text');
  const importPreviewBtn = $('#import-preview-btn');
  const importApplyBtn = $('#import-apply-btn');
  const importDiffContainer = $('#import-diff-container');
  const importDiffTbody = $('#import-diff-tbody');
  const importDiffSummary = $('#import-diff-summary');
  const importError = $('#import-error');

  if (importBtn && importDialog) {
    importBtn.addEventListener('click', () => {
      previewedImportYAML = null;
      importYamlText.value = '';
      importFileInput.value = '';
      importDiffContainer.classList.add('hidden');
      importApplyBtn.classList.add('hidden');
      importError.textContent = '';
      importDialog.showModal();
    });
  }
  if (importDialog && !importDialog.dataset.bound) {
    importDialog.dataset.bound = 'true';
    const invalidateImportPreview = () => {
      previewedImportYAML = null;
      importDiffContainer.classList.add('hidden');
      importApplyBtn.classList.add('hidden');
    };
    const showImportError = message => {
      importError.textContent = message;
      if (message) importError.focus();
    };
    if (closeImportDialog) closeImportDialog.addEventListener('click', () => importDialog.close());

    importYamlText.addEventListener('input', invalidateImportPreview);

    if (importFileInput) {
      importFileInput.addEventListener('change', e => {
        const file = e.target.files[0];
        if (file) {
          if (file.size > 1024 * 1024) {
            invalidateImportPreview();
            showImportError(L('YAML files must not exceed 1 MiB.'));
            return;
          }
          const reader = new FileReader();
          reader.onload = ev => {
            importYamlText.value = ev.target.result;
            invalidateImportPreview();
          };
          reader.onerror = () => showImportError(L('Unable to read the selected YAML file.'));
          reader.readAsText(file);
        }
      });
    }

    if (importPreviewBtn) {
      importPreviewBtn.addEventListener('click', async () => {
        showImportError('');
        const yaml = importYamlText.value.trim();
        if (!yaml) {
          showImportError(L('Enter or upload YAML configuration content.'));
          return;
        }
        if (new TextEncoder().encode(yaml).length > 1024 * 1024) {
          showImportError(L('YAML files must not exceed 1 MiB.'));
          return;
        }
        try {
          importPreviewBtn.disabled = true;
          importPreviewBtn.setAttribute('aria-busy', 'true');
          const res = await api('/settings/import/preview', {
            method: 'POST',
            body: JSON.stringify({ yaml })
          });
          if (res.valid) {
            importDiffTbody.innerHTML = (res.diff || []).map(d => `
              <tr>
                <td><code>${esc(d.path)}</code></td>
                <td><span class="muted">${esc(d.old_value || '—')}</span></td>
                <td><strong>${esc(d.new_value || '—')}</strong></td>
              </tr>
            `).join('') || `<tr><td colspan="3" class="muted">${L('No changes detected')}</td></tr>`;

            importDiffSummary.innerHTML = `<strong>${esc(res.summary)}</strong> · ${res.restart_required ? L('Restart required') : L('Immediate')}`;
            previewedImportYAML = yaml;
            importDiffContainer.classList.remove('hidden');
            importApplyBtn.classList.remove('hidden');
          }
        } catch (err) {
          previewedImportYAML = null;
          showImportError(err.message);
          importDiffContainer.classList.add('hidden');
          importApplyBtn.classList.add('hidden');
        } finally {
          importPreviewBtn.disabled = false;
          importPreviewBtn.removeAttribute('aria-busy');
        }
      });
    }

    if (importApplyBtn) {
      importApplyBtn.addEventListener('click', async () => {
        showImportError('');
        const yaml = importYamlText.value.trim();
        if (!yaml) return;
        if (yaml !== previewedImportYAML) {
          invalidateImportPreview();
          showImportError(L('The YAML changed after validation. Validate and preview it again before applying.'));
          return;
        }
        try {
          importApplyBtn.disabled = true;
          importApplyBtn.setAttribute('aria-busy', 'true');
          const res = await api('/settings/import', {
            method: 'POST',
            body: JSON.stringify({ yaml })
          });
          notice(res.restart_required ? L('Configuration saved. Restart MirrorRelay to take effect.') : L('Configuration imported successfully.'));
          importDialog.close();
          await loadSettings();
        } catch (err) {
          showImportError(err.message);
        } finally {
          importApplyBtn.disabled = false;
          importApplyBtn.removeAttribute('aria-busy');
        }
      });
    }
  }

  // Configuration History handlers
  const historyPanel = $('#history-panel');
  const historyBtn = $('#history-settings-btn');
  const closeHistoryBtn = $('#close-history-btn');

  async function loadHistory() {
    try {
      const versions = await api('/settings/history');
      const tbody = $('#history-tbody');
      if (!versions || versions.length === 0) {
        tbody.innerHTML = `<tr><td colspan="6" class="muted">${L('No configuration history recorded yet.')}</td></tr>`;
        return;
      }
      tbody.innerHTML = versions.map(v => `
        <tr>
          <td><strong>v${v.version}</strong></td>
          <td>${esc(new Date(v.created_at).toLocaleString())}</td>
          <td>${esc(v.operator || 'system')}</td>
          <td><span class="status-pill status-healthy">${esc(v.source)}</span></td>
          <td>${esc(v.description || '')}</td>
          <td class="table-actions">
            <button type="button" class="link rollback-history-btn" data-version="${v.version}">
              ${icon('refresh', 12)} ${L('Rollback')}
            </button>
          </td>
        </tr>
      `).join('');

      tbody.querySelectorAll('.rollback-history-btn').forEach(btn => {
        btn.addEventListener('click', async () => {
          const ver = btn.dataset.version;
          if (!confirm(L('Confirm rollback to version %s?', ver))) return;
          try {
            btn.disabled = true;
            btn.setAttribute('aria-busy', 'true');
            const res = await api(`/settings/history/${ver}/rollback`, { method: 'POST' });
            notice(res.restart_required ? L('Configuration saved. Restart MirrorRelay to take effect.') : L('Configuration rolled back successfully.'));
            await loadSettings();
          } catch (err) {
            $('#settings-error').textContent = err.message;
            $('#settings-error').focus();
          } finally {
            if (btn.isConnected) {
              btn.disabled = false;
              btn.removeAttribute('aria-busy');
            }
          }
        });
      });
    } catch (err) {
      $('#history-tbody').innerHTML = `<tr><td colspan="6" class="error">${esc(err.message)}</td></tr>`;
    }
  }

  if (historyBtn && historyPanel) {
    historyBtn.addEventListener('click', async () => {
      historyPanel.classList.toggle('hidden');
      if (!historyPanel.classList.contains('hidden')) {
        await loadHistory();
      }
    });
    if (closeHistoryBtn) {
      closeHistoryBtn.addEventListener('click', () => {
        historyPanel.classList.add('hidden');
      });
    }
  }

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
        webhookTestBtn.setAttribute('aria-busy', 'true');
        await api('/webhooks/test', {
          method: 'POST',
          body: JSON.stringify(webhookTestPayload())
        });
        notice(L('Test Webhook notification delivered successfully.'));
      } catch (err) {
        $('#webhook-test-error').textContent = err.message;
        $('#webhook-test-error').setAttribute('tabindex', '-1');
        $('#webhook-test-error').focus();
      } finally {
        webhookTestBtn.disabled = false;
        webhookTestBtn.removeAttribute('aria-busy');
      }
    });
  }

  $('#settings-form').addEventListener('submit', async event => {
    event.preventDefault();
    const submitButton = event.target.querySelector('button[type="submit"]');
    $('#settings-error').textContent = '';
    const next = JSON.parse(JSON.stringify(settings));
    event.target.querySelectorAll('[data-setting-path]').forEach(input => {
      if (input.disabled) return;
      let value;
      if (input.dataset.settingType === 'boolean') value = input.checked;
      else if (input.dataset.settingType === 'number') value = Number(input.value);
      else if (input.dataset.settingType === 'list') value = parseList(input.value);
      else value = input.value.trim();
      setNestedValue(next, input.dataset.settingPath, value);
    });

    // Merge interactive tables for distributed routing
    const clientNetworks = parseClientNetworksTable();
    const regions = parseRegionsTable();
    if (!next.distributed) next.distributed = {};
    if (!next.distributed.routing) next.distributed.routing = {};
    next.distributed.routing.client_networks = clientNetworks;
    next.distributed.routing.regions = regions;

    try {
      submitButton.disabled = true;
      submitButton.setAttribute('aria-busy', 'true');
      const saved = await api('/settings', {method: 'PUT', body: JSON.stringify(next)});
      notice(saved.restart_required ? L('Configuration saved. Restart MirrorRelay to take effect.') : L('Settings already match the running process.'));
      await loadSettings();
    } catch (error) {
      $('#settings-error').textContent = error.message;
      $('#settings-error').focus();
    } finally {
      submitButton.disabled = false;
      submitButton.removeAttribute('aria-busy');
    }
  });

  $('#reset-settings').addEventListener('click', async event => {
    if (!confirm(L('Discard the Web UI override and restore YAML values after restart?'))) return;
    const button = event.currentTarget;
    try {
      button.disabled = true;
      button.setAttribute('aria-busy', 'true');
      await api('/settings', {method: 'DELETE'});
      notice(L('Web UI override removed; restart MirrorRelay.'));
      await loadSettings();
    } catch (error) {
      $('#settings-error').textContent = error.message;
      $('#settings-error').focus();
    } finally {
      if (button.isConnected) {
        button.disabled = false;
        button.removeAttribute('aria-busy');
      }
    }
  });
}
