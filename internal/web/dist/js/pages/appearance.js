// Appearance page: theme, branding, login page, repository browser and
// custom CSS settings.
import { api } from '../api.js';
import { $, esc, notice } from '../dom.js';
import { L } from '../i18n.js';

export async function loadAppearance() {
  const appearance = await api('/appearance').catch(() => ({
    enabled: false,
    theme: 'system',
    accent_color: '#2563eb',
    branding: { title: 'MirrorRelay', logo: '', favicon: '' },
    login: { title: 'MirrorRelay', subtitle: 'Repository Proxy Service' },
    custom_css: { enabled: false, file: '/var/lib/mirrorrelay/ui/custom.css' },
    repository_browser: { enabled: true }
  }));

  $('#page-appearance').innerHTML = `<div class="panel">
    <h2>${L('Appearance & Branding')}</h2>
    <p class="muted">${L('Configure Web UI appearance, color themes, branding, custom CSS and directory browser. Appearance settings apply immediately across all interface components.')}</p>
  </div>
  <form id="appearance-form" class="settings-form">
    <fieldset><legend>${L('Theme and colors')}</legend><div class="form-grid">
      <label class="check wide"><input id="app-ui-enabled" type="checkbox" ${appearance.enabled ? 'checked' : ''}><span>${L('Enable UI Enhancement (Themes & Repository Browser)')}</span></label>
      <label><span>${L('Theme')}</span><select id="app-theme">
        <option value="system" ${appearance.theme === 'system' ? 'selected' : ''}>System (自动跟随系统)</option>
        <option value="light" ${appearance.theme === 'light' ? 'selected' : ''}>Light (浅色明亮)</option>
        <option value="dark" ${appearance.theme === 'dark' ? 'selected' : ''}>Dark (深色暗黑)</option>
      </select></label>
      <label><span>${L('Accent Color')}</span><input type="color" id="app-accent-color" value="${esc(appearance.accent_color || '#2563eb')}"></label>
    </div></fieldset>

    <fieldset><legend>${L('Branding')}</legend><div class="form-grid">
      <label><span>${L('Instance Name / Title')}</span><input id="app-brand-title" value="${esc(appearance.branding?.title || 'MirrorRelay')}"></label>
      <label><span>${L('Logo URL (optional)')}</span><input id="app-brand-logo" value="${esc(appearance.branding?.logo || '')}"></label>
      <label class="wide"><span>${L('Favicon URL (optional)')}</span><input id="app-brand-favicon" value="${esc(appearance.branding?.favicon || '')}"></label>
    </div></fieldset>

    <fieldset><legend>${L('Login page')}</legend><div class="form-grid">
      <label><span>${L('Login Title')}</span><input id="app-login-title" value="${esc(appearance.login?.title || 'MirrorRelay')}"></label>
      <label><span>${L('Login Subtitle')}</span><input id="app-login-subtitle" value="${esc(appearance.login?.subtitle || 'Repository Proxy Service')}"></label>
    </div></fieldset>

    <fieldset><legend>${L('Repository Browser')}</legend><div class="form-grid">
      <label class="check wide"><input id="app-browser-enabled" type="checkbox" ${appearance.repository_browser?.enabled !== false ? 'checked' : ''}><span>${L('Enable Repository Directory Browser')}</span></label>
    </div></fieldset>

    <fieldset><legend>${L('Custom CSS')}</legend><div class="form-grid">
      <label class="check wide"><input id="app-css-enabled" type="checkbox" ${appearance.custom_css?.enabled ? 'checked' : ''}><span>${L('Enable Custom CSS')}</span></label>
      <label class="wide"><span>${L('Custom CSS File Path')}</span><input id="app-css-file" value="${esc(appearance.custom_css?.file || '/var/lib/mirrorrelay/ui/custom.css')}"></label>
    </div></fieldset>

    <footer>
      <button type="button" class="secondary" id="reset-appearance-btn">${L('Reset appearance to defaults')}</button>
      <button type="submit">${L('Save appearance settings')}</button>
    </footer>
    <div id="appearance-error" class="error"></div>
  </form>`;

  $('#appearance-form').addEventListener('submit', async event => {
    event.preventDefault();
    const payload = {
      enabled: $('#app-ui-enabled').checked,
      theme: $('#app-theme').value,
      accent_color: $('#app-accent-color').value,
      branding: {
        title: $('#app-brand-title').value.trim() || 'MirrorRelay',
        logo: $('#app-brand-logo').value.trim(),
        favicon: $('#app-brand-favicon').value.trim()
      },
      login: {
        title: $('#app-login-title').value.trim() || 'MirrorRelay',
        subtitle: $('#app-login-subtitle').value.trim() || 'Repository Proxy Service'
      },
      repository_browser: {
        enabled: $('#app-browser-enabled').checked
      },
      custom_css: {
        enabled: $('#app-css-enabled').checked,
        file: $('#app-css-file').value.trim() || '/var/lib/mirrorrelay/ui/custom.css'
      }
    };
    try {
      await api('/appearance', {method: 'PUT', body: JSON.stringify(payload)});
      notice(L('Appearance settings saved successfully.'));
      await loadAppearance();
    } catch (err) {
      $('#appearance-error').textContent = err.message;
    }
  });

  $('#reset-appearance-btn').addEventListener('click', async () => {
    if (!confirm(L('Reset appearance settings to default values?'))) return;
    try {
      await api('/appearance/reset', {method: 'POST'});
      notice(L('Appearance settings reset to defaults.'));
      await loadAppearance();
    } catch (err) {
      $('#appearance-error').textContent = err.message;
    }
  });
}
