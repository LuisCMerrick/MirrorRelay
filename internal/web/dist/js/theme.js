// Browser-local theme preference with an instance-wide default fallback.
import { L } from './i18n.js';

const storageKey = 'mirrorrelay.theme';
const media = window.matchMedia('(prefers-color-scheme: dark)');
const modes = new Set(['auto', 'light', 'dark']);

function normalize(mode) {
  if (mode === 'system') return 'auto';
  return modes.has(mode) ? mode : 'auto';
}

function storedTheme() {
  try {
    const stored = localStorage.getItem(storageKey);
    return modes.has(stored) ? stored : '';
  } catch (_) {
    return '';
  }
}

let preference = normalize(document.documentElement.dataset.themePreference || storedTheme());

function resolvedTheme(mode) {
  return mode === 'auto' ? (media.matches ? 'dark' : 'light') : mode;
}

function updateControls() {
  const labels = {
    light: L('Light theme'),
    dark: L('Dark theme'),
    auto: L('Auto theme (follow system)')
  };
  document.querySelectorAll('[data-theme-switch]').forEach(group => {
    group.setAttribute('aria-label', L('Theme'));
    group.querySelectorAll('[data-theme-mode]').forEach(button => {
      const active = button.dataset.themeMode === preference;
      button.classList.toggle('active', active);
      button.setAttribute('aria-pressed', String(active));
      button.setAttribute('aria-label', labels[button.dataset.themeMode]);
      button.title = labels[button.dataset.themeMode];
    });
  });
}

export function currentTheme() {
  return preference;
}

export function hasStoredTheme() {
  return Boolean(storedTheme());
}

export function setTheme(mode, { persist = true } = {}) {
  preference = normalize(mode);
  if (persist) {
    try { localStorage.setItem(storageKey, preference); } catch (_) {}
  }
  const resolved = resolvedTheme(preference);
  document.documentElement.dataset.theme = resolved;
  document.documentElement.dataset.themePreference = preference;
  document.documentElement.style.colorScheme = resolved;
  updateControls();
}

export function applyInstanceTheme(mode) {
  if (!hasStoredTheme()) setTheme(mode, { persist: false });
}

export function refreshThemeControls() {
  updateControls();
}

export function initThemeControls() {
  document.querySelectorAll('[data-theme-mode]').forEach(button => {
    button.addEventListener('click', () => setTheme(button.dataset.themeMode));
  });
  updateControls();
}

const handleSystemThemeChange = () => {
  if (preference === 'auto') setTheme('auto', { persist: false });
};
if (media.addEventListener) media.addEventListener('change', handleSystemThemeChange);
else media.addListener(handleSystemThemeChange);
