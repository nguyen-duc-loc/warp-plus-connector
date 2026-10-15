import assert from 'node:assert/strict';
import { PassThrough } from 'node:stream';
import test, { type TestContext } from 'node:test';
import { setImmediate, setTimeout } from 'node:timers/promises';
import { render, Text, useInput } from 'ink';
import type { Channel } from './channel.js';
import { Controls, type ControlsAPI } from './controls.js';
import type { Hello } from './schema.js';

const candidate = '9007199254740993';
const at = { utc: '2026-10-15T00:00:00.000000Z', local: '2026-10-15 00:00:00', utc_offset_seconds: '0' };
const hello: Hello = {
    release: 'dev',
    commit: '',
    protocol: 3,
    parent_pid: '1',
    capabilities: ['overview', 'results', 'details', 'reveal', 'export', 'work', 'reports'],
    color: 'never',
    refresh_delay_ns: '5000000000',
    timezone: 'UTC',
    profile: { state_dir: '/fixture', config_path: null, suggestions: [] },
};
const policy = {
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

function Launcher({ api }: { api: ControlsAPI }) {
    useInput((input) => {
        if (!api.active && input === 'c')
            api.check({ view_id: '1', candidate_id: candidate, marked: '2', matching: '338' });
        if (!api.active && input === 'g') api.crawl();
        if (!api.active && input === 'h') api.recent([]);
        if (!api.active && input === 'w') api.work();
        if (!api.active && input === 'q') api.quit(0);
    });
    return api.active ? null : <Text>Results ready | {api.indicator}</Text>;
}

function completedReview(kind: 'crawl' | 'check' = 'check') {
    return {
        job_id: '1',
        kind: 'work_review',
        state: 'succeeded',
        phase: 'complete',
        completed_count: '1',
        expected_count: null,
        error_code: null,
        warning_code: null,
        remaining_path: null,
        result: {
            type: 'work_review',
            value: {
                review_id: '1',
                kind,
                created_at: at,
                collection_state: 'existing',
                sources: kind === 'crawl' ? [{ channel: 'first', page_limit: '10' }] : null,
                page_budget: kind === 'crawl' ? '10' : null,
                selection: kind === 'check' ? 'focused' : null,
                candidate_id: kind === 'check' ? candidate : null,
                selected_count: kind === 'check' ? '1' : null,
                view_id: kind === 'check' ? '1' : null,
                as_of: kind === 'check' ? at : null,
                filters: kind === 'check' ? { status: [], definitive: [], freshness: [] } : null,
                policy: kind === 'check' ? policy : null,
                image: kind === 'check' ? { text: `sha256:${'0'.repeat(64)}`, shortened: false } : null,
            },
        },
    };
}

const admitted = {
    operation_id: '7',
    revision: '1',
    kind: 'check',
    state: 'running',
    phase: 'admission',
    observed_at: at,
    elapsed_ns: '0',
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

function deferred() {
    let resolve!: (value: unknown) => void;
    const promise = new Promise<unknown>((done) => {
        resolve = done;
    });
    return { promise, resolve };
}

function controlsHarness(t: TestContext, request: (method: string, params: unknown) => Promise<unknown>) {
    const input = Object.assign(new PassThrough(), {
        isTTY: true,
        setRawMode() {
            return this;
        },
        ref() {
            return this;
        },
        unref() {
            return this;
        },
    });
    const output = Object.assign(new PassThrough(), { isTTY: true, columns: 60, rows: 16 });
    let text = '';
    output.on('data', (chunk) => {
        text += chunk.toString();
    });
    const requests: { method: string; params: unknown }[] = [];
    const quits: number[] = [];
    let preparationCancels = 0;
    const channel = {
        connected: true,
        preparation: true,
        async request(method: string, params: unknown) {
            requests.push({ method, params });
            return request(method, params);
        },
        async cancelPreparation() {
            preparationCancels++;
        },
    };
    const app = render(
        <Controls
            channel={channel as unknown as Channel}
            hello={hello}
            quit={(code) => {
                quits.push(code);
            }}
        >
            {(api) => <Launcher api={api} />}
        </Controls>,
        {
            stdin: input as unknown as NodeJS.ReadStream,
            stdout: output as unknown as NodeJS.WriteStream,
            stderr: new PassThrough() as unknown as NodeJS.WriteStream,
            exitOnCtrlC: false,
            patchConsole: false,
            interactive: true,
        },
    );
    t.after(() => app.unmount());
    const frame = () => {
        const start = text.lastIndexOf('\x1b[?2026h');
        const end = text.indexOf('\x1b[?2026l', start);
        // biome-ignore lint/suspicious/noControlCharactersInRegex: Inspect rendered terminal frames.
        return end < 0 ? '' : text.slice(start, end).replace(/\x1b\[[0-?]*[ -/]*[@-~]/gu, '');
    };
    return {
        input,
        output,
        requests,
        quits,
        frame,
        send: async (value: string) => {
            input.write(value);
            await setImmediate();
        },
        preparationCancels: () => preparationCancels,
        see: (value: string) => until(() => frame().includes(value)),
    };
}

// Spec 0012 AC-1, AC-2, AC-5 and AC-13: keyboard setup excludes disabled sources,
// validates the override and treats printable shortcuts as text while editing.
test('crawl setup reviews only enabled choices and Back releases the review', async (t) => {
    const h = controlsHarness(t, async (method, params) => {
        if (method === 'work.options')
            return {
                sources: [
                    { channel: 'first', enabled: true, page_limit: '2' },
                    { channel: 'disabled', enabled: false, page_limit: '9' },
                    { channel: 'last', enabled: true, page_limit: '3' },
                ],
                validator: 'docker',
                image: { text: 'sha256:fixture', shortened: false },
                check_policy: policy,
                check_available: true,
                check_error_code: null,
            };
        if (method === 'work.review_crawl') {
            assert.deepEqual(params, { channels: ['first'], page_limit: '10' });
            return { job_id: '1' };
        }
        if (method === 'job.poll') return completedReview('crawl');
        assert.equal(method, 'work.review_release');
        return { released: true };
    });
    await h.see('Results ready');
    await h.send('g');
    await h.see('> [x] first');
    await h.send('\x1b[B');
    await h.see('> [ ] disabled');
    await h.send(' ');
    await h.send('\x1b[B');
    await h.see('> [x] last');
    await h.send(' ');
    await h.see('> [ ] last');
    await h.send('\t');
    await h.see('> Common page limit:');
    await h.send('q?w');
    await h.send('101');
    await h.see('Common page limit: 101');
    assert.deepEqual(h.quits, []);
    await h.send('\t');
    await h.see('> Back');
    await h.send('\t');
    await h.see('> Review');
    await h.send('\r');
    // A subsequent field navigation proves the rejected activation was handled.
    await h.send('\x1b[Z');
    await h.see('> Back');
    assert.equal(
        h.requests.some((r) => r.method === 'work.review_crawl'),
        false,
    );
    await h.send('\x1b[Z');
    await h.see('> Common page limit:');
    await h.send('\x7f');
    await h.see('Common page limit: 10');
    await h.send('\t');
    await h.see('> Back');
    await h.send('\t');
    await h.see('> Review');
    await h.send('\r');
    await h.see('> Back |');
    await h.see('Maximum page budget 10');
    await h.send('\r');
    await h.see('Select configured sources');
    await until(() => h.requests.some((r) => r.method === 'work.review_release'));
    assert.equal(
        h.requests.some((r) => r.method === 'work.start'),
        false,
    );
    assert.deepEqual(h.quits, []);
});

async function startFocused(h: ReturnType<typeof controlsHarness>) {
    await h.see('Results ready');
    await h.send('c');
    await h.see('> Focused (1)');
    await h.send('\r');
    await h.see('> Back |');
    await h.send('\t');
    await h.see('> Start check');
    await h.send('\r');
}

// Spec 0012 AC-5 and AC-9: repeated activation during a lost or delayed Start
// acknowledgement never repeats Start, and queued quit defaults to Stay.
test('a pending Start is sent once and queued quit defaults to Stay', async (t) => {
    const start = deferred();
    t.after(() => start.resolve({ operation_id: '7' }));
    const h = controlsHarness(t, async (method) => {
        if (method === 'work.review_check') return { job_id: '1' };
        if (method === 'job.poll') return completedReview();
        if (method === 'work.start') return start.promise;
        assert.equal(method, 'work.poll');
        return admitted;
    });
    await startFocused(h);
    await until(() => h.requests.some((r) => r.method === 'work.start'));
    await h.send('\r');
    await h.send('q');
    start.resolve({ operation_id: '7' });
    await h.see('> Stay |');
    assert.deepEqual(h.quits, []);
    assert.equal(h.requests.filter((r) => r.method === 'work.start').length, 1);
    await h.send('\r');
    await h.see('WPC | work');
    await h.send('q');
    await h.see('> Stay |');
    await h.send('\t');
    await h.see('> Cancel work and quit');
    await h.send('\r');
    await until(() => h.quits.length === 1);
    assert.deepEqual(h.quits, [0]);
});

// Spec 0012 AC-7 and AC-13: an outcome updates the indicator without reopening
// the work overlay over the Results surface the user chose.
test('completion updates the indicator without stealing Results focus', async (t) => {
    const poll = deferred();
    t.after(() => poll.resolve(admitted));
    const h = controlsHarness(t, async (method) => {
        if (method === 'work.review_check') return { job_id: '1' };
        if (method === 'job.poll') return completedReview();
        if (method === 'work.start') return { operation_id: '7' };
        assert.equal(method, 'work.poll');
        return poll.promise;
    });
    await startFocused(h);
    await h.see('WPC | work');
    await h.send('\x1b');
    await h.see('Results ready');
    poll.resolve({ ...admitted, state: 'failed', phase: 'complete', error_code: 'state_busy' });
    await h.see('Results ready | Outcome available');
    assert.equal(h.frame().includes('WPC | work'), false);
    assert.deepEqual(h.quits, []);
});

// Spec 0012 AC-7, AC-8 and AC-13: work and preparation cancellation are separate
// keyboard actions, including help and terminals smaller than the normal layout.
test('work cancellation stays available during report preparation and small help', async (t) => {
    const report = deferred();
    const cancelledJob = {
        job_id: '2',
        kind: 'report_open',
        state: 'cancelled',
        phase: 'report',
        completed_count: '0',
        expected_count: null,
        error_code: 'cancelled',
        warning_code: null,
        remaining_path: null,
        result: null,
    };
    t.after(() => report.resolve(cancelledJob));
    const h = controlsHarness(t, async (method, params) => {
        if (method === 'work.review_check') return { job_id: '1' };
        if (method === 'job.poll')
            return (params as { job_id: string }).job_id === '1' ? completedReview() : report.promise;
        if (method === 'work.start') return { operation_id: '7' };
        if (method === 'work.poll') return admitted;
        if (method === 'report.open') {
            assert.deepEqual(params, { run_id: candidate });
            return { job_id: '2' };
        }
        assert.equal(method, 'work.cancel');
        assert.deepEqual(params, { operation_id: '7' });
        return { ...admitted, state: 'cancelling', phase: 'cleanup', cancellation_requested: true };
    });
    await startFocused(h);
    await h.see('check | running');
    await h.send('\x1b');
    await h.see('Results ready');
    await h.send('h');
    await h.see('> Open run ID');
    await h.send('\r');
    await h.see('> Saved run ID:');
    await h.send(candidate);
    await h.see(`Saved run ID: ${candidate}`);
    await h.send('\t');
    await h.see('> Open');
    await h.send('\r');
    await until(() => h.requests.some((r) => r.method === 'report.open'));
    h.output.columns = 40;
    h.output.rows = 10;
    h.output.emit('resize');
    await h.see('Resize to at least');
    await h.send('?');
    await h.see('Keyboard help');
    await h.send('\x18');
    await until(() => h.requests.some((r) => r.method === 'work.cancel'));
    assert.equal(h.preparationCancels(), 0);
    await h.send('\x07');
    await until(() => h.preparationCancels() === 1);
    report.resolve(cancelledJob);
    await until(() => h.frame().includes('Work was cancelled'));
    assert.equal(h.requests.filter((r) => r.method === 'work.cancel').length, 1);
    assert.deepEqual(h.quits, []);
});

async function until(check: () => boolean) {
    for (let i = 0; i < 300; i++) {
        if (check()) return;
        await setTimeout(10);
    }
    assert.fail('The expected controls frame did not render');
}

// Spec 0012 AC-3 and AC-13: review identifies the exact focused candidate before Start,
// including at the minimum size, without inventing a focused identity for a batch.
for (const [columns, rows] of [
    [60, 16],
    [110, 45],
] as const) {
    for (const selection of ['focused', 'marked', 'matching'] as const) {
        test(`${selection} review shows its exact selection at ${columns} by ${rows}`, async (t) => {
            const input = Object.assign(new PassThrough(), {
                isTTY: true,
                setRawMode() {
                    return this;
                },
                ref() {
                    return this;
                },
                unref() {
                    return this;
                },
            });
            const output = Object.assign(new PassThrough(), { isTTY: true, columns, rows });
            let text = '';
            output.on('data', (chunk) => {
                text += chunk.toString();
            });
            const requests: string[] = [];
            const channel = {
                connected: true,
                async request(method: string, params: unknown) {
                    requests.push(method);
                    if (method === 'work.review_check') {
                        assert.deepEqual(params, {
                            view_id: '1',
                            selection,
                            candidate_id: selection === 'focused' ? candidate : null,
                        });
                        return { job_id: '1' };
                    }
                    assert.equal(method, 'job.poll');
                    return {
                        job_id: '1',
                        kind: 'work_review',
                        state: 'succeeded',
                        phase: 'complete',
                        completed_count: '1',
                        expected_count: null,
                        error_code: null,
                        warning_code: null,
                        remaining_path: null,
                        result: {
                            type: 'work_review',
                            value: {
                                review_id: '1',
                                kind: 'check',
                                created_at: at,
                                collection_state: 'existing',
                                sources: null,
                                page_budget: null,
                                selection,
                                candidate_id: selection === 'focused' ? candidate : null,
                                selected_count: selection === 'focused' ? '1' : selection === 'marked' ? '2' : '338',
                                view_id: '1',
                                as_of: at,
                                filters: { status: [], definitive: [], freshness: [] },
                                policy,
                                image: { text: `sha256:${'0'.repeat(64)}`, shortened: false },
                            },
                        },
                    };
                },
            };
            const app = render(
                <Controls channel={channel as unknown as Channel} hello={hello} quit={() => assert.fail('quit')}>
                    {(api) => <Launcher api={api} />}
                </Controls>,
                {
                    stdin: input as unknown as NodeJS.ReadStream,
                    stdout: output as unknown as NodeJS.WriteStream,
                    stderr: new PassThrough() as unknown as NodeJS.WriteStream,
                    exitOnCtrlC: false,
                    patchConsole: false,
                    interactive: true,
                },
            );
            t.after(() => app.unmount());
            const frame = () => {
                const start = text.lastIndexOf('\x1b[?2026h');
                const end = text.indexOf('\x1b[?2026l', start);
                // biome-ignore lint/suspicious/noControlCharactersInRegex: Captured terminal frames contain ANSI controls.
                return end < 0 ? '' : text.slice(start, end).replace(/\x1b\[[0-?]*[ -/]*[@-~]/gu, '');
            };
            await until(() => frame().includes('Results ready'));
            input.write('c');
            await until(() => frame().includes('> Focused (1)'));
            if (selection !== 'focused') {
                input.write('\t');
                await until(() => frame().includes('> Marked (2)'));
            }
            if (selection === 'matching') {
                input.write('\t');
                await until(() => frame().includes('> Matching (338)'));
            }
            input.write('\r');
            await until(() => frame().includes('> Back |') && frame().includes(`Selection ${selection}`));
            assert.equal(frame().includes(`Candidate ${candidate}`), selection === 'focused', frame());
            assert.equal(frame().includes('9007199254740992'), false);
            assert.equal(requests.includes('work.start'), false, 'review must not start work');
        });
    }
}
