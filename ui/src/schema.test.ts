import test from 'node:test';
import assert from 'node:assert/strict';
import {overview} from './schema.js';

const at = {utc: '2026-10-14T00:00:00.000000Z', local: '2026-10-14 00:00:00', utc_offset_seconds: '0'};
const available = <T>(data: T) => ({availability: 'available', observed_at: at, age_ns: '0',
  clock_anomaly: false, cache_policy: 'replace', error_code: null, data});
const missing = () => ({availability: 'missing', observed_at: at, age_ns: '0', clock_anomaly: false,
  cache_policy: 'discard', error_code: 'service_not_installed', data: null});
function savedOverview() {
  return {header_at: at, collection: available({as_of: at, max_age_ns: '3600000000000', total: '0',
    latest_outcomes: {unchecked: '0', confirmed: '0', invalid: '0', inconclusive: '0'},
    fresh_confirmed: '0', stale_confirmed: '0', unknown_freshness: '0', recent_runs: [], older_runs_available: false,
    schedule: {checkpoint_at: at, clock_regressed: false, validation_recovery_pending: false,
      crawl: {coverage: [{channel: 'warpplus', page_limit: '1'}], due_at: at, reason: 'initial', basis_at: at,
        interval_ns: '3600000000000', basis_run_id: '9007199254740993', active_run_id: null},
      candidates: {total: '0', active: '0', due: '0', waiting: '0', next_due_at: null},
      validation_wait: {reason: 'backend_unavailable', basis_at: at, retry_at: at, observed_at: at,
        interval_ns: '3600000000000'}}}),
    service: {identity: {state: 'missing', installation_id: null, cache_policy: 'discard'},
      collection_match: 'unknown', bound_state_dir: null, installation: missing(), manager: missing(), runner: missing()}};
}

// Store migration 005 and crawl completion save these exact reasons, not candidate schedule reasons.
for (const reason of ['initial', 'coverage_changed', 'completed', 'failed', 'cancelled', 'interrupted']) {
  test(`saved crawl schedule reason ${reason} reaches the overview`, () => {
    const value = savedOverview();
    value.collection.data.schedule.crawl.reason = reason;
    assert.equal(overview(value).collection.data?.schedule.crawl?.reason, reason);
  });
}
for (const reason of ['backend_unavailable', 'cleanup_pending', 'provider_cooldown', 'validator_busy',
  'validator_clock_regressed', 'validation_rate_limited', 'validation_probe_failed', 'validation_interrupted']) {
  test(`saved validation wait reason ${reason} reaches the overview`, () => {
    const value = savedOverview();
    value.collection.data.schedule.validation_wait.reason = reason;
    assert.equal(overview(value).collection.data?.schedule.validation_wait?.reason, reason);
  });
}
test('schedule fields reject reasons from unrelated catalogues', () => {
  for (const reason of ['routine', 'failure', 'first_check', 'output_error']) {
    const value = savedOverview();
    value.collection.data.schedule.crawl.reason = reason;
    assert.throws(() => overview(value), /ui_protocol_invalid/);
  }
  const value = savedOverview();
  value.collection.data.schedule.validation_wait.reason = 'output_error';
  assert.throws(() => overview(value), /ui_protocol_invalid/);
});
test('invalid RFC3339 calendar days cannot become healthy observation stamps', () => {
  for (const utc of ['2026-02-30T00:00:00.000000Z', '2025-02-29T00:00:00.000000Z',
    '2026-10-14T24:00:00.000000Z']) {
    const value = savedOverview();
    value.header_at = {...at, utc};
    assert.throws(() => overview(value), /ui_protocol_invalid/);
  }
});
test('valid RFC3339 stamps preserve leap days and full microsecond precision', () => {
  for (const utc of ['2024-02-29T23:59:59.123456Z', '2026-10-14T00:00:00.999999Z']) {
    const value = savedOverview();
    value.header_at = {...at, utc};
    assert.equal(overview(value).header_at.utc, utc);
  }
});
