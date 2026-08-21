// Apply the browser theme before stylesheets load to avoid a light/dark flash.
(function () {
  const key = 'mirrorrelay.theme';
  const allowed = ['auto', 'light', 'dark'];
  let preference = 'auto';

  try {
    const stored = localStorage.getItem(key);
    if (allowed.includes(stored)) preference = stored;
  } catch (_) {
    // Storage can be unavailable in hardened/private browser contexts.
  }

  const prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
  const resolved = preference === 'auto' ? (prefersDark ? 'dark' : 'light') : preference;
  document.documentElement.dataset.theme = resolved;
  document.documentElement.dataset.themePreference = preference;
  document.documentElement.style.colorScheme = resolved;
}());
