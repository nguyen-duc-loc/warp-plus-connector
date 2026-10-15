import assert from 'node:assert/strict';
import test from 'node:test';
import { overview, reply } from './schema.js';

const at = { utc: '2026-10-14T00:00:00.000000Z', local: '2026-10-14 00:00:00', utc_offset_seconds: '0' };
const available = <T>(data: T) => ({
    availability: 'available',
    observed_at: at,
    age_ns: '0',
    clock_anomaly: false,
    cache_policy: 'replace',
    error_code: null,
    data,
});
const missing = () => ({
    availability: 'missing',
    observed_at: at,
    age_ns: '0',
    clock_anomaly: false,
    cache_policy: 'discard',
    error_code: 'service_not_installed',
    data: null,
});
function savedOverview() {
    return {
        header_at: at,
        collection: available({
            as_of: at,
            max_age_ns: '3600000000000',
            total: '0',
            latest_outcomes: { unchecked: '0', confirmed: '0', invalid: '0', inconclusive: '0' },
            fresh_confirmed: '0',
            stale_confirmed: '0',
            unknown_freshness: '0',
            recent_runs: [],
            older_runs_available: false,
            schedule: {
                checkpoint_at: at,
                clock_regressed: false,
                validation_recovery_pending: false,
                crawl: {
                    coverage: [{ channel: 'warpplus', page_limit: '1' }],
                    due_at: at,
                    reason: 'initial',
                    basis_at: at,
                    interval_ns: '3600000000000',
                    basis_run_id: '9007199254740993',
                    active_run_id: null,
                },
                candidates: { total: '0', active: '0', due: '0', waiting: '0', next_due_at: null },
                validation_wait: {
                    reason: 'backend_unavailable',
                    basis_at: at,
                    retry_at: at,
                    observed_at: at,
                    interval_ns: '3600000000000',
                },
            },
        }),
        service: {
            identity: { state: 'missing', installation_id: null, cache_policy: 'discard' },
            collection_match: 'unknown',
            bound_state_dir: null,
            installation: missing(),
            manager: missing(),
            runner: missing(),
        },
    };
}

// Store migration 005 and crawl completion save these exact reasons, not candidate schedule reasons.
for (const reason of ['initial', 'coverage_changed', 'completed', 'failed', 'cancelled', 'interrupted']) {
    test(`saved crawl schedule reason ${reason} reaches the overview`, () => {
        const value = savedOverview();
        value.collection.data.schedule.crawl.reason = reason;
        assert.equal(overview(value).collection.data?.schedule.crawl?.reason, reason);
    });
}
for (const reason of [
    'backend_unavailable',
    'cleanup_pending',
    'provider_cooldown',
    'validator_busy',
    'validator_clock_regressed',
    'validation_rate_limited',
    'validation_probe_failed',
    'validation_interrupted',
]) {
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
    for (const utc of ['2026-02-30T00:00:00.000000Z', '2025-02-29T00:00:00.000000Z', '2026-10-14T24:00:00.000000Z']) {
        const value = savedOverview();
        value.header_at = { ...at, utc };
        assert.throws(() => overview(value), /ui_protocol_invalid/);
    }
});
test('valid RFC3339 stamps preserve leap days and full microsecond precision', () => {
    for (const utc of ['2024-02-29T23:59:59.123456Z', '2026-10-14T00:00:00.999999Z']) {
        const value = savedOverview();
        value.header_at = { ...at, utc };
        assert.equal(overview(value).header_at.utc, utc);
    }
});

// Spec 0010 AC-12: every nested record rejects added or missing fields before rendering.
test('all overview records reject extra and missing fields', () => {
    const id = 'a'.repeat(32);
    const value = {
        ...savedOverview(),
        service: {
            identity: { state: 'verified', installation_id: id, cache_policy: 'retain' },
            collection_match: 'match',
            bound_state_dir: '/state',
            installation: available({
                installation_id: id,
                phase: 'ready',
                autostart_enabled: true,
                state_dir: '/state',
            }),
            manager: available({
                loaded: true,
                running: true,
                pid: '123',
                last_exit_code: '-1',
                last_signal: '15',
                active_state: 'active',
                pending_job: false,
                autostart_observed: true,
            }),
            runner: available({
                lock_held: true,
                owner_pid: '123',
                installation_matches: true,
                stale: false,
                clock_anomaly: false,
                observation: {
                    phase: 'running',
                    started_at: at,
                    updated_at: at,
                    stopped_at: null,
                    crawl_phase: 'idle',
                    validation_phase: 'waiting',
                    failure_code: null,
                },
            }),
        },
    };
    const run = {
        id: '9007199254740993',
        kind: 'validation',
        origin: 'scheduled',
        status: 'completed',
        started_at: at,
        finished_at: at,
        recovered_at: null,
        reason_code: null,
        clock_anomaly: false,
        crawl_counts: null,
        validation_counts: {
            selected: '1',
            pending: '0',
            active: '0',
            completed: '1',
            skipped: '0',
            interrupted: '0',
            confirmed: '1',
            invalid: '0',
            inconclusive: '0',
            unresolved: '0',
            unstarted: '0',
        },
    };
    const complete = {
        ...value,
        collection: {
            ...value.collection,
            data: {
                ...value.collection.data,
                recent_runs: [
                    run,
                    {
                        ...run,
                        id: '2',
                        kind: 'crawl',
                        validation_counts: null,
                        crawl_counts: { saved_candidates: '0', saved_sightings: '0' },
                    },
                ],
            },
        },
    };
    assert.doesNotThrow(() => overview(complete));
    function visit(node: unknown, path: string) {
        if (node === null || typeof node !== 'object') return;
        if (Array.isArray(node)) {
            node.forEach((item, i) => {
                visit(item, `${path}[${i}]`);
            });
            return;
        }
        const record = node as Record<string, unknown>;
        record.private_key = 'synthetic_private_value';
        assert.throws(() => overview(complete), /ui_protocol_invalid/, `${path} extra field`);
        delete record.private_key;
        for (const [key, original] of Object.entries(record)) {
            delete record[key];
            assert.throws(() => overview(complete), /ui_protocol_invalid/, `${path}.${key} missing`);
            record[key] = original;
        }
        for (const [key, child] of Object.entries(record)) visit(child, `${path}.${key}`);
    }
    // Shared stamps are valid again before traversal advances to another record.
    visit(complete, 'overview');
});

// Spec 0010 AC-4, AC-12: canonical decimal strings preserve values above JavaScript integer precision.
test('collection totals require exact decimal arithmetic', () => {
    const value = savedOverview();
    value.collection.data.total = '9007199254740993';
    value.collection.data.latest_outcomes.unchecked = '9007199254740993';
    assert.equal(overview(value).collection.data?.total, '9007199254740993');
    value.collection.data.latest_outcomes.unchecked = '9007199254740992';
    assert.throws(() => overview(value), /ui_protocol_invalid/);
    for (const total of ['01', '-1', '1e3', '1.0', '', '9'.repeat(129), 1, null]) {
        const bad = savedOverview();
        assert.throws(
            () => overview({ ...bad, collection: { ...bad.collection, data: { ...bad.collection.data, total } } }),
            /ui_protocol_invalid/,
        );
    }
});

// Spec 0010 AC-9, AC-12: contradictory availability or clock metadata cannot appear healthy.
test('component metadata must agree with data and observation age', () => {
    const value = savedOverview();
    for (const change of [
        { data: null },
        { cache_policy: 'retain' },
        { error_code: 'state_error' },
        { age_ns: null },
        { clock_anomaly: true },
        { availability: 'unavailable' },
        { availability: 'missing' },
    ]) {
        assert.throws(
            () => overview({ ...value, collection: { ...value.collection, ...change } }),
            /ui_protocol_invalid/,
        );
    }
    const anomaly = overview({ ...value, collection: { ...value.collection, clock_anomaly: true, age_ns: null } });
    assert.equal(anomaly.collection.clock_anomaly, true);
    const unavailable = {
        ...value.collection,
        availability: 'unavailable',
        cache_policy: 'retain',
        data: null,
        error_code: 'state_error',
    };
    assert.equal(overview({ ...value, collection: unavailable }).collection.data, null);
    assert.throws(
        () => overview({ ...value, collection: { ...unavailable, availability: 'missing' } }),
        /ui_protocol_invalid/,
    );
});

// Spec 0010 AC-12: a reply belongs to one pending request and exposes only its result.
test('reply rejects mismatched identities versions and error details', () => {
    const result = { total: '9007199254740993' };
    assert.deepEqual(reply({ version: 3, id: '9', result }, '9'), result);
    for (const value of [
        { version: 3, id: '8', result },
        { version: 99, id: '9', result },
        { version: 3, id: 9, result },
        { version: 3, id: '9', result, error: { code: 'state_error' } },
        { version: 3, id: '9', error: { code: 'state_error', detail: 'private error' } },
        { version: 3, id: '9', error: { code: 'state_error' } },
        null,
        [],
    ]) {
        assert.throws(() => reply(value, '9'), /ui_protocol_invalid/);
    }
});
