import { Box, Text, useInput, useWindowSize } from 'ink';
import { useEffect, useRef, useState } from 'react';
import type { Channel } from './channel.js';
import * as wire from './results-schema.js';
import type { Hello, Schema, Stamp } from './schema.js';
import { invalid } from './schema.js';
import { safe, theme } from './theme.js';

type Props = {
    channel: Channel;
    hello: Hello;
    active: boolean;
    onOverview: () => void;
    quit: (code: number, interrupt?: boolean) => void;
};
type Surface = 'results' | 'filters' | 'details' | 'export' | 'review' | 'outcome';
const time = (v: Stamp | null) => (v ? `${v.local} (${v.utc})` : 'Unavailable');
const display = (v: { text: string; shortened: boolean } | null) =>
    v ? v.text + (v.shortened ? ' [shortened]' : '') : 'Unavailable';
const explain = (code: string) =>
    (
        ({
            state_not_initialized: 'No saved collection yet. Return to Overview for collection commands.',
            state_migration_required: 'Saved state needs migration through an explicit command.',
            state_schema_too_new: 'Saved state needs a newer WPC version.',
            state_error: 'Saved state is unavailable or cannot be read.',
            ui_key_too_large: 'Key is too large to reveal. Use exact file export from Results.',
            ui_export_selection_empty: 'No marked candidates. Mark candidates or choose matching.',
            ui_collection_changed: 'Saved collection changed. Refresh Results before opening details.',
            ui_snapshot_cleanup_failed: 'Temporary cleanup failed. New preparation waits for cleanup.',
            ui_handle_stale: 'This handle is no longer current. Return to Results.',
            export_exists: 'The destination already exists. Choose another path.',
            export_destination_invalid: 'The destination is unavailable or unsafe. Choose another path.',
            export_key_unrepresentable: 'A selected key cannot be represented as one exact line. No file was created.',
        }) as Record<string, string>
    )[code] ?? code;
const filterLabel = (f: wire.Filters) =>
    Object.entries(f)
        .map(([k, v]) => `${k}: ${v.join(', ') || 'Any'}`)
        .join(' | ');
// biome-ignore lint/style/noNonNullAssertion: A regex match always contains at least one code point.
const escapeCells = (s: string) => safe(s).replace(/[^\x20-\x7e]/gu, (c) => `\\u{${c.codePointAt(0)!.toString(16)}}`);
const wrap = (s: string, width: number) => {
    const text = escapeCells(s),
        out: string[] = [];
    for (let i = 0; i < text.length; i += width) out.push(text.slice(i, i + width));
    return out.length ? out : [''];
};
function summaryLines(s: wire.Summary) {
    return [
        `Candidate ${s.id}`,
        `Latest ${s.latest_outcome} | Reason ${s.latest_reason ?? 'None'}`,
        `Last definitive ${s.last_definitive ?? 'None'} | Freshness ${s.freshness}`,
        `Evidence ${time(s.evidence_at)} | Clock anomaly ${s.clock_anomaly ? 'Yes' : 'No'}`,
        `First discovered ${time(s.first_seen_at)}`,
        `Last discovered ${time(s.last_seen_at)}`,
        `Latest attempt ${s.latest_attempt_id ?? 'None'} | Definitive attempt ${s.definitive_attempt_id ?? 'None'}`,
        `Active attempt ${s.active_attempt_id ?? 'None'}`,
    ];
}
function historyLines(row: wire.SourceRow | wire.AttemptRow): string[] {
    if ('source_id' in row)
        return [
            `Sighting ${row.id} | Candidate ${row.candidate_id}`,
            `Source ${display(row.source_id)} | Message ${display(row.message_id)}`,
            `URL ${display(row.message_url)}`,
            `Published ${time(row.published_at)}`,
            `First seen ${time(row.first_seen_at)}`,
            `Last seen ${time(row.last_seen_at)}`,
        ];
    const a = row;
    const out = [
        `Attempt ${a.id} | Candidate ${a.candidate_id} | Run ${a.run_id}`,
        `Outcome ${a.outcome ?? 'Unresolved'} | Reason ${a.reason_code ?? 'None'}`,
        `Backend ${display(a.backend_id)} | Version ${display(a.backend_version)}`,
        `Started ${time(a.started_at)}`,
        `Finished ${time(a.finished_at)}`,
        `Recovered ${time(a.recovered_at)}`,
        `Clock anomaly ${a.clock_anomaly ? 'Yes' : 'No'}`,
    ];
    if (a.evidence)
        out.push(
            `Evidence ${a.evidence.kind} | Observed ${time(a.evidence.observed_at)}`,
            display(a.evidence.account_type ?? a.evidence.rejection_code),
        );
    else out.push('No definitive evidence');
    if (a.retry)
        out.push(
            `Attempt number ${a.retry.attempt_number} | Failure class ${a.retry.failure_class ?? 'None'}`,
            `Retry wait ${a.retry.wait_ns} ns | Eligible ${time(a.retry.eligible_at)}`,
        );
    else out.push('Retry facts unavailable');
    return out;
}

export function ResultsScreen({ channel, hello, active, onOverview, quit }: Props) {
    const { columns, rows } = useWindowSize();
    const width = Math.max(1, columns),
        height = Math.max(1, rows - 7);
    const small = columns < theme.minimumColumns || rows < theme.minimumRows;
    const viewport = Math.max(1, Math.floor(height / (columns >= theme.tableColumns ? 1 : 3)));
    const [surface, setSurface] = useState<Surface>('results');
    const [view, setView] = useState<wire.View | null>(null);
    const viewRef = useRef(view);
    viewRef.current = view;
    const [page, setPage] = useState<wire.ResultPage | null>(null);
    const pageRef = useRef(page);
    pageRef.current = page;
    const [focus, setFocus] = useState(0n);
    const focusRef = useRef(focus);
    focusRef.current = focus;
    const retryPosition = useRef<bigint | null>(null);
    const [busy, setBusy] = useState(false);
    const entered = useRef(false);
    const [job, setJob] = useState<wire.Job | null>(null);
    const cancelWanted = useRef(false);
    const [message, setMessage] = useState('');
    const [help, setHelp] = useState(false);
    const helpReturnScroll = useRef(0);
    const [scroll, setScroll] = useState(0);
    const [draft, setDraft] = useState(wire.emptyFilters);
    const [group, setGroup] = useState(0);
    const [option, setOption] = useState(0);
    const [detail, setDetail] = useState<wire.Detail | null>(null);
    const detailRef = useRef(detail);
    detailRef.current = detail;
    const [detailSummary, setDetailSummary] = useState<wire.Summary | null>(null);
    const [section, setSection] = useState(0);
    const [history, setHistory] = useState<wire.DetailPage | null>(null);
    const [historyFocus, setHistoryFocus] = useState(0n);
    const [historyUnavailable, setHistoryUnavailable] = useState(false);
    const sectionPositions = useRef([0n, 0n, 0n]);
    const sectionScroll = useRef([0, 0, 0]);
    const [revealedKey, setRevealedKey] = useState<string | null>(null);
    const revealGeneration = useRef(0);
    const revealPending = useRef(false);
    const pendingDetailAction = useRef<(() => void) | null>(null);
    const [selection, setSelection] = useState<'matching' | 'marked'>('matching');
    const [format, setFormat] = useState<'json' | 'keys'>('json');
    const [includeKeys, setIncludeKeys] = useState(false);
    const [path, setPath] = useState('');
    const [field, setField] = useState(0);
    const [review, setReview] = useState<wire.Review | null>(null);
    const [createFocused, setCreateFocused] = useState(false);
    const [receipt, setReceipt] = useState<wire.Receipt | null>(null);
    const desiredPosition = useRef(0n);
    const pendingPosition = useRef<bigint | null>(null);
    const mounted = useRef(true);
    const navigateRef = useRef<(n: bigint) => void>(() => {});
    const clearKey = () => {
        revealGeneration.current++;
        setRevealedKey(null);
    };
    useEffect(
        () => () => {
            mounted.current = false;
            revealGeneration.current++;
        },
        [],
    );
    useEffect(() => {
        if (small || !active) {
            revealGeneration.current++;
            setRevealedKey(null);
        }
    }, [small, active]);

    const rpc = async <T,>(method: string, params: unknown, parse: Schema<T>) =>
        wire.operation(await channel.request(method, params), parse);
    const perform = (work: () => Promise<void>) => {
        if (entered.current) return;
        entered.current = true;
        setBusy(true);
        setMessage('');
        void work()
            .catch((e) => {
                if (!mounted.current) return;
                if (e instanceof wire.OperationError)
                    setMessage(`${explain(e.message)}${retryPosition.current !== null ? ' t retries the page.' : ''}`);
                else quit(1);
            })
            .finally(() => {
                entered.current = false;
                if (!mounted.current) return;
                const detailAction = pendingDetailAction.current;
                pendingDetailAction.current = null;
                if (detailAction && channel.connected) {
                    detailAction();
                    return;
                }
                setBusy(false);
                const next = pendingPosition.current;
                pendingPosition.current = null;
                if (next !== null) navigateRef.current(next);
            });
    };
    const loadPage = async (v: wire.View, position: bigint) => {
        const max = BigInt(v.matching_count);
        const target = max === 0n ? 0n : position < 0n ? 0n : position >= max ? max - 1n : position;
        let p: wire.ResultPage;
        try {
            p = await rpc('results.page', { view_id: v.view_id, offset: target.toString() }, wire.resultPage);
        } catch (e) {
            retryPosition.current = target;
            throw e;
        }
        retryPosition.current = null;
        if (p.view.view_id !== v.view_id || p.offset !== target.toString()) invalid();
        setView(p.view);
        viewRef.current = p.view;
        setPage(p);
        pageRef.current = p;
        setFocus(target);
        focusRef.current = target;
        desiredPosition.current = target;
    };
    const installView = async (v: wire.View, move: boolean) => {
        setView(v);
        viewRef.current = v;
        const target = move ? BigInt(v.focus_offset ?? '0') : focusRef.current;
        await loadPage(v, target);
    };
    const loadHistory = async (d: wire.Detail, tab: number, position: bigint) => {
        if (tab === 0) {
            setHistory(null);
            return;
        }
        const total = BigInt(tab === 1 ? d.source_count : d.attempt_count);
        const target = total === 0n ? 0n : position < 0n ? 0n : position >= total ? total - 1n : position;
        let p: wire.DetailPage;
        try {
            p = await rpc(
                'details.page',
                { detail_id: d.detail_id, section: tab === 1 ? 'sources' : 'attempts', offset: target.toString() },
                wire.detailPage,
            );
        } catch (e) {
            setHistoryUnavailable(true);
            throw e;
        }
        setHistoryUnavailable(false);
        if (
            p.detail_id !== d.detail_id ||
            p.section !== (tab === 1 ? 'sources' : 'attempts') ||
            p.offset !== target.toString() ||
            p.total !== total.toString() ||
            p.items.some((r) => r.candidate_id !== d.candidate_id)
        )
            invalid();
        setHistory(p);
        setHistoryFocus(target);
        sectionPositions.current[tab] = target;
    };
    const runJob = async (method: string, params: unknown) => {
        cancelWanted.current = false;
        setJob(null);
        const started = await rpc(method, params, wire.started);
        let first = true;
        while (mounted.current && channel.connected) {
            const cancel = !first && cancelWanted.current;
            if (cancel) cancelWanted.current = false;
            first = false;
            const j = await rpc(cancel ? 'job.cancel' : 'job.poll', { job_id: started.job_id }, wire.job);
            if (j.job_id !== started.job_id) invalid();
            setJob(j);
            if (j.state === 'running' || j.state === 'cancelling') {
                await new Promise<void>((resolve) => setTimeout(resolve, 250));
                continue;
            }
            if (j.result?.type === 'view') {
                await installView(
                    j.result.value,
                    j.kind !== 'clear_marks' &&
                        !(j.kind === 'filter' && viewRef.current?.view_id === j.result.value.view_id),
                );
                setSurface('results');
                if (j.result.value.removed_marks !== '0')
                    setMessage(`${j.result.value.removed_marks} marks removed after refresh`);
            } else if (j.result?.type === 'detail') {
                const d = j.result.value;
                if (d.candidate_id !== d.summary.id) invalid();
                setDetail(d);
                detailRef.current = d;
                setDetailSummary(d.summary);
                setSurface('details');
                setSection(0);
                setScroll(0);
            } else if (j.result?.type === 'review') {
                setReview(j.result.value);
                setCreateFocused(false);
                setSurface('review');
                setScroll(0);
            } else if (j.result?.type === 'export') {
                setReceipt(j.result.value);
                setSurface('outcome');
                setScroll(0);
            }
            if (j.warning_code) setMessage(`${j.warning_code}: ${j.remaining_path ?? ''}`);
            if (j.state !== 'succeeded') {
                setMessage(`${explain(j.error_code ?? 'failed')} (${j.phase})`);
                if (j.kind === 'refresh' && viewRef.current) {
                    // Fetch the retained marker without moving the local position.
                    await loadPage(viewRef.current, focusRef.current);
                }
            }
            return;
        }
    };
    // biome-ignore lint/correctness/useExhaustiveDependencies: Entry runs only when active changes; render-local jobs must not restart it.
    useEffect(() => {
        if (!active) return;
        if (viewRef.current === null) perform(() => runJob('results.open', {}));
        else {
            setSurface('results');
            setHelp(false);
        }
    }, [active]);
    const focused = page?.items.find((_, i) => BigInt(page.offset) + BigInt(i) === focus);
    const navigate = (position: bigint) => {
        desiredPosition.current = position;
        if (entered.current) {
            pendingPosition.current = position;
            return;
        }
        const v = viewRef.current;
        if (!v) return;
        const count = BigInt(v.matching_count),
            target = count === 0n ? 0n : position < 0n ? 0n : position >= count ? count - 1n : position;
        const p = pageRef.current;
        if (p && target >= BigInt(p.offset) && target + BigInt(viewport) <= BigInt(p.offset) + BigInt(p.items.length)) {
            setFocus(target);
            focusRef.current = target;
            desiredPosition.current = target;
        } else perform(() => loadPage(v, target));
    };
    navigateRef.current = navigate;
    const openDetail = () => {
        if (!view || !focused) return;
        clearKey();
        setDetail(null);
        detailRef.current = null;
        setDetailSummary(focused.summary);
        setHistory(null);
        setSection(0);
        setScroll(0);
        setSurface('details');
        sectionPositions.current = [0n, 0n, 0n];
        sectionScroll.current = [0, 0, 0];
        perform(() => runJob('details.open', { view_id: view.view_id, candidate_id: focused.summary.id }));
    };
    const leaveDetails = () => {
        clearKey();
        perform(async () => {
            try {
                if (detail) await rpc('details.close', { detail_id: detail.detail_id }, wire.empty);
            } finally {
                setDetail(null);
                detailRef.current = null;
                setHistory(null);
                setSurface('results');
                setScroll(0);
            }
        });
    };
    const groups = Object.keys(wire.filterValues) as (keyof wire.Filters)[];
    const helpLines = [
        'Keyboard help',
        'Results: Up/Down, Page Up/Page Down, Home/End navigate',
        'Enter details | Space mark | u clear marks | f filters | e export',
        'r refresh | Esc overview | ? help | q quit | Ctrl+C interrupt',
        'Details: Tab sections, arrows scroll, Page Up/Down history, Home/End limits',
        'k reveal or hide | r refresh details | Esc results',
        'Filters: Tab groups, arrows choose, Space toggle, c clear, Enter apply',
        'Export: Tab fields, Space choices, type destination, Enter focused action',
        'Review: Tab Back/Create file, Enter activates focused action',
        'During preparation: c or Esc cancels, q quits. Help and resize stay available.',
        'Results remain fixed until refresh. Details use a separate newer observation.',
        'Export always uses Results. Revealing a key does not enable export disclosure.',
        'Keys are displayed as quoted text. Exact bytes are available through file export.',
        '? or Esc closes help. Up/Down scroll help.',
    ];

    useInput((input, key) => {
        if (!active) return;
        if (key.ctrl && input === 'c') {
            clearKey();
            quit(130, true);
            return;
        }
        const textInput = surface === 'export' && field === 3 && !help && !small && !busy;
        if (textInput) {
            if (key.escape) {
                setSurface('results');
                return;
            }
            if (key.tab) {
                setField(key.shift ? 2 : 4);
                return;
            }
            if (key.return) return;
            if (key.backspace || key.delete) {
                setPath((p) => Array.from(p).slice(0, -1).join(''));
                return;
            }
            if (!key.ctrl && !key.meta && input) {
                setPath((p) => p + input);
                return;
            }
            return;
        }
        if (input === 'q') {
            clearKey();
            quit(0);
            return;
        }
        if (input === '?') {
            clearKey();
            if (help) {
                setHelp(false);
                setScroll(helpReturnScroll.current);
            } else {
                helpReturnScroll.current = scroll;
                setHelp(true);
                setScroll(0);
            }
            return;
        }
        if (help) {
            if (key.escape) {
                setHelp(false);
                setScroll(helpReturnScroll.current);
            } else if (key.upArrow || key.pageUp) setScroll((n) => Math.max(0, n - (key.pageUp ? height : 1)));
            else if (key.downArrow || key.pageDown) setScroll((n) => n + (key.pageDown ? height : 1));
            return;
        }
        if (busy) {
            if (revealPending.current && (key.escape || input === 'c' || input === 'k' || (input === 'r' && !small))) {
                clearKey();
                if (key.escape) pendingDetailAction.current = leaveDetails;
                else if (input === 'r') pendingDetailAction.current = openDetail;
                setMessage('Cancellation requested');
                return;
            }
            if (input === 'c' || key.escape) {
                cancelWanted.current = true;
                setMessage('Cancellation requested');
            } else if (surface === 'results' && !job?.state.match(/running|cancelling/) && view) {
                const base = pendingPosition.current ?? desiredPosition.current;
                if (key.upArrow || key.pageUp) pendingPosition.current = base - BigInt(key.pageUp ? viewport : 1);
                else if (key.downArrow || key.pageDown)
                    pendingPosition.current = base + BigInt(key.pageDown ? viewport : 1);
                else if (key.home) pendingPosition.current = 0n;
                else if (key.end) pendingPosition.current = BigInt(view.matching_count) - 1n;
            }
            return;
        }
        if (small) return;
        if (surface === 'results') {
            if (key.escape) {
                clearKey();
                setJob(null);
                onOverview();
                return;
            }
            if (input === 'r') {
                clearKey();
                perform(() =>
                    runJob(
                        view ? 'results.refresh' : 'results.open',
                        view ? { view_id: view.view_id, focus_id: focused?.summary.id ?? null } : {},
                    ),
                );
                return;
            }
            if (!view) return;
            if (input === 't' && retryPosition.current !== null) {
                // biome-ignore lint/style/noNonNullAssertion: The retry branch checks this ref and perform invokes the callback synchronously.
                perform(() => loadPage(view, retryPosition.current!));
                return;
            }
            if (key.upArrow || key.pageUp) navigate(focus - BigInt(key.pageUp ? viewport : 1));
            else if (key.downArrow || key.pageDown) navigate(focus + BigInt(key.pageDown ? viewport : 1));
            else if (key.home) navigate(0n);
            else if (key.end) navigate(BigInt(view.matching_count) - 1n);
            else if (key.return) openDetail();
            else if (input === ' ' && focused)
                perform(async () => {
                    const m = await rpc(
                        'results.mark',
                        { view_id: view.view_id, candidate_id: focused.summary.id, marked: !focused.marked },
                        wire.marks,
                    );
                    if (m.view_id !== view.view_id || m.candidate_id !== focused.summary.id) invalid();
                    await loadPage(view, focus);
                });
            else if (input === 'u') perform(() => runJob('results.clear_marks', { view_id: view.view_id }));
            else if (input === 'f') {
                setDraft(structuredClone(view.filters));
                setGroup(0);
                setOption(0);
                setSurface('filters');
                setScroll(0);
            } else if (input === 'e') {
                setSelection('matching');
                setFormat('json');
                setIncludeKeys(false);
                setPath('');
                setField(0);
                setSurface('export');
                setReview(null);
                setReceipt(null);
                setScroll(0);
            }
        } else if (surface === 'filters') {
            // biome-ignore lint/style/noNonNullAssertion: Keyboard navigation keeps group within the fixed groups array.
            const name = groups[group]!;
            const values = wire.filterValues[name];
            if (key.escape) setSurface('results');
            else if (key.tab) {
                setGroup((group + (key.shift ? 2 : 1)) % 3);
                setOption(0);
            } else if (key.upArrow) setOption((n) => Math.max(0, n - 1));
            else if (key.downArrow) setOption((n) => Math.min(values.length - 1, n + 1));
            else if (input === 'c') setDraft(wire.emptyFilters());
            else if (input === ' ')
                setDraft((d) => ({
                    ...d,
                    [name]: values.filter((v) => (v === values[option] ? !d[name].includes(v) : d[name].includes(v))),
                }));
            else if (key.return && view)
                perform(() => runJob('results.filter', { view_id: view.view_id, filters: draft }));
        } else if (surface === 'details') {
            if (key.escape) {
                leaveDetails();
                return;
            }
            if (input === 'r') {
                openDetail();
                return;
            }
            if (key.tab && detail) {
                const next = (section + (key.shift ? 2 : 1)) % 3;
                sectionScroll.current[section] = scroll;
                setSection(next);
                setHistory(null);
                setHistoryUnavailable(false);
                setScroll(sectionScroll.current[next] ?? 0);
                perform(() => loadHistory(detail, next, sectionPositions.current[next] ?? 0n));
                return;
            }
            if (input === 'k' && detail) {
                if (revealedKey !== null) {
                    clearKey();
                    return;
                }
                const generation = ++revealGeneration.current,
                    d = detail;
                perform(async () => {
                    revealPending.current = true;
                    try {
                        const r = await rpc(
                            'candidate.reveal',
                            { view_id: d.view_id, detail_id: d.detail_id, candidate_id: d.candidate_id },
                            wire.revealed,
                        );
                        if (r.view_id !== d.view_id || r.detail_id !== d.detail_id || r.candidate_id !== d.candidate_id)
                            invalid();
                        if (generation === revealGeneration.current && detailRef.current?.detail_id === d.detail_id)
                            setRevealedKey(r.key);
                    } finally {
                        revealPending.current = false;
                    }
                });
                return;
            }
            if (section !== 0 && detail && (key.pageUp || key.pageDown || key.home || key.end)) {
                const total = BigInt(section === 1 ? detail.source_count : detail.attempt_count);
                const target = key.home ? 0n : key.end ? total - 1n : historyFocus + (key.pageUp ? -1n : 1n);
                setScroll(0);
                perform(() => loadHistory(detail, section, target));
                return;
            }
            if (key.home) setScroll(0);
            else if (key.end) setScroll(Number.MAX_SAFE_INTEGER);
            else if (key.upArrow) setScroll((n) => Math.max(0, n - 1));
            else if (key.downArrow) setScroll((n) => n + 1);
        } else if (surface === 'export') {
            if (key.escape) {
                setSurface('results');
                return;
            }
            if (key.tab) {
                setField((field + (key.shift ? 5 : 1)) % 6);
                return;
            }
            if (input === ' ' || key.leftArrow || key.rightArrow) {
                if (field === 0) setSelection((v) => (v === 'matching' ? 'marked' : 'matching'));
                if (field === 1) {
                    setFormat((f) => (f === 'json' ? 'keys' : 'json'));
                    setIncludeKeys(format === 'json');
                }
                if (field === 2 && format === 'json') setIncludeKeys((v) => !v);
            }
            if (key.return && field === 5) setSurface('results');
            if (key.return && field === 4 && view) {
                const params = { view_id: view.view_id, selection, format, include_keys: includeKeys, path };
                if (
                    Buffer.byteLength(
                        `${JSON.stringify({ version: 2, id: '9'.repeat(128), method: 'export.review', params })}\n`,
                    ) > 4096
                ) {
                    setMessage('Path is too long for this interface');
                    return;
                }
                perform(() => runJob('export.review', params));
            }
        } else if (surface === 'review' && review) {
            const back = () =>
                perform(async () => {
                    await rpc('export.discard', { review_id: review.review_id }, wire.empty);
                    setReview(null);
                    setSurface('export');
                });
            if (key.escape) {
                back();
                return;
            }
            if (key.tab || key.leftArrow || key.rightArrow) setCreateFocused((v) => !v);
            if (key.upArrow) setScroll((n) => Math.max(0, n - 1));
            else if (key.downArrow) setScroll((n) => n + 1);
            if (key.return) {
                if (!createFocused) back();
                else perform(() => runJob('export.start', { review_id: review.review_id }));
            }
        } else if (surface === 'outcome') {
            if (key.return || key.escape) {
                setSurface('results');
                setJob(null);
                setScroll(0);
            } else if (key.upArrow) setScroll((n) => Math.max(0, n - 1));
            else if (key.downArrow) setScroll((n) => n + 1);
        }
    });

    let content: string[] = [];
    let footer = '';
    let range = '';
    if (surface === 'results') {
        footer = 'Enter details | Space mark | f filters | e export | r refresh | Esc overview';
        if (!view) content = [busy || !message ? 'Preparing saved collection...' : message];
        else if (view.matching_count === '0')
            content = ['No matching candidates. f changes filters, r refreshes saved data.'];
        else if (page) {
            const start = Number(focus - BigInt(page.offset));
            const visible = page.items.slice(Math.max(0, start), Math.max(0, start) + viewport);
            content = visible.flatMap((r, i) =>
                columns >= theme.tableColumns
                    ? [
                          `${i === 0 ? '>' : ' '} ${r.marked ? '[x]' : '[ ]'} ${r.summary.id.padEnd(19)} ${r.summary.latest_outcome.padEnd(12)} ${(
                              r.summary.last_definitive ?? 'none'
                          ).padEnd(9)} ${r.summary.evidence_at?.local ?? 'No evidence'} ${r.summary.freshness}`,
                      ]
                    : [
                          `${i === 0 ? '>' : ' '} ${r.marked ? '[x]' : '[ ]'} Candidate ${r.summary.id} | ${r.summary.latest_outcome}`,
                          `  Definitive ${r.summary.last_definitive ?? 'none'} | ${r.summary.freshness}`,
                          `  Evidence ${r.summary.evidence_at?.local ?? 'Unavailable'}`,
                      ],
            );
            range = `Rows ${focus + 1n} to ${focus + BigInt(visible.length)} of ${view.matching_count}`;
        }
    } else if (surface === 'filters') {
        content = [
            'Applying changed filters clears marks.',
            ...groups.flatMap((g, i) => [
                `${g} (${draft[g].join(', ') || 'Any'})`,
                ...wire.filterValues[g].map(
                    (v, n) => `${group === i && option === n ? '>' : ' '} [${draft[g].includes(v) ? 'x' : ' '}] ${v}`,
                ),
            ]),
        ];
        footer = 'Tab group | Up/Down choice | Space toggle | c clear | Enter apply | Esc cancel';
    } else if (surface === 'details') {
        content = [
            `Results as of ${time(view?.as_of ?? null)}`,
            `Details as of ${time(detail?.as_of ?? null)}`,
            `${section === 0 ? '>' : ''} Summary | ${section === 1 ? '>' : ''} Sources ${detail?.source_count ?? 'Unavailable'} | ${
                section === 2 ? '>' : ''
            } Attempts ${detail?.attempt_count ?? 'Unavailable'}`,
        ];
        if (!detail)
            content.push(
                'Detail history unavailable or not loaded. r retries.',
                ...(detailSummary ? summaryLines(detailSummary) : []),
            );
        else if (section === 0)
            content.push(
                ...summaryLines(detail.summary),
                revealedKey === null ? 'Key hidden. k reveals this candidate.' : `Key ${JSON.stringify(revealedKey)}`,
            );
        else {
            const item = history?.items[0];
            content.push(
                ...(item
                    ? historyLines(item)
                    : [
                          busy
                              ? 'Loading history...'
                              : historyUnavailable
                                ? 'History unavailable. Tab returns to this section to retry.'
                                : 'No saved records.',
                      ]),
            );
            range = `${section === 1 ? 'Source' : 'Attempt'} ${item ? historyFocus + 1n : 0n} of ${section === 1 ? detail.source_count : detail.attempt_count}`;
        }
        footer = 'Tab sections | Up/Down scroll | PgUp/PgDn records | k key | r refresh | Esc results';
    } else if (surface === 'export') {
        const fields = [
            `Selection: ${selection}`,
            `Format: ${format}`,
            format === 'keys' ? 'Contains complete keys' : `[${includeKeys ? 'x' : ' '}] Include keys`,
            `Destination: ${path.length > width - 17 ? `(end) ${path.slice(-(width - 23))}` : path || '(enter a file path)'}`,
            'Review export',
            'Back',
        ];
        content = [
            'Export the fixed Results snapshot.',
            ...fields.map((s, i) => `${field === i ? '>' : ' '} ${s}`),
            'Relative paths use the invocation directory. No shell expansion.',
            'Existing files are never replaced.',
        ];
        footer = 'Tab field | Space choice | Enter focused action | Esc back';
    } else if (surface === 'review' && review) {
        content = [
            `Selection ${review.selection} | ${review.selected_count} selected of ${review.matching_count} matching`,
            `Filters ${filterLabel(review.filters)}`,
            `Results as of ${time(review.as_of)} | Max age ${review.max_age_ns} ns`,
            ...(review.retained ? ['Retained after refresh failure'] : []),
            `Format ${review.format} | Keys ${review.include_keys ? 'included' : 'hidden'}`,
            `Destination ${review.destination}`,
            'Created privately. An existing path is never replaced.',
            'Interruption during publication may leave a complete file. Check the destination before retrying.',
        ];
        footer = `${createFocused ? ' ' : '>'} Back | ${createFocused ? '>' : ' '} Create file | Tab change | Enter | Esc`;
    } else if (surface === 'outcome' && receipt) {
        content = [
            receipt.published === true
                ? 'File created'
                : receipt.published === false
                  ? 'This operation did not create a file'
                  : 'File may have been created',
            `Destination ${receipt.destination}`,
            `${receipt.prepared_count} prepared of ${receipt.selected_count} selected`,
            `Format ${receipt.format} | Keys ${receipt.include_keys ? 'included' : 'hidden'} | Durability ${receipt.durability}`,
            ...(receipt.error_code ? [`Result ${receipt.error_code}`] : ['Export complete.']),
            ...(receipt.remaining_path ? [`Cleanup needed: ${receipt.remaining_path}`] : []),
            'A new export requires another review. Existing paths are never replaced.',
        ];
        footer = 'Up/Down scroll | Enter or Esc acknowledges and returns to Results';
    }
    if (help) {
        content = helpLines;
        footer = 'Up/Down scroll | ? or Esc close help';
    }
    if (small && !help) {
        content = ['Resize to at least 60 columns and 16 rows.'];
        footer = 'c cancel | ? help | q quit';
    }
    const wrapped = content.flatMap((s) => wrap(s, width));
    const maximum = Math.max(0, wrapped.length - height);
    let position = Math.min(scroll, maximum);
    if ((surface === 'filters' || surface === 'export') && !help) {
        const selected = content.findIndex((line) => line.startsWith('>'));
        const selectedLine = content
            .slice(0, Math.max(0, selected))
            .reduce((n, line) => n + wrap(line, width).length, 0);
        position = Math.max(0, Math.min(selectedLine, Math.max(position, selectedLine - height + 1)));
    }
    useEffect(() => {
        if (!small) setScroll((n) => Math.min(n, maximum));
    }, [maximum, small]);
    const progress = busy
        ? job
            ? `${job.state} ${job.phase}: ${job.completed_count}${job.expected_count === null ? '' : ` of ${job.expected_count}`}`
            : 'Loading...'
        : '';
    if (!active) return null;
    return (
        <Box flexDirection="column" width={width} height={Math.max(1, rows)}>
            <Text wrap="truncate-end">
                {safe(
                    `WPC Results ${surface === 'results' ? '' : `/ ${surface}`} | ${view?.matching_count ?? '?'} matching | ${view?.marked_count ?? '0'} marked`,
                )}
            </Text>
            <Text wrap="truncate-end">{safe(`Collection ${hello.profile.state_dir} | ${hello.timezone}`)}</Text>
            <Text wrap="truncate-end">
                {safe(
                    view
                        ? `As of ${view.as_of.local} | ${view.retained ? 'Retained after refresh failure' : 'Fixed snapshot'}`
                        : 'No snapshot',
                )}
            </Text>
            <Text wrap="truncate-end">{safe(progress || message || (view ? filterLabel(view.filters) : ''))}</Text>
            <Box flexDirection="column" height={height} overflow="hidden">
                {wrapped.slice(position, position + height).map((line, i) => (
                    <Text
                        // biome-ignore lint/suspicious/noArrayIndexKey: Keys identify stateless terminal row slots, not records.
                        key={i}
                        wrap="truncate-end"
                        color={hello.color !== 'never' && line.startsWith('>') ? theme.accent : undefined}
                    >
                        {line}
                    </Text>
                ))}
            </Box>
            <Text wrap="truncate-end">{safe(busy ? 'c or Esc cancel | ? help | q quit' : footer)}</Text>
            <Text wrap="truncate-end">{safe(range || message || '? help | q quit')}</Text>
            <Text wrap="truncate-end">
                {safe(busy && message ? message : view?.retained ? (view.error_code ?? '') : '')}
            </Text>
        </Box>
    );
}
