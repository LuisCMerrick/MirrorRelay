// Form field parsing and nested settings access helpers.
import { L } from './i18n.js';

export function parseList(value) { return value.split(/[\n,]+/).map(item => item.trim()).filter(Boolean); }

export function parseHeaders(value) {
  const result = {};
  for (const line of value.split(/\n+/).map(item => item.trim()).filter(Boolean)) {
    const index = line.indexOf(':');
    if (index <= 0) throw new Error(L('Invalid header line: %s', line));
    result[line.slice(0, index).trim()] = line.slice(index + 1).trim();
  }
  return result;
}

export function parseUpstreams(value) {
  return value.split(/\n+/).filter(line => line.trim()).map(line => {
    const match = line.trim().match(/^(\d+)\s+(https?:\/\/\S+)$/);
    if (!match) throw new Error(L('Invalid upstream line: %s', line));
    return {url: match[2], priority: Number(match[1]), weight: 1, enabled: true};
  });
}

export function nestedValue(object, path) {
  return path.split('.').reduce((value, part) => value?.[part], object);
}

export function setNestedValue(object, path, value) {
  const parts = path.split('.');
  const final = parts.pop();
  const parent = parts.reduce((value, part) => value[part], object);
  parent[final] = value;
}
