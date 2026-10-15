import { attempt, count, display, filters, id, pageBounds } from './results-schema.js';
import { array, bool, code, invalid, nullable, object, one, run, stamp, text, type Value } from './schema.js';

const totals = object(
    Object.fromEntries(
        [
            'selected',
            'pending',
            'active',
            'completed',
            'skipped',
            'interrupted',
            'confirmed',
            'invalid',
            'inconclusive',
            'unresolved',
            'unstarted',
        ].map((k) => [k, count]),
    ),
);
const policyShape = object({
    version: id,
    workers: id,
    max_attempts: id,
    mutation_interval_ns: id,
    candidate_timeout_ns: id,
    attempt_timeout_ns: id,
    command_timeout_ns: id,
    poll_timeout_ns: id,
    poll_interval_ns: id,
    cleanup_timeout_ns: id,
    shutdown_timeout_ns: id,
    finalize_timeout_ns: id,
    retry_base_ns: id,
    retry_cap_ns: id,
    memory_mib: id,
    cpus: id,
    processes: id,
    protocol_version: id,
});
export function policy(value: unknown) {
    const p = policyShape(value);
    const fixed = {
        version: '1',
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
    for (const [k, v] of Object.entries(fixed)) if (p[k as keyof typeof p] !== v) invalid();
    if (
        BigInt(p.workers) > 4n ||
        BigInt(p.max_attempts) > 5n ||
        BigInt(p.mutation_interval_ns) < 1000000000n ||
        BigInt(p.mutation_interval_ns) > 60000000000n ||
        BigInt(p.mutation_interval_ns) % 1000000000n !== 0n ||
        BigInt(p.candidate_timeout_ns) < 60000000000n ||
        BigInt(p.candidate_timeout_ns) > 3600000000000n
    )
        invalid();
    return p;
}
const channel = (v: unknown) => (typeof v === 'string' && /^[a-z0-9_]{1,64}$/.test(v) ? v : invalid());
const pageLimit = (v: unknown) => (BigInt(id(v)) <= 100n ? id(v) : invalid());
const plan = object({ channel, page_limit: pageLimit });
const optionsShape = object({
    sources: array(object({ channel, enabled: bool, page_limit: pageLimit }), 0, 32),
    validator: one('unsupported', 'docker'),
    image: nullable(display),
    check_policy: policy,
    check_available: bool,
    check_error_code: nullable(code),
});
export function options(value: unknown) {
    const o = optionsShape(value);
    if (
        new Set(o.sources.map((s) => s.channel)).size !== o.sources.length ||
        (o.validator === 'docker'
            ? !o.check_available || o.image === null || o.check_error_code !== null
            : o.check_available || o.image !== null || o.check_error_code !== 'validation_unsupported')
    )
        invalid();
    return o;
}
const reviewShape = object({
    review_id: id,
    kind: one('crawl', 'check'),
    created_at: stamp,
    collection_state: one('absent', 'existing'),
    sources: nullable(array(plan, 1, 32)),
    page_budget: nullable(id),
    selection: nullable(one('focused', 'marked', 'matching')),
    candidate_id: nullable(id),
    selected_count: nullable(id),
    view_id: nullable(id),
    as_of: nullable(stamp),
    filters: nullable(filters),
    policy: nullable(policy),
    image: nullable(display),
});
export function review(v: unknown) {
    const r = reviewShape(v);
    if (r.kind === 'crawl') {
        if (
            !r.sources ||
            r.page_budget === null ||
            [r.selection, r.candidate_id, r.selected_count, r.view_id, r.as_of, r.filters, r.policy, r.image].some(
                (v) => v !== null,
            )
        )
            invalid();
        if (r.sources.reduce((n, p) => n + BigInt(p.page_limit), 0n) !== BigInt(r.page_budget)) invalid();
    } else if (
        r.sources !== null ||
        r.page_budget !== null ||
        r.selection === null ||
        r.selected_count === null ||
        r.view_id === null ||
        r.as_of === null ||
        r.filters === null ||
        r.policy === null ||
        r.image === null ||
        (r.selection === 'focused') !== (r.candidate_id !== null)
    )
        invalid();
    if (r.selection === 'focused' && r.selected_count !== '1') invalid();
    return r;
}
export type Review = ReturnType<typeof review>;
export const unresolved = object({
    operation_id: id,
    run_id: nullable(id),
    run_created: one('true', 'false', 'unknown'),
    error_code: code,
});
const crawl = object({
    selected_sources: count,
    completed_sources: count,
    failed_sources: count,
    skipped_sources: count,
    page_budget: count,
    requested_pages: count,
    completed_pages: count,
    saved_candidates: count,
    saved_sightings: count,
    current_channel: nullable(channel),
    current_page: nullable(id),
});
const check = object({
    totals,
    attempts_started: count,
    attempts_completed: count,
    retry_waiting: count,
    next_retry_wait_ns: nullable(count),
    policy,
    workers: array(
        object({
            candidate_id: id,
            attempt_number: id,
            phase: one('preparing', 'registering', 'attaching', 'polling', 'cleanup'),
        }),
        0,
        4,
    ),
});
const stateShape = object({
    operation_id: id,
    revision: id,
    kind: one('crawl', 'check'),
    state: one('running', 'cancelling', 'succeeded', 'failed', 'cancelled'),
    phase: one('admission', 'recovery', 'crawl', 'check', 'retry_wait', 'cleanup', 'finalization', 'complete'),
    observed_at: stamp,
    elapsed_ns: count,
    stop_elapsed_ns: nullable(count),
    run_id: nullable(id),
    run_created: one('true', 'false', 'unknown'),
    run_status: nullable(one('running', 'completed', 'failed', 'cancelled', 'interrupted')),
    cancellation_requested: bool,
    persistence: one('known', 'uncertain'),
    cleanup: one('not_needed', 'pending', 'complete', 'unresolved'),
    error_code: nullable(code),
    crawl: nullable(crawl),
    check: nullable(check),
    guidance: object({
        action: one(
            'none',
            'retry_review',
            'inspect_report',
            'configure_validator',
            'inspect_owner',
            'stop_service',
            'recover_validator',
            'review_single_candidate',
            'restart_session',
        ),
        retry_at: nullable(stamp),
        service_observed_at: nullable(stamp),
    }),
    unresolved_cleanup: nullable(unresolved),
});
export function workState(v: unknown) {
    const r = stateShape(v),
        terminal = !['running', 'cancelling'].includes(r.state);
    if (
        (r.run_created === 'true') !== (r.run_id !== null) ||
        terminal !== (r.phase === 'complete') ||
        (r.state === 'failed' && r.error_code === null) ||
        (['running', 'succeeded'].includes(r.state) && r.error_code !== null)
    )
        invalid();
    if (
        r.run_id === null
            ? r.crawl !== null || r.check !== null
            : r.kind === 'crawl'
              ? r.crawl === null || r.check !== null
              : r.check === null || r.crawl !== null
    )
        invalid();
    if (
        r.check &&
        (BigInt(r.check.workers.length) > BigInt(r.check.policy.workers) ||
            new Set(r.check.workers.map((w) => w.candidate_id)).size !== r.check.workers.length)
    )
        invalid();
    return r;
}
export type WorkState = ReturnType<typeof workState>;
export const started = object({ operation_id: id });
export const released = object({ released: (v: unknown) => (v === true ? true : invalid()) });
export const acknowledged = object({
    acknowledged: (v: unknown) => (v === true ? true : invalid()),
    unresolved_cleanup: nullable(unresolved),
});
export const focus = object({
    section: one('summary', 'sources', 'pages', 'items', 'attempts'),
    source_id: nullable(text),
    candidate_id: nullable(id),
    page_number: nullable(id),
    attempt_id: nullable(id),
    offset: count,
});
export type Focus = Value<typeof focus>;
const legacy = object({
    source_id: display,
    requested_url: display,
    coverage_mode: display,
    page_limit: count,
    fetched_at: nullable(stamp),
    parsed_post_count: nullable(count),
    min_message_id: nullable(id),
    max_message_id: nullable(id),
});
export const report = object({
    report_id: id,
    as_of: stamp,
    run,
    legacy: bool,
    source_count: count,
    page_count: count,
    item_count: nullable(count),
    attempt_count: count,
    crawl_counts: nullable(object({ saved_candidates: count, saved_sightings: count })),
    validation_counts: nullable(totals),
    policy: nullable(policy),
    backend_id: nullable(display),
    backend_version: nullable(display),
    legacy_coverage: nullable(legacy),
    focus,
    retained: bool,
    error_code: nullable(code),
});
export type Report = Value<typeof report>;
const source = object({
    source_id: text,
    channel: display,
    position: count,
    page_limit: id,
    status: one('pending', 'running', 'completed', 'failed', 'cancelled', 'skipped', 'interrupted'),
    started_at: nullable(stamp),
    finished_at: nullable(stamp),
    recovered_at: nullable(stamp),
    stop_reason: nullable(code),
    page_count: count,
    saved_candidates: count,
    saved_sightings: count,
});
const page = object({
    source_id: text,
    page_number: id,
    requested_url: display,
    status: one('requested', 'parsed', 'completed', 'failed', 'cancelled', 'interrupted'),
    requested_at: stamp,
    before_id: nullable(id),
    fetched_at: nullable(stamp),
    finished_at: nullable(stamp),
    recovered_at: nullable(stamp),
    parsed_post_count: nullable(count),
    min_message_id: nullable(id),
    max_message_id: nullable(id),
    next_before_id: nullable(id),
    reason_code: nullable(code),
    saved_candidates: count,
    saved_sightings: count,
});
const item = object({
    candidate_id: id,
    ordinal: count,
    status: one('pending', 'active', 'completed', 'skipped', 'interrupted'),
    stop_reason: nullable(code),
    started_at: nullable(stamp),
    finished_at: nullable(stamp),
    recovered_at: nullable(stamp),
    attempt_count: count,
    latest_outcome: nullable(one('confirmed', 'invalid', 'inconclusive')),
});
const reportAttempt = object({ candidate_id: id, attempt });
export function reportPage(v: unknown) {
    const p = object({
        report_id: id,
        section: one('sources', 'pages', 'items', 'attempts'),
        source_id: nullable(text),
        candidate_id: nullable(id),
        offset: count,
        total: count,
        items: array((v: unknown) => v, 0, 50),
        next_offset: nullable(count),
    })(v);
    const items = p.items.map((v) =>
        p.section === 'sources'
            ? source(v)
            : p.section === 'pages'
              ? page(v)
              : p.section === 'items'
                ? item(v)
                : reportAttempt(v),
    );
    pageBounds(p.offset, items.length, p.total, p.next_offset);
    for (const row of items) if ('attempt' in row && row.candidate_id !== row.attempt.candidate_id) invalid();
    if (Buffer.byteLength(JSON.stringify(p.items)) > 768 * 1024 + 101) invalid();
    return { ...p, items };
}
export type ReportPage = ReturnType<typeof reportPage>;
const jobShape = object({
    job_id: id,
    kind: one('work_review', 'report_open', 'report_refresh'),
    state: one('running', 'cancelling', 'succeeded', 'failed', 'cancelled'),
    phase: one('selection', 'report', 'cleanup', 'complete'),
    completed_count: count,
    expected_count: nullable(count),
    error_code: nullable(code),
    warning_code: nullable(one('ui_snapshot_cleanup_failed')),
    remaining_path: nullable(text),
    result: nullable((v: unknown) => {
        const r = object({ type: one('work_review', 'report'), value: (v: unknown) => v })(v);
        return r.type === 'work_review'
            ? { type: r.type, value: review(r.value) }
            : { type: r.type, value: report(r.value) };
    }),
});

export function job(value: unknown) {
    const j = jobShape(value),
        active = j.state === 'running' || j.state === 'cancelling';
    if (active && (j.result !== null || j.error_code !== null || j.warning_code !== null)) invalid();
    if (j.state === 'succeeded' && (j.result === null || j.error_code !== null || j.phase !== 'complete')) invalid();
    if (
        (j.state === 'failed' || j.state === 'cancelled') &&
        (j.result !== null || j.error_code === null || j.phase === 'complete')
    )
        invalid();
    if (j.result && j.result.type !== (j.kind === 'work_review' ? 'work_review' : 'report')) invalid();
    if (j.warning_code !== null && (j.state !== 'succeeded' || j.remaining_path === null)) invalid();
    return j;
}
