// Bilingual locale registry and translation helpers.
import en from '../locales/en.js';
import zh from '../locales/zh.js';

const locales = {en, zh};

function readStoredLanguage() {
  try {
    return localStorage.getItem('mirrorrelay.language');
  } catch (_) {
    return '';
  }
}

function storeLanguage(value) {
  try {
    localStorage.setItem('mirrorrelay.language', value);
  } catch (_) {
    // Language switching still works when storage is unavailable.
  }
}

const storedLanguage = readStoredLanguage();
let language = storedLanguage === 'zh' || storedLanguage === 'en'
  ? storedLanguage
  : ((navigator.languages || [navigator.language]).some(value => /^zh(?:-|$)/i.test(value || '')) ? 'zh' : 'en');

let languageChangeHandler = () => {};

export function currentLanguage() {
  return language;
}

export function getLocale(lang = language) {
  return locales[lang] || locales.en || {};
}

export function L(english, ...args) {
  const loc = getLocale();
  let str;
  if (loc.strings && english in loc.strings) {
    str = loc.strings[english];
  } else {
    const enLoc = getLocale('en');
    if (enLoc && enLoc.strings && english in enLoc.strings) {
      str = enLoc.strings[english];
    } else {
      str = english;
    }
  }
  for (const arg of args) {
    str = str.replace('%s', arg);
  }
  return str;
}

// onLanguageChange registers the callback that refreshes page content after
// the static labels have been retranslated. Registered by the entry point so
// this module does not depend on the router.
export function onLanguageChange(handler) {
  languageChangeHandler = handler;
}

export function applyLanguage(next, persist = false) {
  language = next === 'zh' ? 'zh' : 'en';
  if (persist) storeLanguage(language);
  document.documentElement.lang = language === 'zh' ? 'zh-CN' : 'en';
  const loc = getLocale();
  const dict = loc.dictionary || {};
  document.querySelectorAll('[data-i18n]').forEach(element => {
    const value = dict[element.dataset.i18n];
    if (value) element.textContent = value;
  });
  document.querySelectorAll('.language-switch button').forEach(button => button.classList.toggle('active', button.dataset.lang === language));
  languageChangeHandler();
}
