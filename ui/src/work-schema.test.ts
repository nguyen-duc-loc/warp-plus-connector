import assert from 'node:assert/strict';
import test from 'node:test';
import { acknowledged, job, options, policy, reportPage, workState } from './work-schema.js';

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
