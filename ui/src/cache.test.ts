import test from 'node:test';
import assert from 'node:assert/strict';
import {age, apply, empty, initial, refresh} from './cache.js';
import {hello, type Component, type Overview} from './schema.js';
import {safe} from './theme.js';
import {parseFrame} from './channel.js';

const at = {utc: '2026-10-14T00:00:00.000000Z', local: '2026-10-14 00:00:00', utc_offset_seconds: '0'};
test('retained data keeps the original observation and receive time', () => {
  const good: Component<{total: string}> = {availability: 'available', observed_at: at, age_ns: '1000000000',
    clock_anomaly: false, cache_policy: 'replace', error_code: null, data: {total: '9007199254740993'}};
  const previous = apply(empty<{total: string}>(), good, 10);
  const stale = apply(previous, {...good, availability: 'unavailable', data: null, cache_policy: 'retain',
    error_code: 'state_error', observed_at: {...at, local: '2026-10-15 00:00:00'}}, 20);
  assert.equal(stale.status, 'stale');
  assert.equal(stale.data?.total, '9007199254740993');
  assert.equal(stale.observed, at);
  assert.equal(stale.received, 10);
  assert.equal(apply(stale, {...good, availability: 'missing', data: null, cache_policy: 'discard'}, 30).data, null);
});
test('service identity mismatch discards cached observations even with retain policy', () => {
  const previous = initial();
  previous.identity = 'a'.repeat(32);
  previous.manager = {...empty(), data: {loaded: true, running: true, pid: '5', last_exit_code: null,
    last_signal: null, active_state: 'active', pending_job: false, autostart_observed: true}, status: 'current'};
  const missing = {availability: 'unavailable', observed_at: at, age_ns: '0', clock_anomaly: false,
    cache_policy: 'retain', error_code: 'service_manager_unavailable', data: null};
  const next = {header_at: at, collection: missing, service: {identity: {state: 'verified',
    installation_id: 'b'.repeat(32), cache_policy: 'retain'}, collection_match: 'unknown', bound_state_dir: '/test',
    installation: missing, manager: missing, runner: missing}} as Overview;
  assert.equal(refresh(previous, next).manager.data, null);
});
test('closed handshake shape rejects missing and unknown fields', () => {
  const valid = {release: 'dev', commit: '', protocol: 1, parent_pid: '123', capabilities: ['overview'], color: 'never',
    refresh_delay_ns: '5000000000', timezone: 'UTC', profile: {state_dir: '/test', config_path: null,
      suggestions: [{action: 'sources', command_text: 'wpc sources'}, {action: 'crawl', command_text: 'wpc crawl'}]}};
  assert.equal(hello(valid).parent_pid, '123');
  assert.throws(() => hello({...valid, keys: ['private']}));
  assert.throws(() => hello({...valid, parent_pid: 123}));
  assert.throws(() => hello({...valid, profile: {state_dir: '/test'}}));
});
test('dynamic controls never reach the renderer', () => {
  assert.equal(safe('name\x1b]52;paste\x07\n'), 'name\\u001b]52;paste\\u0007\\u000a');
});
test('duplicate fields cannot override validated reply identities', () => {
  assert.throws(() => parseFrame('{"version":1,"id":"1","id":"2","result":{}}'));
  assert.throws(() => parseFrame('{"result":{"data":0,"data":1}}'));
  assert.deepEqual(parseFrame('{"text":"quoted \\\"key\\\"", "values":[null, true, 1]}'),
    {text: 'quoted "key"', values: [null, true, 1]});
});

// Spec 0010 AC-8, AC-9: missing and failed reads never invent zeroes, and recovery replaces stale facts.
test('cache transitions distinguish loading failure missing and successful recovery', () => {
  const loading = empty<{total: string}>();
  assert.equal(loading.status, 'loading');
  const good: Component<{total: string}> = {availability: 'available', observed_at: at, age_ns: '2000000000',
    clock_anomaly: false, cache_policy: 'replace', error_code: null, data: {total: '7'}};
  const failed = {...good, availability: 'unavailable', cache_policy: 'retain', data: null, error_code: 'state_error'};
  const unavailable = apply(loading, failed, 100);
  assert.equal(unavailable.status, 'unavailable');
  assert.equal(unavailable.data, null);
  const current = apply(unavailable, good, 200);
  const stale = apply(current, failed, 300);
  assert.equal(stale.status, 'stale');
  assert.equal(stale.data?.total, '7');
  const recovered = apply(stale, {...good, data: {total: '9'}, age_ns: '0'}, 400);
  assert.equal(recovered.status, 'current');
  assert.equal(recovered.data?.total, '9');
  assert.equal(recovered.received, 400);
  assert.equal(recovered.error, null);
  const missing = apply(recovered, {...failed, availability: 'missing', cache_policy: 'discard',
    error_code: 'state_not_initialized'}, 500);
  assert.equal(missing.status, 'missing');
  assert.equal(missing.data, null);
  assert.equal(missing.observed, null);
});

// Spec 0010 AC-6, AC-9: observation age uses exact nanoseconds and the original monotonic receive time.
test('age preserves large durations and never disguises clock anomalies', () => {
  const saved = {...empty<{total: string}>(), data: {total: '1'}, age: '9007199254740993000', received: 100};
  assert.equal(age(saved, 1100), 'Observed 9007199255s ago');
  assert.equal(age(saved, 99), 'Observed 9007199254s ago');
  assert.equal(age({...saved, anomaly: true}, 1100), 'Clock anomaly, age unknown');
  assert.equal(age({...saved, age: null}, 1100), 'Clock anomaly, age unknown');
  assert.equal(age(empty(), 1100), '');
});

// Spec 0010 AC-7, AC-9: verified identity permits retention of only the failed service component.
test('refresh keeps stale manager evidence while replacing other service observations', () => {
  const previous = initial();
  const id = 'a'.repeat(32);
  previous.identity = id;
  previous.manager = {...empty(), data: {loaded: true, running: true, pid: '5', last_exit_code: null,
    last_signal: null, active_state: 'active', pending_job: false, autostart_observed: true}, status: 'current',
    observed: at, age: '1000000000', received: 42};
  const missing = {availability: 'unavailable', observed_at: at, age_ns: '0', clock_anomaly: false,
    cache_policy: 'retain', error_code: 'service_manager_unavailable', data: null};
  const installation = {...missing, availability: 'available', cache_policy: 'replace', error_code: null,
    data: {installation_id: id, phase: 'ready', autostart_enabled: true, state_dir: '/service'}};
  const next = {header_at: at, collection: missing, service: {identity: {state: 'verified',
    installation_id: id, cache_policy: 'retain'}, collection_match: 'different', bound_state_dir: '/service',
    installation, manager: missing, runner: missing}} as Overview;
  const result = refresh(previous, next);
  assert.equal(result.manager.status, 'stale');
  assert.equal(result.manager.data?.pid, '5');
  assert.equal(result.manager.received, 42);
  assert.equal(result.manager.observed, at);
  assert.equal(result.installation.status, 'current');
  assert.deepEqual(result.installation.data, installation.data);
  assert.equal(result.service?.collection_match, 'different');
  assert.equal(result.collection.status, 'unavailable');
  const discarded = refresh(result, {...next, service: {...next.service,
    identity: {...next.service.identity, cache_policy: 'discard'}}});
  assert.equal(discarded.manager.data, null);
  assert.equal(discarded.manager.status, 'unavailable');
});
