import assert from 'node:assert/strict';
import test from 'node:test';
import { acknowledged, job, options, policy, reportPage, review, workState } from './work-schema.js';

const at = { utc: '2026-10-15T00:00:00.000000Z', local: '2026-10-15 00:00:00', utc_offset_seconds: '0' };
const configured = {
    version: '1',
    workers: '2',
    max_attempts: '3',
    mutation_interval_ns: '1000000000',
    candidate_timeout_ns: '900000000000',
    attempt_timeout_ns: '240000000000',
    command_timeout_ns: '60000000000',
    poll_timeout_ns: '10000000000',
    poll_interval_ns: '250000000',
    cleanup_timeout_ns: '60000000000',
    shutdown_timeout_ns: '60000000000',
    finalize_timeout_ns: '5000000000',
    retry_base_ns: '5000000000',
    retry_cap_ns: '60000000000',
    memory_mib: '512',
    cpus: '1',
    processes: '256',
    protocol_version: '2',
};
const initial = {
    operation_id: '9007199254740993',
    revision: '1',
    kind: 'crawl',
    state: 'running',
    phase: 'admission',
    observed_at: at,
    elapsed_ns: '1',
    stop_elapsed_ns: null,
    run_id: null,
    run_created: 'false',
    run_status: null,
    cancellation_requested: false,
    persistence: 'known',
    cleanup: 'not_needed',
    error_code: null,
    crawl: null,
    check: null,
    guidance: { action: 'none', retry_at: null, service_observed_at: null },
    unresolved_cleanup: null,
};
// Spec 0012 AC-4 and AC-14: configuration readiness is not provider proof.
test('work configuration preserves the exact policy and rejects invented readiness', () => {
    assert.deepEqual(policy(configured), configured);
    for (const changed of [
        { workers: '5' },
        { max_attempts: '0' },
        { cleanup_timeout_ns: '61000000000' },
        { protocol_version: '3' },
    ])
        assert.throws(() => policy({ ...configured, ...changed }));
    const o = {
        sources: [{ channel: 'fixture', enabled: false, page_limit: '100' }],
        validator: 'unsupported',
        image: null,
        check_policy: configured,
        check_available: false,
        check_error_code: 'validation_unsupported',
    };
    assert.equal(options(o).sources[0]?.enabled, false);
    assert.throws(() => options({ ...o, check_available: true }));
    assert.throws(() => options({ ...o, sources: [...o.sources, ...o.sources] }));
});
// Spec 0012 AC-6 and AC-15: unknown creation stays unknown, without a guessed ID.
test('work observations reject inconsistent creation and untyped private fields', () => {
    assert.equal(workState(initial).operation_id, '9007199254740993');
    assert.equal(workState({ ...initial, run_created: 'unknown' }).run_id, null);
    for (const changed of [
        { run_id: '1' },
        { run_created: true },
        { phase: 'complete' },
        { state: 'failed', phase: 'complete' },
        { key: 'private' },
        { session_id: 'private' },
    ])
        assert.throws(() => workState({ ...initial, ...changed }));
});
// Spec 0012 AC-11 through AC-14: cancelled pages are saved facts, not invalid keys.
test('report pages retain cancelled coverage and reject gaps or disclosure', () => {
    const row = {
        source_id: 'telegram:fixture',
        page_number: '1',
        requested_url: { text: 'https://t.me/s/fixture', shortened: false },
        status: 'cancelled',
        requested_at: at,
        before_id: null,
        fetched_at: null,
        finished_at: at,
        recovered_at: null,
        parsed_post_count: null,
        min_message_id: null,
        max_message_id: null,
        next_before_id: null,
        reason_code: 'cancelled',
        saved_candidates: '0',
        saved_sightings: '0',
    };
    const page = {
        report_id: '1',
        section: 'pages',
        source_id: 'telegram:fixture',
        candidate_id: null,
        offset: '0',
        total: '1',
        items: [row],
        next_offset: null,
    };
    assert.equal(reportPage(page).items.length, 1);
    assert.throws(() => reportPage({ ...page, total: '2' }));
    assert.throws(() => reportPage({ ...page, items: [{ ...row, key: 'private' }] }));
});
// Spec 0012 AC-5: a completed preparation cannot invent an absent review.
test('work preparation requires a matching committed result', () => {
    const pending = {
        job_id: '1',
        kind: 'work_review',
        state: 'running',
        phase: 'selection',
        completed_count: '0',
        expected_count: null,
        error_code: null,
        warning_code: null,
        remaining_path: null,
        result: null,
    };
    assert.equal(job(pending).result, null);
    assert.throws(() => job({ ...pending, state: 'succeeded', phase: 'complete' }));
    assert.throws(() => job({ ...pending, error_code: 'state_error' }));
});

// Only explicit acknowledgement releases the local operation handle.
test('work acknowledgement cannot silently decline release', () => {
    assert.equal(acknowledged({ acknowledged: true, unresolved_cleanup: null }).acknowledged, true);
    assert.throws(() => acknowledged({ acknowledged: false, unresolved_cleanup: null }));
});

// Spec 0012 AC-2, AC-3 and AC-14: review records cannot mix crawl and check
// identities, misstate a page budget, or round an exact candidate ID.
test('review validation binds the displayed budget and focused identity', () => {
    const crawl = {
        review_id: '1',
        kind: 'crawl',
        created_at: at,
        collection_state: 'absent',
        sources: [
            { channel: 'first', page_limit: '1' },
            { channel: 'last', page_limit: '100' },
        ],
        page_budget: '101',
        selection: null,
        candidate_id: null,
        selected_count: null,
        view_id: null,
        as_of: null,
        filters: null,
        policy: null,
        image: null,
    };
    assert.equal(review(crawl).page_budget, '101');
    for (const changed of [
        { page_budget: '100' },
        { sources: [] },
        { selected_count: '1' },
        { sources: [{ channel: 'first', page_limit: '101' }] },
        { key: 'private' },
    ])
        assert.throws(() => review({ ...crawl, ...changed }));
    const check = {
        ...crawl,
        kind: 'check',
        collection_state: 'existing',
        sources: null,
        page_budget: null,
        selection: 'focused',
        candidate_id: '9007199254740993',
        selected_count: '1',
        view_id: '2',
        as_of: at,
        filters: { status: [], definitive: [], freshness: [] },
        policy: configured,
        image: { text: 'sha256:fixture', shortened: false },
    };
    assert.equal(review(check).candidate_id, '9007199254740993');
    for (const changed of [
        { candidate_id: null },
        { candidate_id: 9007199254740992 },
        { selected_count: '2' },
        { selection: 'matching' },
        { policy: null },
        { page_budget: '1' },
    ])
        assert.throws(() => review({ ...check, ...changed }));
});

// Spec 0012 AC-4, AC-6 and AC-14: concurrent activity respects the saved policy,
// while retry waits and exact identities remain independent observations.
test('worker observations reject duplicates and workers beyond saved capacity', () => {
    const check = {
        ...initial,
        kind: 'check',
        phase: 'check',
        run_created: 'true',
        run_id: '9007199254740993',
        run_status: 'running',
        cleanup: 'pending',
        check: {
            totals: {
                selected: '3',
                pending: '1',
                active: '2',
                completed: '0',
                skipped: '0',
                interrupted: '0',
                confirmed: '0',
                invalid: '0',
                inconclusive: '1',
                unresolved: '2',
                unstarted: '0',
            },
            attempts_started: '3',
            attempts_completed: '1',
            retry_waiting: '1',
            next_retry_wait_ns: '1000000000',
            policy: configured,
            workers: [
                { candidate_id: '9007199254740993', attempt_number: '1', phase: 'attaching' },
                { candidate_id: '9007199254740994', attempt_number: '2', phase: 'cleanup' },
            ],
        },
    };
    const parsed = workState(check);
    assert.deepEqual(parsed.check?.workers, check.check.workers);
    assert.equal(parsed.check?.retry_waiting, '1');
    for (const changed of [
        { workers: [check.check.workers[0], check.check.workers[0]] },
        { policy: { ...configured, workers: '1' } },
        { workers: [{ ...check.check.workers[0], phase: 'provider-secret' }] },
        { workers: [{ ...check.check.workers[0], session_id: 'private' }] },
    ])
        assert.throws(() => workState({ ...check, check: { ...check.check, ...changed } }));
});

// Spec 0012 AC-11 and AC-14: complete page traversal remains bounded and exact.
test('report item pages retain the next offset and reject oversized pages', () => {
    const items = Array.from({ length: 50 }, (_, n) => ({
        candidate_id: (9007199254740993n + BigInt(n)).toString(),
        ordinal: (100 + n).toString(),
        status: 'pending',
        stop_reason: null,
        started_at: null,
        finished_at: null,
        recovered_at: null,
        attempt_count: '0',
        latest_outcome: null,
    }));
    const page = {
        report_id: '1',
        section: 'items',
        source_id: null,
        candidate_id: null,
        offset: '100',
        total: '151',
        items,
        next_offset: '150',
    };
    assert.equal(reportPage(page).next_offset, '150');
    assert.deepEqual(reportPage(page).items, items);
    assert.throws(() => reportPage({ ...page, next_offset: null }));
    assert.throws(() => reportPage({ ...page, next_offset: '149' }));
    assert.throws(() => reportPage({ ...page, items: [...items, items[0]], next_offset: null }));
});
