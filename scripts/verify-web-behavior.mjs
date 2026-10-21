// Behavioural checks for the dependency-free Web UI helpers. These run in plain
// Node.js without a DOM, so only pure modules are exercised.
import assert from 'node:assert/strict';

if (!globalThis.navigator) {
  Object.defineProperty(globalThis, 'navigator', {value: {languages: ['en'], language: 'en'}});
}

const {L} = await import('../internal/web/dist/js/i18n.js');
const {parseUpstreams, parseHeaders, setNestedValue} = await import('../internal/web/dist/js/forms.js');
const {healthClass, healthBadge} = await import('../internal/web/dist/js/ui.js');
const {date} = await import('../internal/web/dist/js/format.js');

// Replacement patterns in arguments must be inserted literally.
assert.equal(L('Invalid header line: %s', 'a$&b'), 'Invalid header line: a$&b');

// Editing upstreams keeps the weight and enabled flag of unchanged URLs.
const existing = [{url: 'https://a.example/', priority: 10, weight: 7, enabled: false}];
assert.deepEqual(parseUpstreams('5 https://a.example/\n20 https://b.example/', existing), [
  {url: 'https://a.example/', priority: 5, weight: 7, enabled: false},
  {url: 'https://b.example/', priority: 20, weight: 1, enabled: true}
]);
assert.throws(() => parseUpstreams('1 https://a.example/\n2 https://a.example/'), /Duplicate upstream URL/);
assert.throws(() => parseUpstreams('https://a.example/'), /Invalid upstream line/);
assert.deepEqual(parseHeaders('X-A: 1\nX-B: two: parts'), {'X-A': '1', 'X-B': 'two: parts'});

// Nested settings writes create missing intermediate objects.
const target = {a: null};
setNestedValue(target, 'a.b.c', 3);
assert.deepEqual(target, {a: {b: {c: 3}}});

// Health colours: unknown is a warning, not a failure.
assert.equal(healthClass('healthy'), 'ok');
assert.equal(healthClass('unknown'), 'yellow');
assert.equal(healthClass(undefined), 'yellow');
assert.equal(healthClass('unhealthy'), 'bad');
assert.ok(!healthBadge('disabled').includes('pulse-dot'));
assert.ok(healthBadge('<x>').includes('&lt;x&gt;'));

// Zero Go timestamps render as a dash instead of year 1.
assert.equal(date('0001-01-01T00:00:00Z'), '—');
assert.equal(date('not a date'), '—');
assert.equal(date(''), '—');

console.log('Web UI behaviour checks passed.');
