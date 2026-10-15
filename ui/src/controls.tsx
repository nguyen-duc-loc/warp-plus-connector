import { Box, Text, useInput, useWindowSize } from 'ink';
import { type ReactNode, useCallback, useEffect, useRef, useState } from 'react';
import type { Channel } from './channel.js';
import { started as jobStarted, OperationError, operation } from './results-schema.js';
import type { Hello, run, Schema, Value } from './schema.js';
import { safe, theme } from './theme.js';
import * as wire from './work-schema.js';

type Selection = { view_id: string; candidate_id: string | null; marked: string; matching: string };
type Recent = Value<typeof run>;
export type ControlsAPI = {
    active: boolean;
    indicator: string;
    cancelLabel: string;
    crawl: () => void;
    check: (s: Selection) => void;
    work: () => void;
    recent: (r: Recent[]) => void;
    quit: (code: number, interrupt?: boolean) => void;
};
type Surface = 'crawl' | 'check' | 'review' | 'work' | 'recent' | 'run_id' | 'report' | 'quit' | null;
const summaryFocus = (): wire.Focus => ({
    section: 'summary',
    source_id: null,
    candidate_id: null,
    page_number: null,
    attempt_id: null,
    offset: '0',
});
const terminal = (s: wire.WorkState) => !['running', 'cancelling'].includes(s.state);
const fields = (value: unknown, prefix = ''): string[] => {
    if (value === null) return [`${prefix}: Unavailable`];
    if (typeof value !== 'object') return [`${prefix}: ${String(value)}`];
    if ('text' in value && typeof value.text === 'string')
        return [`${prefix}: ${value.text}${'shortened' in value && value.shortened ? ' [shortened]' : ''}`];
    if ('local' in value && typeof value.local === 'string') return [`${prefix}: ${value.local}`];
    return Object.entries(value).flatMap(([k, v]) =>
        fields(v, `${prefix ? `${prefix} / ` : ''}${k.replaceAll('_', ' ')}`),
    );
};
function linesAtWidth(lines: string[], width: number): string[] {
    return lines.flatMap((line) => {
        const text = safe(line).replace(/[^\x20-\x7e]/gu, (c) => `\\u{${c.codePointAt(0)?.toString(16)}}`);
        const out = [];
        for (let n = 0; n < text.length; n += width) out.push(text.slice(n, n + width));
        return out.length ? out : [''];
    });
}
const explanation = (code: string) =>
    (
        ({
            state_busy:
                'Another writer owns this collection. Inspect the foreground runner or service before trying again.',
            validation_unsupported:
                'Docker validation is not configured. Configure the existing validator settings, then restart WPC.',
            ui_work_recovery_required:
                'This session cannot safely start more work. Inspect saved history and recovery, then restart.',
            ui_collection_changed: 'The collection changed. Refresh Results and prepare a new review.',
            ui_candidate_unavailable:
                'A reviewed candidate changed or disappeared. Refresh Results and review the selection again.',
            validator_probe_required:
                'Provider recovery requires a single candidate probe. Review one focused candidate.',
            validation_cleanup_pending:
                'Remote cleanup is unresolved. Inspect validator recovery before another check.',
            source_rate_limited:
                'Telegram limited requests. Earlier saved pages remain available; later sources were skipped.',
            ui_busy: 'Another preparation or unacknowledged operation owns this slot.',
            ui_work_selection_empty: 'Choose at least one saved candidate.',
            state_record_not_found: 'That saved run is unavailable.',
            cancelled: 'Work was cancelled. Committed evidence remains in the saved report.',
        }) as Record<string, string>
    )[code] ?? `Work could not finish (${code}). Inspect the saved report when available.`;
const duration = (n: string) => `${(BigInt(n) / 1000000n).toString()} ms`;
function policyLines(p: Value<typeof wire.policy> | null): string[] {
    return p
        ? Object.entries(p).map(
              ([k, v]) => `${k.replace(/_ns$/, '').replaceAll('_', ' ')}: ${k.endsWith('_ns') ? duration(v) : v}`,
          )
        : ['Policy unavailable'];
}
function progressLines(w: wire.WorkState): string[] {
    if (w.crawl) {
        const c = w.crawl;
        return [
            `Sources: ${c.selected_sources} selected, ${c.completed_sources} completed, ${c.failed_sources} failed, ${c.skipped_sources} skipped`,
            `Pages: ${c.requested_pages} request intents, ${c.completed_pages} completed, budget ${c.page_budget}`,
            `Saved candidates ${c.saved_candidates} | Saved sightings ${c.saved_sightings}`,
            `Current source ${c.current_channel ?? 'Unavailable'} | Page ${c.current_page ?? 'Unavailable'}`,
        ];
    }
    if (w.check) {
        const c = w.check;
        return [
            ...fields(c.totals, 'Items'),
            `Attempts: ${c.attempts_started} started, ${c.attempts_completed} completed`,
            `Workers ${c.workers.length} of ${c.policy.workers}`,
            ...c.workers.map((v) => `Candidate ${v.candidate_id}, attempt ${v.attempt_number}: ${v.phase}`),
            `Retry waiting ${c.retry_waiting}${c.next_retry_wait_ns === null ? '' : `, next wait ${duration(c.next_retry_wait_ns)}`}`,
        ];
    }
    return ['Saved progress is not yet available.'];
}
function reportSummary(r: wire.Report): string[] {
    return [
        `Origin ${r.run.origin} | Saved status ${r.run.status}`,
        `Started ${r.run.started_at.local}`,
        `Finished ${r.run.finished_at?.local ?? 'Unavailable'} | Recovered ${r.run.recovered_at?.local ?? 'Unavailable'}`,
        `Reason ${r.run.reason_code ?? 'None'}`,
        `Sources ${r.source_count} | Pages ${r.page_count} | Items ${r.item_count ?? 'Unavailable'} | Attempts ${r.attempt_count}`,
        ...fields(r.crawl_counts ?? r.validation_counts, 'Saved counts'),
        ...(r.legacy ? ['Legacy history: unavailable fields remain unavailable.'] : []),
        ...(r.policy ? [`Backend ${r.backend_id?.text} ${r.backend_version?.text}`, ...policyLines(r.policy)] : []),
        ...(r.legacy_coverage ? fields(r.legacy_coverage, 'Saved coverage') : []),
    ];
}
export function Controls({
    channel,
    hello,
    quit,
    children,
}: {
    channel: Channel;
    hello: Hello;
    quit: ControlsAPI['quit'];
    children: (api: ControlsAPI) => ReactNode;
}) {
    const { columns, rows } = useWindowSize(),
        width = Math.max(1, columns),
        height = Math.max(1, rows - 7);
    const small = columns < 60 || rows < 16;
    const [surface, setSurface] = useState<Surface>(null);
    const [work, setWork] = useState<wire.WorkState | null>(null);
    const [unresolved, setUnresolved] = useState<Value<typeof wire.unresolved> | null>(null);
    const [operationID, setOperationID] = useState<string | null>(null);
    const [received, setReceived] = useState(0);
    const [now, setNow] = useState(performance.now());
    const [options, setOptions] = useState<Value<typeof wire.options> | null>(null);
    const [selected, setSelected] = useState<string[]>([]);
    const [source, setSource] = useState(0);
    const [limit, setLimit] = useState('');
    const [field, setField] = useState(0);
    const [selection, setSelection] = useState<Selection | null>(null);
    const [review, setReview] = useState<wire.Review | null>(null);
    const [recent, setRecent] = useState<Recent[]>([]);
    const [runID, setRunID] = useState('');
    const [report, setReport] = useState<wire.Report | null>(null);
    const [page, setPage] = useState<wire.ReportPage | null>(null);
    const [reportFocus, setReportFocus] = useState(summaryFocus);
    const [row, setRow] = useState(0);
    const [busy, setBusy] = useState(false);
    const [message, setMessage] = useState('');
    const [help, setHelp] = useState(false);
    const [scroll, setScroll] = useState(0);
    const entered = useRef(false),
        generation = useRef(0),
        startPending = useRef(false),
        quitPending = useRef(false);
    const workRef = useRef(work);
    workRef.current = work;
    const returnSurface = useRef<Surface>(null);
    const alive = useRef(true);
    const rpc = useCallback(
        async <T,>(method: string, params: unknown, parse: Schema<T>) =>
            operation(await channel.request(method, params), parse),
        [channel],
    );
    const update = useCallback((s: wire.WorkState) => {
        setWork(s);
        setUnresolved(s.unresolved_cleanup);
        setReceived(performance.now());
    }, []);
    const perform = (fn: () => Promise<void>) => {
        if (entered.current) return;
        entered.current = true;
        setBusy(true);
        setMessage('');
        void fn()
            .catch((e) => {
                if (e instanceof OperationError) {
                    setMessage(explanation(e.message));
                    if (e.message === 'ui_collection_changed') {
                        setReport(null);
                        setPage(null);
                    }
                } else quit(1);
            })
            .finally(() => {
                entered.current = false;
                if (alive.current) setBusy(false);
            });
    };
    useEffect(() => {
        const timer = setInterval(() => setNow(performance.now()), 500);
        return () => {
            clearInterval(timer);
            alive.current = false;
        };
    }, []);
    useEffect(() => {
        if (!operationID) return;
        let live = true;
        let timer: NodeJS.Timeout | undefined;
        const poll = async () => {
            try {
                const s = await rpc('work.poll', { operation_id: operationID }, wire.workState);
                if (!live) return;
                update(s);
                if (!terminal(s)) timer = setTimeout(() => void poll(), 1000);
            } catch (e) {
                if (live) {
                    if (e instanceof OperationError) setMessage(explanation(e.message));
                    else quit(1);
                }
            }
        };
        void poll();
        return () => {
            live = false;
            clearTimeout(timer);
        };
    }, [operationID, rpc, update, quit]);
    useEffect(() => {
        if (surface === 'crawl')
            setScroll(Math.max(0, (field === 1 ? (options?.sources.length ?? 0) + 1 : source) - height + 3));
        if (surface === 'recent') setScroll(Math.max(0, field - height + 2));
    }, [surface, source, field, height, options]);
    const cancelWork = () => {
        if (!operationID || (work && (terminal(work) || work.cancellation_requested))) return;
        void rpc('work.cancel', { operation_id: operationID }, wire.workState)
            .then(update)
            .catch(() => quit(1));
    };
    const cancelPreparation = () => {
        void channel.cancelPreparation?.().catch(() => quit(1));
    };
    const open = (next: Surface) => {
        if (review && next !== 'review') {
            const id = review.review_id;
            setReview(null);
            void rpc('work.review_release', { review_id: id }, wire.released).catch((e) => {
                if (!(e instanceof OperationError)) quit(1);
            });
        }
        generation.current++;
        setHelp(false);
        setScroll(0);
        setField(0);
        setMessage('');
        setSurface(next);
    };
    const requestQuit: ControlsAPI['quit'] = (code, interrupt) => {
        if (code !== 0 || interrupt) {
            quit(code, interrupt);
            return;
        }
        if (startPending.current) {
            quitPending.current = true;
            return;
        }
        if (operationID && (!workRef.current || !terminal(workRef.current))) {
            returnSurface.current = surface;
            open('quit');
            return;
        }
        quit(0);
    };
    const quitRef = useRef(requestQuit);
    quitRef.current = requestQuit;
    const stableQuit = useCallback<ControlsAPI['quit']>((code, interrupt) => quitRef.current(code, interrupt), []);
    const leave = () => {
        generation.current++;
        open(null);
    };
    const prepare = async (method: string, params: unknown) => {
        const intent = generation.current;
        const started = await rpc(method, params, jobStarted);
        for (;;) {
            const job = await rpc('job.poll', { job_id: started.job_id }, wire.job);
            if (job.state === 'running' || job.state === 'cancelling') {
                setMessage(`${job.phase}: ${job.completed_count} prepared`);
                await new Promise((r) => setTimeout(r, 100));
                continue;
            }
            if (job.state !== 'succeeded' || !job.result) {
                if (method === 'report.refresh' && report) {
                    const retained = await rpc('report.state', { report_id: report.report_id }, wire.report);
                    setReport(retained);
                }
                throw new OperationError(job.error_code ?? 'ui_snapshot_failed');
            }
            if (intent !== generation.current) {
                if (job.result.type === 'work_review')
                    await rpc('work.review_release', { review_id: job.result.value.review_id }, wire.released);
                return;
            }
            if (job.result.type === 'work_review') {
                setReview(job.result.value);
                setSurface('review');
                setField(0);
            } else {
                setReport(job.result.value);
                const saved = job.result.value;
                setReportFocus(saved.focus);
                setPage(null);
                if (saved.focus.section !== 'summary') {
                    const f = saved.focus;
                    setPage(
                        await rpc(
                            'report.page',
                            {
                                report_id: saved.report_id,
                                section: f.section,
                                source_id: f.section === 'pages' ? f.source_id : null,
                                candidate_id: f.section === 'attempts' ? f.candidate_id : null,
                                offset: f.offset,
                            },
                            wire.reportPage,
                        ),
                    );
                }
                setRow(0);
                setSurface('report');
            }
            setScroll(0);
            setMessage(job.warning_code ?? '');
            return;
        }
    };
    const openReport = (id: string) => {
        open('report');
        perform(() => prepare('report.open', { run_id: id }));
    };
    const loadPage = async (f: wire.Focus) => {
        if (!report) return;
        const source_id = f.section === 'pages' ? f.source_id : null;
        const candidate_id = f.section === 'attempts' ? f.candidate_id : null;
        const p = await rpc(
            'report.page',
            { report_id: report.report_id, section: f.section, source_id, candidate_id, offset: f.offset },
            wire.reportPage,
        );
        setReportFocus(f);
        setPage(p);
        setRow(0);
        setScroll(0);
    };
    const currentFocus = (): wire.Focus => {
        const f = { ...reportFocus, offset: (BigInt(page?.offset ?? '0') + BigInt(row)).toString() };
        const item = page?.items[row];
        if (item) {
            if ('attempt' in item) f.attempt_id = item.attempt.id;
            else if ('candidate_id' in item) f.candidate_id = item.candidate_id;
            else if ('page_number' in item) f.page_number = item.page_number;
            else f.source_id = item.source_id;
        }
        return f;
    };
    const api: ControlsAPI = {
        active: surface !== null,
        indicator: work
            ? terminal(work)
                ? 'Outcome available'
                : `${work.kind}: ${work.phase}${now - received > 3000 ? ' (delayed)' : ''}`
            : 'Idle',
        cancelLabel: 'Ctrl+X Cancel work | Ctrl+G Cancel preparation',
        crawl: () => {
            open('crawl');
            setLimit('');
            setSource(0);
            perform(async () => {
                const o = await rpc('work.options', {}, wire.options);
                setOptions(o);
                setSelected(o.sources.filter((s) => s.enabled).map((s) => s.channel));
            });
        },
        check: (s) => {
            setSelection(s);
            open('check');
        },
        work: () => open('work'),
        recent: (r) => {
            setRecent(r);
            open('recent');
        },
        quit: stableQuit,
    };
    if (unresolved) api.indicator += ` | Cleanup unresolved, run ${unresolved.run_id ?? 'unknown'}`;
    useInput((input, key) => {
        if (key.ctrl && input === 'x') {
            cancelWork();
            return;
        }
        if (key.ctrl && input === 'g') {
            cancelPreparation();
            return;
        }
        if (surface === null) return;
        if (key.ctrl && input === 'c') {
            quit(130, true);
            return;
        }
        const textInput =
            !help && !small && !busy && ((surface === 'crawl' && field === 1) || (surface === 'run_id' && field === 0));
        if (textInput) {
            if (key.escape) {
                leave();
                return;
            }
            if (key.tab) {
                setField(surface === 'crawl' ? (key.shift ? 0 : 2) : key.shift ? 2 : 1);
                return;
            }
            if (key.return) return;
            const change = surface === 'crawl' ? setLimit : setRunID;
            if (key.backspace || key.delete) change((v) => v.slice(0, -1));
            else if (!key.ctrl && !key.meta && /^\d+$/.test(input)) change((v) => (v + input).slice(0, 19));
            return;
        }
        if (input === 'q') {
            requestQuit(0);
            return;
        }
        if (input === '?') {
            setHelp((v) => !v);
            setScroll(0);
            return;
        }
        if (help) {
            if (key.escape) setHelp(false);
            else if (key.upArrow) setScroll((v) => Math.max(0, v - 1));
            else if (key.downArrow) setScroll((v) => v + 1);
            return;
        }
        if (surface === 'quit') {
            if (key.escape) {
                open(returnSurface.current);
                return;
            }
            if (key.tab) setField((v) => 1 - v);
            if (key.return) {
                if (field === 0) open(returnSurface.current);
                else quit(0);
            }
            return;
        }
        if (startPending.current) return;
        if (key.escape) {
            if (surface === 'review' && review) open(review.kind);
            else leave();
            return;
        }
        if (busy) {
            if (input === 'c') cancelPreparation();
            return;
        }
        if (input === 'w') {
            open('work');
            return;
        }
        if (small) return;
        if (surface === 'crawl') {
            if (key.tab) {
                setField((v) => (v + (key.shift ? 3 : 1)) % 4);
                return;
            }
            if (field === 0) {
                if (key.upArrow) setSource((v) => Math.max(0, v - 1));
                if (key.downArrow) setSource((v) => Math.min((options?.sources.length ?? 1) - 1, v + 1));
                if (input === ' ') {
                    const s = options?.sources[source];
                    if (s?.enabled)
                        setSelected((v) =>
                            v.includes(s.channel) ? v.filter((c) => c !== s.channel) : [...v, s.channel],
                        );
                }
            }
            if (key.return && field === 2) leave();
            if (
                key.return &&
                field === 3 &&
                selected.length &&
                (!limit || (BigInt(limit) >= 1n && BigInt(limit) <= 100n))
            ) {
                perform(() =>
                    prepare('work.review_crawl', {
                        channels: selected,
                        page_limit: limit ? BigInt(limit).toString() : null,
                    }),
                );
            }
            return;
        }
        if (surface === 'check') {
            if (key.tab || key.downArrow) setField((v) => (v + 1) % 4);
            if (key.upArrow) setField((v) => (v + 3) % 4);
            if (key.return) {
                if (field === 3) {
                    leave();
                    return;
                }
                if (!selection) return;
                const mode = (['focused', 'marked', 'matching'] as const)[field];
                if (!mode) return;
                const count = mode === 'focused' ? (selection.candidate_id ? '1' : '0') : selection[mode];
                if (count === '0') return;
                perform(() =>
                    prepare('work.review_check', {
                        view_id: selection.view_id,
                        selection: mode,
                        candidate_id: mode === 'focused' ? selection.candidate_id : null,
                    }),
                );
            }
            return;
        }
        if (surface === 'review') {
            if (key.tab) setField((v) => 1 - v);
            if (key.return && field === 0) {
                if (review) open(review.kind);
                return;
            }
            if (key.return && field === 1 && review) {
                const r = review;
                setReview(null);
                startPending.current = true;
                perform(async () => {
                    try {
                        const start = await rpc('work.start', { review_id: r.review_id }, wire.started);
                        setOperationID(start.operation_id);
                        setWork(null);
                        setSurface('work');
                    } finally {
                        startPending.current = false;
                        if (quitPending.current) {
                            quitPending.current = false;
                            returnSurface.current = 'work';
                            setSurface('quit');
                            setField(0);
                        }
                    }
                });
            }
        } else if (surface === 'work') {
            if (input === 'c') {
                cancelWork();
                return;
            }
            if (input === 'r' && (work?.run_id || unresolved?.run_id)) {
                openReport(work?.run_id ?? unresolved?.run_id ?? '');
                return;
            }
            if (input === 'a' && work && terminal(work))
                perform(async () => {
                    const ack = await rpc('work.acknowledge', { operation_id: work.operation_id }, wire.acknowledged);
                    setOperationID(null);
                    setWork(null);
                    setUnresolved(ack.unresolved_cleanup);
                    open(null);
                });
        } else if (surface === 'recent') {
            if (key.upArrow) setField((v) => Math.max(0, v - 1));
            if (key.downArrow || key.tab) setField((v) => (v + 1) % (recent.length + 1));
            if (key.return) {
                const r = recent[field];
                if (r) openReport(r.id);
                else {
                    setRunID('');
                    open('run_id');
                }
            }
            return;
        } else if (surface === 'run_id') {
            if (key.tab) setField((v) => (v + (key.shift ? 2 : 1)) % 3);
            if (key.return && field === 1 && /^[1-9][0-9]*$/.test(runID) && BigInt(runID) <= 9223372036854775807n)
                openReport(runID);
            if (key.return && field === 2) leave();
            return;
        } else if (surface === 'report' && report) {
            if (input === 'r') {
                const focus = currentFocus();
                perform(() => prepare('report.refresh', { report_id: report.report_id, focus }));
                return;
            }
            if (key.tab) {
                const sections =
                    report.run.kind === 'crawl'
                        ? ['summary', 'sources']
                        : report.legacy
                          ? ['summary', 'attempts']
                          : ['summary', 'items'];
                const next = sections[(sections.indexOf(reportFocus.section) + 1) % sections.length] ?? 'summary';
                const f = { ...summaryFocus(), section: next as wire.Focus['section'] };
                if (next === 'summary') {
                    setReportFocus(f);
                    setPage(null);
                    setScroll(0);
                } else perform(() => loadPage(f));
                return;
            }
            if (key.return && page) {
                const item = page.items[row];
                if (item && page.section === 'sources' && 'source_id' in item)
                    perform(() => loadPage({ ...summaryFocus(), section: 'pages', source_id: item.source_id }));
                if (item && page.section === 'items' && 'candidate_id' in item)
                    perform(() =>
                        loadPage({ ...summaryFocus(), section: 'attempts', candidate_id: item.candidate_id }),
                    );
                return;
            }
            if (key.leftArrow) {
                setRow((v) => Math.max(0, v - 1));
                setScroll(0);
                return;
            }
            if (key.rightArrow) {
                setRow((v) => Math.min((page?.items.length ?? 1) - 1, v + 1));
                setScroll(0);
                return;
            }
            if (key.pageDown && page?.next_offset) {
                perform(() => loadPage({ ...reportFocus, offset: page.next_offset ?? '0' }));
                return;
            }
            if (key.pageUp && page && page.offset !== '0') {
                perform(() =>
                    loadPage({
                        ...reportFocus,
                        offset: (BigInt(page.offset) > 50n ? BigInt(page.offset) - 50n : 0n).toString(),
                    }),
                );
                return;
            }
        }
        if (key.upArrow) setScroll((v) => Math.max(0, v - 1));
        if (key.downArrow) setScroll((v) => v + 1);
    });
    let lines: string[] = [],
        actions = 'Esc back | ? help | q quit';
    if (surface === 'crawl') {
        lines = [
            'Select configured sources. Disabled sources cannot run.',
            ...(selected.length === 0 ? ['Select at least one enabled source before review.'] : []),
            ...(options?.sources ?? []).map(
                (s, i) =>
                    `${field === 0 && source === i ? '>' : ' '} ${selected.includes(s.channel) ? '[x]' : '[ ]'} ${s.channel} | ${s.page_limit} pages${s.enabled ? '' : ' | Disabled'}`,
            ),
            `${field === 1 ? '>' : ' '} Common page limit: ${limit || 'Configured limits'}`,
            'Use 1 through 100, or leave the common page limit empty.',
        ];
        actions = `${field === 2 ? '>' : ' '} Back | ${field === 3 ? '>' : ' '} Review | Tab fields | Enter action`;
    } else if (surface === 'check') {
        lines = [
            'Choose exactly which candidates to check.',
            `Snapshot membership stays fixed until Start.`,
            ...['Focused', 'Marked', 'Matching', 'Back'].map(
                (name, i) =>
                    `${field === i ? '>' : ' '} ${name}${i === 0 ? ` (${selection?.candidate_id ? '1' : '0'})` : i === 1 ? ` (${selection?.marked ?? '0'})` : i === 2 ? ` (${selection?.matching ?? '0'})` : ''}`,
            ),
        ];
        actions = 'Up/Down or Tab choice | Enter review | Esc back';
    } else if (surface === 'review' && review) {
        lines = [
            `Collection ${hello.profile.state_dir}`,
            review.collection_state === 'absent'
                ? 'Start may initialize this collection.'
                : 'Start may recover interrupted saved records.',
            ...(review.kind === 'crawl'
                ? [
                      'Crawl only. Validation will not start.',
                      `Maximum page budget ${review.page_budget}, sources may end earlier.`,
                      ...(review.sources ?? []).map((s) => `${s.channel}: ${s.page_limit} pages`),
                  ]
                : [
                      `Selection ${review.selection} | ${review.selected_count} candidates`,
                      ...(review.candidate_id === null ? [] : [`Candidate ${review.candidate_id}`]),
                      `Results observed ${review.as_of?.local}`,
                      `Docker configured, not yet contacted.`,
                      `Image ${review.image?.text}`,
                      `Exact candidate identities are compared again under writer ownership.`,
                      `Validator recovery may run before new checks.`,
                      ...fields(review.filters, 'Filters'),
                      ...policyLines(review.policy),
                  ]),
        ];
        actions = `${field === 0 ? '>' : ' '} Back | ${field === 1 ? '>' : ' '} Start ${review.kind} | Tab | Enter`;
    } else if (surface === 'work') {
        lines = work
            ? [
                  `${work.kind} | ${work.state} | ${work.run_id ? `Run ${work.run_id}` : work.run_created === 'false' ? 'Not yet saved' : 'Save outcome unknown'}`,
                  `Saved status ${work.run_status ?? 'Unavailable'} | Cleanup ${work.cleanup}`,
                  `Persistence ${work.persistence} | Cancel requested ${work.cancellation_requested}`,
                  `Observed ${work.observed_at.local} | ${now - received > 3000 ? 'Delayed' : 'Current'}`,
                  `Elapsed ${(BigInt(work.elapsed_ns) / 1000000000n).toString()} seconds`,
                  ...progressLines(work),
                  ...(work.guidance.action === 'stop_service'
                      ? [
                            `Service ownership observed ${work.guidance.service_observed_at?.local}.`,
                            'You can run wpc service stop outside this UI.',
                        ]
                      : []),
                  ...(work.guidance.retry_at ? [`Retry time ${work.guidance.retry_at.local}`] : []),
                  work.error_code ? explanation(work.error_code) : '',
              ]
            : ['No operation in this session.'];
        actions =
            work && terminal(work)
                ? 'a Acknowledge | r Saved report | Esc back'
                : 'c Cancel work | r Saved report | Esc back';
    } else if (surface === 'recent') {
        lines = [
            ...recent.map((r, i) => `${field === i ? '>' : ' '} Run ${r.id} | ${r.kind} | ${r.status}`),
            `${field === recent.length ? '>' : ' '} Open run ID`,
        ];
        actions = 'Up/Down choose | Enter open | Esc back';
    } else if (surface === 'run_id') {
        lines = [`${field === 0 ? '>' : ' '} Saved run ID: ${runID}`];
        actions = `${field === 1 ? '>' : ' '} Open | ${field === 2 ? '>' : ' '} Back | Tab | Enter`;
    } else if (surface === 'report' && report) {
        lines = [
            `Run ${report.run.id} | ${report.run.kind} | ${report.run.status}`,
            `Observed ${report.as_of.local} | ${report.retained ? 'Retained after refresh failure' : 'Fixed snapshot'}`,
            `Section ${reportFocus.section}`,
            ...(reportFocus.section === 'summary'
                ? reportSummary(report)
                : page?.items[row]
                  ? fields(page.items[row])
                  : ['No records']),
        ];
        actions = 'Tab sections | Enter details | Left/Right row | PgUp/PgDn';
    } else if (surface === 'quit') {
        lines = [
            'Leaving cancels owned work and waits for bounded cleanup.',
            ...(channel.preparation ? ['Preparation is also cancelled. A file may already have been published.'] : []),
        ];
        actions = `${field === 0 ? '>' : ' '} Stay | ${field === 1 ? '>' : ' '} Cancel work and quit | Tab | Enter`;
    }
    if (help) {
        lines = [
            'Keyboard help',
            'Ctrl+X cancels work. Ctrl+G cancels preparation.',
            'These actions are independent and work in text fields.',
            'Tab moves fields or actions. Enter activates a focused action.',
            'Escape leaves a screen, without cancelling active work.',
            'Up and Down scroll details. Reports use Left and Right for records.',
            'Page Up and Page Down browse complete report pages.',
            'w opens Work. q offers quit while work is active.',
            'Ctrl+C interrupts immediately.',
            'Keys are never revealed on work or report screens.',
        ];
        actions = '? or Esc close help';
    }
    if (small && !help && surface !== 'quit') {
        lines = ['Resize to at least 60 columns and 16 rows.'];
        actions = '? help | q quit | Ctrl+C interrupt';
    }
    if (busy) lines.unshift(`Preparing: ${message || 'Waiting for parent'}`);
    const wrapped = linesAtWidth(lines, width),
        position = Math.min(scroll, Math.max(0, wrapped.length - height));
    return (
        <>
            {children(api)}
            {surface !== null && (
                <Box flexDirection="column" width={width} height={Math.max(1, rows)}>
                    <Text wrap="truncate-end">{safe(`WPC | ${surface} | ${api.indicator}`)}</Text>
                    <Text wrap="truncate-end">{safe(hello.profile.state_dir)}</Text>
                    <Text wrap="truncate-end">{safe(message)}</Text>
                    <Box flexDirection="column" height={height} overflow="hidden">
                        {wrapped.slice(position, position + height).map((line, i) => (
                            <Text
                                // biome-ignore lint/suspicious/noArrayIndexKey: Stateless terminal row slots retain no record state.
                                key={`${position + i}:${line}`}
                                color={hello.color !== 'never' && line.startsWith('>') ? theme.accent : undefined}
                            >
                                {line}
                            </Text>
                        ))}
                    </Box>
                    <Text wrap="truncate-end">{actions}</Text>
                    <Text>Ctrl+X Cancel work | Ctrl+G Cancel preparation</Text>
                    <Text wrap="truncate-end">
                        {surface === 'report'
                            ? 'Up/Down scroll | r refresh | Esc back | ? help | q quit'
                            : '? help | Esc back | q quit | Ctrl+C interrupt'}
                    </Text>
                    <Text wrap="truncate-end">
                        {unresolved ? `Cleanup unresolved, run ${unresolved.run_id ?? 'unknown'}` : ''}
                    </Text>
                </Box>
            )}
        </>
    );
}
