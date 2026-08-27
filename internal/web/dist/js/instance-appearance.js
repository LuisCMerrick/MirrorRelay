// Apply validated instance branding without using HTML injection. Asset paths
// are same-origin by backend policy so the login page does not contact an
// administrator-supplied third party before authentication.
import { applyInstanceTheme } from './theme.js';

function parseHexColor(value) {
  const raw = String(value || '').trim().replace(/^#/, '');
  const expanded = raw.length === 3 ? raw.split('').map(part => part + part).join('') : raw.slice(0, 6);
  if (!/^[0-9a-fA-F]{6}$/.test(expanded)) return null;
  return {
    hex: `#${expanded.toLowerCase()}`,
    red: parseInt(expanded.slice(0, 2), 16),
    green: parseInt(expanded.slice(2, 4), 16),
    blue: parseInt(expanded.slice(4, 6), 16)
  };
}

function channelToLinear(channel) {
  const value = channel / 255;
  return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
}

function darken(channel) {
  return Math.max(0, Math.round(channel * 0.78));
}

function toHex(channel) {
  return channel.toString(16).padStart(2, '0');
}

function applyAccent(value) {
  const color = parseHexColor(value) || parseHexColor('#2563eb');
  const hover = `#${toHex(darken(color.red))}${toHex(darken(color.green))}${toHex(darken(color.blue))}`;
  const luminance = 0.2126 * channelToLinear(color.red) + 0.7152 * channelToLinear(color.green) + 0.0722 * channelToLinear(color.blue);
  const root = document.documentElement.style;
  root.setProperty('--primary', color.hex);
  root.setProperty('--primary-hover', hover);
  root.setProperty('--primary-gradient-end', hover);
  root.setProperty('--primary-button-text', (luminance + 0.05) / 0.05 >= 1.05 / (luminance + 0.05) ? '#000000' : '#ffffff');
  root.setProperty('--primary-glow', `rgba(${color.red}, ${color.green}, ${color.blue}, 0.22)`);
  root.setProperty('--primary-surface', `rgba(${color.red}, ${color.green}, ${color.blue}, 0.12)`);
  root.setProperty('--border-primary', `rgba(${color.red}, ${color.green}, ${color.blue}, 0.42)`);
  root.setProperty('--link-hover', hover);
}

function applyLogo(path) {
  document.querySelectorAll('[data-brand-mark]').forEach(mark => {
    const image = mark.querySelector('img');
    const fallback = mark.querySelector('svg');
    if (!image || !fallback) return;
    const showFallback = () => {
      image.hidden = true;
      fallback.hidden = false;
    };
    if (!path) {
      image.removeAttribute('src');
      showFallback();
      return;
    }
    image.onload = () => {
      image.hidden = false;
      fallback.hidden = true;
    };
    image.onerror = showFallback;
    image.src = path;
  });
}

function applyFavicon(path) {
  const favicon = document.getElementById('instance-favicon');
  if (!favicon) return;
  if (path) favicon.href = path;
  else favicon.removeAttribute('href');
}

export function applyInstanceAppearance(appearance = {}) {
  const instanceTitle = appearance.branding?.title || 'MirrorRelay';
  const loginTitle = appearance.login?.title || instanceTitle;
  const loginSubtitle = appearance.login?.subtitle || 'Repository Proxy Service';

  applyInstanceTheme(appearance.theme);
  applyAccent(appearance.accent_color);
  applyLogo(appearance.branding?.logo || '');
  applyFavicon(appearance.branding?.favicon || '');

  document.querySelectorAll('[data-instance-title]').forEach(element => { element.textContent = instanceTitle; });
  document.querySelectorAll('[data-login-title]').forEach(element => { element.textContent = loginTitle; });
  document.querySelectorAll('[data-login-subtitle]').forEach(element => { element.textContent = loginSubtitle; });
  document.title = `${instanceTitle} Control Plane`;
}
