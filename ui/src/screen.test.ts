import assert from 'node:assert/strict';
import test from 'node:test';
import { type Cache, initial } from './cache.js';
import type { Collection, Hello } from './schema.js';
import { body } from './screen.js';

const at = { utc: '2026-10-14T00:00:00.000000Z', local: '2026-10-14 05:45:00', utc_offset_seconds: '20700' };
const welcome: Hello = {
    release: 'dev',
    commit: '',
    protocol: 2,
    parent_pid: '123',
    capabilities: ['overview', 'results', 'details', 'reveal', 'export'],
    color: 'never',
    refresh_delay_ns: '5000000000',
    timezone: 'NPT',
    profile: {
        state_dir: '/selected',
        config_path: null,
        suggestions: [
            { action: 'sources', command_text: "wpc sources --state-dir '/selected'" },
            { action: 'crawl', command_text: "wpc crawl --state-dir '/selected'" },
        ],
    },
};
function collection(): Collection {
    return {
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
            checkpoint_at: null,
            clock_regressed: false,
            validation_recovery_pending: false,
            crawl: null,
            candidates: { total: '0', active: '0', due: '0', waiting: '0', next_due_at: null },
            validation_wait: null,
        },
    };
}
function loaded(): Cache {
    const cache = initial();
    cache.collection = {
        ...cache.collection,
        status: 'current',
        data: collection(),
        observed: at,
        age: '0',
        received: 0,
    };
    return cache;
}

// Spec 0010 AC-9, AC-14: failure and absence must not be presented as an initialized empty collection.
test('screen distinguishes loading absent unavailable and initialized empty collections', () => {
    const cache = initial();
    assert.deepEqual(body(cache, welcome, false, 0)[0], ['Loading']);
    cache.collection.status = 'missing';
    cache.collection.error = 'state_not_initialized';
    const missing = body(cache, welcome, false, 0)[0]!.join('\n');
    assert.match(missing, /No saved collection yet/);
    assert.match(missing, /does not create state or start work/);
    for (const suggestion of welcome.profile.suggestions) assert.ok(missing.includes(suggestion.command_text));
    assert.doesNotMatch(missing, /0 TOTAL CANDIDATES|No candidates saved/);
    cache.collection.status = 'unavailable';
    cache.collection.error = 'state_schema_too_new';
    const unavailable = body(cache, welcome, false, 0)[0]!.join('\n');
    assert.match(unavailable, /newer matching CLI/);
    assert.doesNotMatch(unavailable, /0 TOTAL CANDIDATES|wpc crawl/);
    const empty = body(loaded(), welcome, false, 0)[0]!.join('\n');
    assert.match(empty, /0 TOTAL CANDIDATES/);
    assert.match(empty, /No candidates saved/);
});

// Spec 0010 AC-4, AC-5, AC-11: large values and missing legacy facts stay truthful in both layouts.
for (const wide of [false, true]) {
    test(`screen preserves complete values and legacy history in ${wide ? 'wide' : 'compact'} layout`, () => {
        const cache = loaded();
        const saved = cache.collection.data!;
        saved.total = '9007199254740993';
        saved.fresh_confirmed = '9007199254740992';
        saved.older_runs_available = true;
        saved.recent_runs = [
            {
                id: '9007199254740997',
                kind: 'validation',
                origin: 'unknown',
                status: 'running',
                started_at: at,
                finished_at: null,
                recovered_at: null,
                reason_code: null,
                clock_anomaly: false,
                crawl_counts: null,
                validation_counts: null,
            },
        ];
        const sections = body(cache, welcome, wide, 0);
        assert.match(sections[0]!.join('\n'), /9007199254740992 FRESH CONFIRMED.*9007199254740993 TOTAL CANDIDATES/);
        const history = sections[1]!.join('\n');
        assert.match(history, /9007199254740997/);
        assert.match(history, /unknown.*Saved as running/);
        assert.match(history, /Legacy selection counts unavailable/);
        assert.match(history, /Showing latest 10 runs; more saved history exists/);
        assert.match(history, /2026-10-14 05:45:00 UTC\+05:45/);
        assert.doesNotMatch(history, /Finished|Recovered/);
        assert.equal(sections.flat().join('\n').includes('\x1b'), false);
    });
}

// Spec 0010 AC-6, AC-9: retained collection facts and clock anomalies remain visibly qualified.
test('stale collection labels cover readiness history and schedules', () => {
    const cache = loaded();
    cache.collection.status = 'stale';
    cache.collection.error = 'state_error';
    cache.collection.anomaly = true;
    cache.collection.age = null;
    cache.collection.data!.schedule.clock_regressed = true;
    cache.collection.data!.schedule.validation_recovery_pending = true;
    const sections = body(cache, welcome, false, 1000);
    assert.match(sections[0]!.join('\n'), /Stale.*Clock anomaly, age unknown/);
    assert.match(sections[0]!.join('\n'), /Unavailable \(state_error\)/);
    assert.ok(sections[1]!.includes('Stale collection snapshot'));
    assert.ok(sections[2]!.includes('Stale collection snapshot'));
    assert.match(sections[2]!.join('\n'), /Clock pause/);
    assert.match(sections[2]!.join('\n'), /Validation recovery pending/);
    assert.match(sections[2]!.join('\n'), /not proof of a live runner/);
});

// Spec 0010 AC-7, AC-9: service evidence remains useful when collection data is unavailable.
test('service shows selected and bound collections without inventing runner ownership', () => {
    const cache = initial();
    const unavailable = {
        availability: 'unavailable' as const,
        observed_at: at,
        age_ns: '0',
        clock_anomaly: false,
        cache_policy: 'discard' as const,
        error_code: 'service_manager_unavailable',
        data: null,
    };
    cache.service = {
        identity: { state: 'verified', installation_id: 'a'.repeat(32), cache_policy: 'retain' },
        collection_match: 'different',
        bound_state_dir: '/service',
        installation: unavailable,
        manager: unavailable,
        runner: unavailable,
    };
    cache.manager = {
        ...cache.manager,
        status: 'current',
        observed: at,
        age: '0',
        received: 0,
        data: {
            loaded: true,
            running: null,
            pid: null,
            last_exit_code: null,
            last_signal: null,
            active_state: null,
            pending_job: null,
            autostart_observed: null,
        },
    };
    const service = body(cache, welcome, false, 0)[3]!.join('\n');
    assert.match(service, /Service uses a different collection\nSelected \/selected\nService \/service/);
    assert.match(service, /Loaded Yes \| Running Unknown \| PID Unknown/);
    assert.match(service, /Last exit Unknown \| Last signal Unknown/);
    assert.doesNotMatch(service, /Running Yes|PID 0|Last exit 0/);
});
