// Locale-aware formatting helpers.
import { currentLanguage, getLocale, L } from './i18n.js';

export function locale() { return getLocale().locale || (currentLanguage() === 'zh' ? 'zh-CN' : 'en-US'); }
export function number(value = 0) { return new Intl.NumberFormat(locale()).format(value); }
export function date(value) { return value ? new Date(value).toLocaleString(locale()) : '—'; }
export function bytes(value = 0) {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let index = 0, amount = Number(value) || 0;
  while (amount >= 1024 && index < units.length - 1) { amount /= 1024; index++; }
  return `${amount.toFixed(index ? 2 : 0)} ${units[index]}`;
}
export function duration(seconds = 0) {
  const days = Math.floor(seconds / 86400), hours = Math.floor(seconds % 86400 / 3600), minutes = Math.floor(seconds % 3600 / 60);
  const loc = getLocale();
  if (typeof loc.duration === 'function') return loc.duration(days, hours, minutes);
  return `${days}d ${hours}h ${minutes}m`;
}
export function stateLabel(value) {
  const loc = getLocale();
  const key = String(value || '').toLowerCase();
  if (loc.stateLabels && key in loc.stateLabels) return loc.stateLabels[key];
  return String(value || '—');
}
export function exitSummary(status = {}) {
  if (!status.last_exit_at) return '—';
  const loc = getLocale();
  if (typeof loc.exitSummary === 'function') {
    return loc.exitSummary(date(status.last_exit_at), status.last_exit_code, status.last_exit_reason);
  }
  const code = status.last_exit_code === -1
    ? L('exit code unknown')
    : `exit code ${status.last_exit_code ?? '—'}`;
  return `${date(status.last_exit_at)} · ${code} · ${status.last_exit_reason || ''}`;
}
