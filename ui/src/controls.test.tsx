import assert from 'node:assert/strict';
import { PassThrough } from 'node:stream';
import test from 'node:test';
import { setTimeout } from 'node:timers/promises';
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
    });
    return api.active ? null : <Text>Results ready</Text>;
}

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
