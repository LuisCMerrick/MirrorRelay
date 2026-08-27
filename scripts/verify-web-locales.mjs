import fs from 'node:fs';

import { settingsGroups } from '../internal/web/dist/js/settings-schema.js';
import en from '../internal/web/dist/locales/en.js';
import zh from '../internal/web/dist/locales/zh.js';

function fail(message) {
  throw new Error(message);
}

function sameKeys(left, right, label) {
  const leftKeys = Object.keys(left || {}).sort();
  const rightKeys = Object.keys(right || {}).sort();
  if (JSON.stringify(leftKeys) !== JSON.stringify(rightKeys)) {
    fail(`${label} key mismatch`);
  }
}

if (en.lang !== 'en' || zh.lang !== 'zh') fail('locale language identifiers are invalid');
if (fs.readFileSync('internal/web/dist/locales/zh.js', 'utf8').includes('受管上游 Nginx')) {
  fail('Chinese Web UI contains an obsolete translation of the Managed Upstream Nginx product name');
}
sameKeys(en.dictionary, zh.dictionary, 'dictionary');
sameKeys(en.pageMeta, zh.pageMeta, 'page metadata');
sameKeys(en.stateLabels, zh.stateLabels, 'state labels');
sameKeys(en.strings, zh.strings, 'runtime translations');

const indexHTML = fs.readFileSync('internal/web/dist/index.html', 'utf8');
for (const match of indexHTML.matchAll(/data-i18n="([^"]+)"/g)) {
  const key = match[1];
  if (!(key in en.dictionary) || !(key in zh.dictionary)) fail(`missing static translation: ${key}`);
}

const sourceFiles = [];
function collectJavaScript(directory) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const path = `${directory}/${entry.name}`;
    if (entry.isDirectory()) collectJavaScript(path);
    else if (entry.isFile() && path.endsWith('.js')) sourceFiles.push(path);
  }
}
collectJavaScript('internal/web/dist/js');
for (const path of sourceFiles) {
  const source = fs.readFileSync(path, 'utf8');
  if (source.includes('style="')) fail(`inline style conflicts with the administration CSP in ${path}`);
  if (/setInterval\s*\(\s*async\b/.test(source)) fail(`overlapping asynchronous interval polling in ${path}`);
  for (const match of source.matchAll(/\bL\(\s*(['"])(.*?)\1/g)) {
    const key = match[2];
    if (!(key in en.strings) || !(key in zh.strings)) fail(`missing runtime translation ${JSON.stringify(key)} used by ${path}`);
  }
}

if (indexHTML.includes('style="')) fail('inline style conflicts with the administration CSP in index.html');

if (!globalThis.navigator) {
  Object.defineProperty(globalThis, 'navigator', {value: {languages: ['en'], language: 'en'}});
}
const {renderDonutChart} = await import('../internal/web/dist/js/charts.js');
const renderedDonut = renderDonutChart({title: 'test', slices: [{label: 'ok', value: 1, color: '#10b981'}]});
if (!renderedDonut.includes('viewBox="0 0 140 140"')) fail('donut chart has an invalid SVG viewBox');
if (renderedDonut.includes('style="')) fail('rendered donut chart contains an inline style');

const paths = new Set();
for (const group of settingsGroups) {
  if (!group.title || !(group.title in en.strings) || !(group.title in zh.strings)) fail(`missing settings group translation: ${group.title}`);
  for (const field of group.fields || []) {
    if (!field.path || paths.has(field.path)) fail(`invalid or duplicate settings path: ${field.path}`);
    paths.add(field.path);
    if (!(field.label in en.strings) || !(field.label in zh.strings)) fail(`missing settings label translation: ${field.label}`);
    for (const option of field.options || []) {
      if (!(option[1] in en.strings) || !(option[1] in zh.strings)) fail(`missing settings option translation: ${option[1]}`);
    }
  }
}

for (const required of [
  'database.path',
  'distributed.mutation_token_key_files',
  'admin.passkey.enabled',
  'admin.passkey.rp_name',
  'admin.passkey.rp_id',
  'admin.passkey.origins'
]) {
  if (!paths.has(required)) fail(`settings schema omits required path: ${required}`);
}
if (paths.size < 100) fail(`settings schema unexpectedly contains only ${paths.size} fields`);
