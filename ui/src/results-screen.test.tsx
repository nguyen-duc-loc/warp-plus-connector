import assert from 'node:assert/strict';
import { PassThrough } from 'node:stream';
import test, { type TestContext } from 'node:test';
import { setTimeout } from 'node:timers/promises';
import { render } from 'ink';
import type { Channel } from './channel.js';
import { ResultsScreen } from './results-screen.js';
import type { Hello } from './schema.js';

const at = { utc: '2026-10-15T00:00:00.000000Z', local: '2026-10-15 00:00:00', utc_offset_seconds: '0' };
const summary = {
    id: '1',
    first_seen_at: at,
    last_seen_at: at,
    latest_outcome: 'unchecked',
    latest_reason: null,
    last_definitive: null,
    evidence_at: null,
    freshness: 'not_applicable',
    clock_anomaly: false,
    latest_attempt_id: null,
    definitive_attempt_id: null,
    active_attempt_id: null,
};
const view = {
    snapshot_id: '1',
    view_id: '1',
    as_of: at,
    max_age_ns: '1',
    filters: { status: [], definitive: [], freshness: [] },
    matching_count: '1',
    marked_count: '0',
    removed_marks: '0',
    focus_id: '1',
    focus_offset: '0',
    retained: false,
    error_code: null,
};
const welcome: Hello = {
    release: 'dev',
    commit: '',
    protocol: 2,
    parent_pid: '1',
    capabilities: ['overview', 'results', 'details', 'reveal', 'export'],
    color: 'never',
    refresh_delay_ns: '5000000000',
    timezone: 'UTC',
    profile: {
        state_dir: '/fixture',
        config_path: null,
        suggestions: [
            { action: 'sources', command_text: 'wpc sources' },
            { action: 'crawl', command_text: 'wpc crawl' },
        ],
    },
};

async function until(check: () => boolean) {
    for (let i = 0; i < 300; i++) {
        if (check()) return;
        await setTimeout(10);
    }
    assert.fail('UI did not finish the expected input or request');
}

// covers: spec 0011 AC-5, AC-6, AC-7. Text input and disclosure choices stay separate from shortcuts.
test('export requires review and defaults to Back with keys hidden', async (t) => {
    const ui = await resultsFixture(t);
    await ui.press('e', '> Selection: matching');
    await ui.press('\t', '> Format: json');
    await ui.press(' ', '> Format: keys');
    await ui.press(' ', '> Format: json');
    await ui.press('\t', '> [ ] Include keys');
    await ui.press('\t', '> Destination:');
    await ui.press('qre file.json', 'Destination: qre file.json');
    assert.deepEqual(ui.exits, []);
    await ui.press('\t', '> Review export');
    await ui.press('\r', '> Back |');
    assert.deepEqual(ui.requests.find((r) => r.method === 'export.review')!.params, {
        view_id: '1',
        selection: 'matching',
        format: 'json',
        include_keys: false,
        path: 'qre file.json',
    });
    assert.equal(
        ui.requests.some((r) => r.method === 'export.start'),
        false,
    );
    await ui.press('\r', '> Review export');
    assert.equal(ui.requests.filter((r) => r.method === 'export.discard').length, 1);
    assert.equal(
        ui.requests.some((r) => r.method === 'export.start'),
        false,
    );
});

// covers: spec 0011 AC-6, AC-9, AC-15. A failed receipt never erases known publication facts.
for (const [published, durability, label] of [
    [true, 'confirmed', 'File created'],
    [false, 'not_published', 'This operation did not create a file'],
    [null, 'uncertain', 'File may have been created'],
] as const) {
    test(`export outcome says ${label} and requires acknowledgement`, async (t) => {
        const ui = await resultsFixture(t, { published, durability });
        await ui.press('e', '> Selection: matching');
        await ui.press('\t', '> Format: json');
        await ui.press('\t', '> [ ] Include keys');
        await ui.press('\t', '> Destination:');
        await ui.press('result.json', 'Destination: result.json');
        await ui.press('\t', '> Review export');
        await ui.press('\r', '> Back |');
        await ui.press('\t', '> Create file');
        await ui.press('\r', label);
        assert.equal(ui.requests.filter((r) => r.method === 'export.start').length, 1);
        await ui.press('?', 'Keyboard help');
        await ui.press('?', label);
        assert.equal(ui.requests.filter((r) => r.method === 'export.start').length, 1);
        await ui.press('\r', 'Rows 1 to 1 of 1');
        await ui.press('e', 'Destination: (enter a file path)');
        assert.equal(ui.requests.filter((r) => r.method === 'export.review').length, 1);
    });
}

// covers: spec 0011 AC-5, AC-6. Help and an unusable terminal clear disclosure permanently.
test('help and resize clear a revealed key while compact results preserve full IDs', async (t) => {
    const ui = await resultsFixture(t);
    ui.output.columns = 60;
    ui.output.emit('resize');
    await until(() => ui.ready('Candidate 9007199254740993'));
    ui.output.columns = 110;
    ui.output.emit('resize');
    await until(() => ui.ready('Rows 1 to 1 of 1'));
    await ui.press('\r', 'Key hidden.');
    await ui.press('k', 'Key "private-revealed-value"');
    await ui.press('?', 'Keyboard help');
    await ui.press('?', 'Key hidden.');
    assert.equal(ui.frame().includes('private-revealed-value'), false);
    await ui.press('k', 'Key "private-revealed-value"');
    ui.output.columns = 59;
    ui.output.emit('resize');
    await until(() => ui.ready('Resize to at least 60 columns'));
    ui.output.columns = 110;
    ui.output.emit('resize');
    await until(() => ui.ready('Key hidden.'));
    assert.equal(ui.frame().includes('private-revealed-value'), false);
});

async function resultsFixture(
    t: TestContext,
    outcome = { published: true as boolean | null, durability: 'confirmed' },
) {
    const input = Object.assign(new PassThrough(), {
        isTTY: true,
        isRaw: false,
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
    const output = Object.assign(new PassThrough(), { isTTY: true, columns: 110, rows: 30 });
    let text = '';
    output.on('data', (chunk) => {
        text += chunk.toString();
    });
    const requests: { method: string; params: unknown }[] = [];
    const exits: number[] = [];
    const exactSummary = { ...summary, id: '9007199254740993' };
    const exactView = { ...view, focus_id: exactSummary.id };
    let current = 'open',
        jobID = 0,
        pending = false;
    const reviewed = {
        review_id: '1',
        snapshot_id: '1',
        view_id: '1',
        selection: 'matching',
        selected_count: '1',
        matching_count: '1',
        filters: view.filters,
        as_of: at,
        max_age_ns: '1',
        retained: false,
        format: 'json',
        include_keys: false,
        destination: '/fixture/qre file.json',
    };
    const channel = {
        connected: true,
        async request(method: string, params: unknown) {
            assert.equal(pending, false, 'only one request may be outstanding');
            pending = true;
            requests.push({ method, params });
            try {
                if (['results.open', 'details.open', 'export.review', 'export.start'].includes(method)) {
                    current =
                        method === 'results.open'
                            ? 'open'
                            : method === 'details.open'
                              ? 'details'
                              : method === 'export.review'
                                ? 'review'
                                : 'export';
                    return { job_id: String(++jobID) };
                }
                if (method === 'job.poll') {
                    const result =
                        current === 'open'
                            ? { type: 'view', value: exactView }
                            : current === 'details'
                              ? {
                                    type: 'detail',
                                    value: {
                                        detail_id: '1',
                                        view_id: '1',
                                        candidate_id: exactSummary.id,
                                        as_of: at,
                                        summary: exactSummary,
                                        source_count: '0',
                                        attempt_count: '0',
                                    },
                                }
                              : current === 'review'
                                ? { type: 'review', value: reviewed }
                                : {
                                      type: 'export',
                                      value: {
                                          destination: reviewed.destination,
                                          selection: 'matching',
                                          format: 'json',
                                          include_keys: false,
                                          selected_count: '1',
                                          prepared_count: '1',
                                          ...outcome,
                                          error_code: 'cancelled',
                                          remaining_path: null,
                                      },
                                  };
                    return {
                        job_id: String(jobID),
                        kind: current,
                        state: current === 'export' ? 'cancelled' : 'succeeded',
                        phase: current === 'export' ? 'publication' : 'complete',
                        completed_count: '1',
                        expected_count: '1',
                        error_code: current === 'export' ? 'cancelled' : null,
                        warning_code: null,
                        remaining_path: null,
                        result,
                    };
                }
                if (method === 'results.page')
                    return {
                        view: exactView,
                        offset: '0',
                        items: [{ summary: exactSummary, marked: false }],
                        next_offset: null,
                    };
                if (method === 'candidate.reveal')
                    return {
                        view_id: '1',
                        detail_id: '1',
                        candidate_id: exactSummary.id,
                        key: 'private-revealed-value',
                    };
                if (method === 'export.discard' || method === 'details.close') return {};
                assert.fail(`unexpected request ${method}`);
            } finally {
                pending = false;
            }
        },
    };
    const app = render(
        <ResultsScreen
            channel={channel as unknown as Channel}
            hello={welcome}
            active
            onOverview={() => assert.fail('unexpected overview')}
            quit={(code) => {
                exits.push(code);
            }}
        />,
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
    const frame = (after = 0) => {
        const start = text.lastIndexOf('\x1b[?2026h'),
            end = text.indexOf('\x1b[?2026l', start);
        // biome-ignore lint/suspicious/noControlCharactersInRegex: Strip real terminal escape sequences from captured frames.
        return start < after || end < 0 ? '' : text.slice(start, end).replace(/\x1b\[[0-?]*[ -/]*[@-~]/gu, '');
    };
    const ready = (needle: string, after = 0) => {
        const latest = frame(after);
        return latest.includes(needle) && !latest.includes('c or Esc cancel | ? help | q quit');
    };
    await until(() => ready('Rows 1 to 1 of 1'));
    return {
        requests,
        exits,
        output,
        frame,
        ready,
        async press(key: string, needle: string) {
            const after = text.length;
            input.write(key);
            await until(() => ready(needle, after));
        },
    };
}

// Spec 0011 AC-5: intent processed during a reveal invalidates that reply, while
// leaving and refresh wait for the sole outstanding request before continuing.
for (const [name, key] of [
    ['leave', '\x1b'],
    ['cancel', 'c'],
    ['hide', 'k'],
    ['refresh', 'r'],
] as const) {
    test(`pending reveal ${name} discards its reply and serializes following work`, async (t) => {
        const input = Object.assign(new PassThrough(), {
            isTTY: true,
            isRaw: false,
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
        const output = Object.assign(new PassThrough(), { isTTY: true, columns: 110, rows: 30 });
        let text = '';
        output.on('data', (chunk) => {
            text += chunk.toString();
        });
        let resolveReveal: (value: unknown) => void = () => {};
        const reveal = new Promise<unknown>((resolve) => {
            resolveReveal = resolve;
        });
        const requests: string[] = [];
        let pending = false,
            details = 0,
            job = 'open';
        const channel = {
            connected: true,
            async request(method: string) {
                assert.equal(pending, false, 'channel requests must never overlap');
                pending = true;
                requests.push(method);
                try {
                    if (method === 'results.open') {
                        job = 'open';
                        return { job_id: '1' };
                    }
                    if (method === 'details.open') {
                        job = 'details';
                        details++;
                        return { job_id: String(details + 1) };
                    }
                    if (method === 'job.poll')
                        return {
                            job_id: job === 'open' ? '1' : String(details + 1),
                            kind: job,
                            state: 'succeeded',
                            phase: 'complete',
                            completed_count: '1',
                            expected_count: '1',
                            error_code: null,
                            warning_code: null,
                            remaining_path: null,
                            result:
                                job === 'open'
                                    ? { type: 'view', value: view }
                                    : {
                                          type: 'detail',
                                          value: {
                                              detail_id: String(details),
                                              view_id: '1',
                                              candidate_id: '1',
                                              as_of: at,
                                              summary,
                                              source_count: '0',
                                              attempt_count: '0',
                                          },
                                      },
                        };
                    if (method === 'results.page')
                        return { view, offset: '0', items: [{ summary, marked: false }], next_offset: null };
                    if (method === 'candidate.reveal') return await reveal;
                    if (method === 'details.close') return {};
                    assert.fail(`unexpected request ${method}`);
                } finally {
                    pending = false;
                }
            },
        };
        const exits: number[] = [];
        const app = render(
            <ResultsScreen
                channel={channel as unknown as Channel}
                hello={welcome}
                active
                onOverview={() => assert.fail('unexpected overview')}
                quit={(code) => {
                    exits.push(code);
                }}
            />,
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
        const frame = (after = 0) => {
            const start = text.lastIndexOf('\x1b[?2026h');
            const end = text.indexOf('\x1b[?2026l', start);
            // biome-ignore lint/suspicious/noControlCharactersInRegex: Strip real terminal escape sequences from captured frames.
            return start < after || end < 0 ? '' : text.slice(start, end).replace(/\x1b\[[0-?]*[ -/]*[@-~]/gu, '');
        };
        const ready = (needle: string, after = 0) => {
            const latest = frame(after);
            return latest.includes(needle) && !latest.includes('c or Esc cancel | ? help | q quit');
        };
        await until(() => ready('Rows 1 to 1 of 1'));
        const entering = text.length;
        input.write('\r');
        await until(() => ready('Key hidden.', entering));
        input.write('k');
        await until(() => requests.includes('candidate.reveal'));
        const cancelling = text.length;
        input.write(key);
        await until(() => frame(cancelling).includes('Cancellation requested'));
        assert.equal(details, 1);
        assert.equal(requests.includes('details.close'), false);
        const after = text.length;
        resolveReveal({ view_id: '1', detail_id: '1', candidate_id: '1', key: 'private_late_reveal' });
        if (name === 'leave') {
            await until(() => requests.includes('details.close') && ready('Rows 1 to 1 of 1', after));
        } else if (name === 'refresh') {
            await until(() => details === 2 && ready('Key hidden.', after));
        } else {
            await until(() => ready('Tab sections | Up/Down scroll', after));
            assert.equal(details, 1);
            assert.equal(requests.includes('details.close'), false);
        }
        assert.equal(text.includes('private_late_reveal'), false);
        assert.deepEqual(exits, []);
    });
}
