import test from 'node:test';
import assert from 'node:assert/strict';
import {apply, empty, initial, refresh} from './cache.js';
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
