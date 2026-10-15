import { Box, Text, useInput, useWindowSize } from 'ink';
import { useEffect, useRef, useState } from 'react';
import { age, type Cache, initial, refresh, type Saved } from './cache.js';
import type { Channel } from './channel.js';
import { ResultsScreen } from './results-screen.js';
import { type Hello, overview, type Stamp } from './schema.js';
import { safe, theme } from './theme.js';

const yes = (value: boolean | null) => (value === null ? 'Unknown' : value ? 'Yes' : 'No');
function at(value: Stamp | null) {
    return value ? `${value.local} UTC${offset(value.utc_offset_seconds)}` : 'Unknown';
}
function offset(value: string) {
    const n = BigInt(value);
    const abs = n < 0n ? -n : n;
    const base = `${n < 0n ? '-' : '+'}${(abs / 3600n).toString().padStart(2, '0')}:${((abs % 3600n) / 60n).toString().padStart(2, '0')}`;
    return abs % 60n === 0n ? base : `${base}:${(abs % 60n).toString().padStart(2, '0')}`;
}
function failure(code: string | null): string {
    switch (code) {
        case 'state_not_initialized':
            return 'No saved collection yet';
        case 'state_migration_required':
            return 'Saved state needs migration. Use the matching CLI writer explicitly.';
        case 'state_schema_too_new':
            return 'Saved state needs a newer matching CLI.';
        case 'service_not_installed':
            return 'Not installed';
        case 'service_changed':
            return 'Service installation changed during observation';
        case 'service_account_unavailable':
            return 'Service account unavailable';
        case 'service_manager_unavailable':
            return 'Native manager unavailable';
        case 'service_owner_unknown':
            return 'Runner ownership unknown';
        case 'deadline_exceeded':
        case 'service_diagnostic_timeout':
            return 'Read timed out';
        default:
            return code ? `Unavailable (${code})` : 'Loading';
    }
}
function evidence<T>(saved: Saved<T>, now: number): string[] {
    if (!saved.data) return [saved.status === 'loading' ? 'Loading' : failure(saved.error)];
    const result = [`${saved.status === 'stale' ? 'Stale' : 'Current'} | ${age(saved, now)} | ${at(saved.observed)}`];
    if (saved.error) result.push(failure(saved.error));
    return result;
}

// A stable list of physical body rows keeps navigation independent of React layout.
export function body(cache: Cache, hello: Hello, wide: boolean, now: number): string[][] {
    const c = cache.collection.data;
    const collection = evidence(cache.collection, now);
    if (c) {
        collection.push(
            `${c.fresh_confirmed} FRESH CONFIRMED${wide ? '          ' : '  |  '}${c.total} TOTAL CANDIDATES`,
            `Stale confirmations ${c.stale_confirmed}   Unknown freshness ${c.unknown_freshness}`,
            'Latest completed outcomes',
            `Unchecked ${c.latest_outcomes.unchecked}  Confirmed ${c.latest_outcomes.confirmed}  ` +
                `Invalid ${c.latest_outcomes.invalid}  Inconclusive ${c.latest_outcomes.inconclusive}`,
            'Fresh evidence and latest outcomes answer different questions. These groups are not additive.',
        );
        if (c.total === '0') collection.push('No candidates saved');
    } else if (cache.collection.status === 'missing') {
        collection.push(
            'The overview does not create state or start work.',
            ...hello.profile.suggestions.map((s) => s.command_text),
        );
    }
    const recent: string[] = [];
    if (!c) recent.push(...evidence(cache.collection, now));
    else {
        if (cache.collection.status === 'stale') recent.push('Stale collection snapshot');
        if (c.recent_runs.length === 0) recent.push('No saved runs');
        if (wide && c.recent_runs.length) recent.push('RUN ID         KIND        ORIGIN      SAVED STATUS');
        for (const r of c.recent_runs) {
            const status = r.status === 'running' ? 'Saved as running' : r.status;
            recent.push(
                wide
                    ? `${r.id.padEnd(14)} ${r.kind.padEnd(11)} ${r.origin.padEnd(11)} ${status}`
                    : `Run ${r.id} | ${r.kind} | ${r.origin} | ${status}`,
            );
            recent.push(`  Started ${at(r.started_at)}`);
            if (r.finished_at) recent.push(`  Finished ${at(r.finished_at)}`);
            if (r.recovered_at) recent.push(`  Recovered ${at(r.recovered_at)}`);
            if (r.reason_code) recent.push(`  Reason ${r.reason_code}`);
            if (r.clock_anomaly) recent.push('  Clock anomaly');
            if (r.crawl_counts)
                recent.push(
                    `  Saved candidates ${r.crawl_counts.saved_candidates}, sightings ${r.crawl_counts.saved_sightings}`,
                );
            if (r.validation_counts)
                recent.push(
                    '  ' +
                        Object.entries(r.validation_counts)
                            .map(([k, v]) => `${k} ${v}`)
                            .join(', '),
                );
            else if (r.kind === 'validation') recent.push('  Legacy selection counts unavailable');
        }
        if (c.older_runs_available) recent.push('Showing latest 10 runs; more saved history exists');
    }
    const schedule: string[] = ['Saved eligibility, not proof of a live runner or a promise of execution.'];
    if (!c) schedule.push(...evidence(cache.collection, now));
    else {
        const s = c.schedule;
        if (cache.collection.status === 'stale') schedule.push('Stale collection snapshot');
        schedule.push(`Checkpoint ${at(s.checkpoint_at)}`);
        if (s.clock_regressed) schedule.push('Clock pause: saved clock checkpoint is in the future');
        if (s.validation_recovery_pending) schedule.push('Validation recovery pending');
        if (s.crawl) {
            const p = s.crawl;
            schedule.push(
                `Crawl due ${at(p.due_at)} | ${p.reason}`,
                `Basis ${at(p.basis_at)} | Interval ${p.interval_ns} ns`,
                `Coverage ${p.coverage.map((v) => `${v.channel} (${v.page_limit} pages)`).join(', ')}`,
                `Basis run ${p.basis_run_id ?? 'None'} | Active saved run ${p.active_run_id ?? 'None'}`,
            );
        } else schedule.push('No saved crawl schedule');
        schedule.push(
            `Candidates ${s.candidates.total} | Active ${s.candidates.active} | Due ${s.candidates.due} | Waiting ${s.candidates.waiting}`,
            `Next candidate due ${at(s.candidates.next_due_at)}`,
        );
        if (s.validation_wait) {
            const w = s.validation_wait;
            schedule.push(
                `Validation wait ${w.reason} | Retry ${at(w.retry_at)}`,
                `Basis ${at(w.basis_at)} | Observed ${at(w.observed_at)} | Interval ${w.interval_ns} ns`,
            );
        }
    }
    const service: string[] = [];
    if (cache.service?.collection_match === 'different')
        service.push(
            'Service uses a different collection',
            `Selected ${hello.profile.state_dir}`,
            `Service ${cache.service.bound_state_dir}`,
        );
    else service.push(`Collection match: ${cache.service?.collection_match ?? 'Unknown'}`);
    service.push('Installation', ...evidence(cache.installation, now));
    if (cache.installation.data) {
        const i = cache.installation.data;
        service.push(
            `Phase ${i.phase} | Autostart enabled ${yes(i.autostart_enabled)}`,
            `Bound collection ${i.state_dir}`,
        );
    }
    service.push('Native manager', ...evidence(cache.manager, now));
    if (cache.manager.data) {
        const m = cache.manager.data;
        service.push(
            `Loaded ${yes(m.loaded)} | Running ${yes(m.running)} | PID ${m.pid ?? 'Unknown'}`,
            `State ${m.active_state ?? 'Unknown'} | Pending job ${yes(m.pending_job)} | Autostart observed ${yes(m.autostart_observed)}`,
            `Last exit ${m.last_exit_code ?? 'Unknown'} | Last signal ${m.last_signal ?? 'Unknown'}`,
        );
    }
    service.push('Runner', ...evidence(cache.runner, now));
    if (cache.runner.data) {
        const r = cache.runner.data;
        service.push(
            `Lock held ${yes(r.lock_held)} | Owner PID ${r.owner_pid ?? 'Unknown'} | Installation matches ${yes(r.installation_matches)}`,
            `Saved observation stale ${yes(r.stale)}`,
        );
        if (r.clock_anomaly) service.push('Runner clock anomaly');
        if (r.observation) {
            const o = r.observation;
            service.push(
                `Saved phase ${o.phase} | Crawl ${o.crawl_phase} | Validation ${o.validation_phase}`,
                `Started ${at(o.started_at)} | Updated ${at(o.updated_at)}`,
            );
            if (o.stopped_at) service.push(`Stopped ${at(o.stopped_at)}`);
            if (o.failure_code) service.push(`Failure ${o.failure_code}`);
        } else service.push('No saved runner observation');
    }
    return [collection, recent, schedule, service];
}

// Escaping non ASCII dynamic text makes cell widths deterministic in any terminal.
function wrap(line: string, width: number): string[] {
    // biome-ignore lint/style/noNonNullAssertion: A regex match always contains at least one code point.
    const clean = safe(line).replace(/[^\x20-\x7e]/gu, (c) => `\\u{${c.codePointAt(0)!.toString(16)}}`);
    const result: string[] = [];
    for (let i = 0; i < clean.length; i += width) result.push(clean.slice(i, i + width));
    return result.length ? result : [''];
}
const names = ['Collection', 'Recent work', 'Schedule', 'Service'];
type ScreenProps = { channel: Channel; hello: Hello; quit: (code: number, interrupt?: boolean) => void };
export function Screen(props: ScreenProps) {
    const [results, setResults] = useState(false);
    return (
        <>
            <OverviewScreen {...props} active={!results} onResults={() => setResults(true)} />
            <ResultsScreen {...props} active={results} onOverview={() => setResults(false)} />
        </>
    );
}
function OverviewScreen({
    channel,
    hello,
    quit,
    active,
    onResults,
}: ScreenProps & { active: boolean; onResults: () => void }) {
    const { columns, rows } = useWindowSize();
    const [cache, setCache] = useState(initial);
    const [busy, setBusy] = useState(true);
    const [focus, setFocus] = useState(0);
    const [scroll, setScroll] = useState(0);
    const [help, setHelp] = useState(false);
    const [now, setNow] = useState(performance.now());
    const navigation = useRef(false);
    const readingRef = useRef(false);
    const openResults = useRef(onResults);
    openResults.current = onResults;
    const request = useRef<() => void>(() => {});
    useEffect(() => {
        if (!active) return;
        navigation.current = false;
        let live = true,
            reading = false,
            pending = false;
        let timer: NodeJS.Timeout | undefined;
        const read = async () => {
            if (!live) return;
            if (reading) {
                pending = true;
                return;
            }
            clearTimeout(timer);
            reading = true;
            readingRef.current = true;
            setBusy(true);
            try {
                const next = overview(await channel.request('overview', {}));
                if (live) setCache((old) => refresh(old, next));
            } catch {
                if (live) quit(1);
                return;
            } finally {
                reading = false;
                readingRef.current = false;
                if (live) setBusy(false);
            }
            if (!live) return;
            if (navigation.current) {
                openResults.current();
                return;
            }
            if (pending) {
                pending = false;
                void read();
            } else timer = setTimeout(() => void read(), theme.refreshDelay);
        };
        request.current = () => void read();
        void read();
        const tick = setInterval(() => setNow(performance.now()), 1000);
        return () => {
            live = false;
            clearTimeout(timer);
            clearInterval(tick);
        };
    }, [channel, quit, active]);
    const width = Math.max(1, columns);
    const height = Math.max(1, rows - theme.headerRows - theme.footerRows);
    const small = columns < theme.minimumColumns || rows < theme.minimumRows;
    const starts: number[] = [];
    const lines: { text: string; heading?: number }[] = [];
    body(cache, hello, columns >= theme.tableColumns, now).forEach((section, i) => {
        starts.push(lines.length);
        lines.push({ text: `${focus === i ? '>' : ' '} ${names[i]}`, heading: i });
        for (const text of section) for (const part of wrap(`  ${text}`, width)) lines.push({ text: part });
        lines.push({ text: '' });
    });
    const maximum = Math.max(0, lines.length - height);
    const position = Math.min(scroll, maximum);
    useEffect(() => setScroll((p) => Math.max(0, Math.min(p, maximum))), [maximum]);
    useInput((input, key) => {
        if (!active) return;
        if (input === 'b' && !help && !small) {
            navigation.current = true;
            if (!readingRef.current) onResults();
            return;
        }
        if (input === 'q') {
            quit(0);
            return;
        }
        if (key.ctrl && input === 'c') {
            quit(130, true);
            return;
        }
        if (input === 'r') {
            request.current();
            return;
        }
        if (input === '?') {
            setHelp((h) => !h);
            return;
        }
        if (key.escape && help) {
            setHelp(false);
            return;
        }
        if (help || small) return;
        if (key.tab) {
            const next = (focus + (key.shift ? 3 : 1)) % 4;
            setFocus(next);
            setScroll(Math.min(starts[next] ?? 0, maximum));
        } else if (key.home) setScroll(0);
        else if (key.end) setScroll(maximum);
        else if (key.upArrow || key.pageUp) setScroll((p) => Math.max(0, p - (key.pageUp ? height : 1)));
        else if (key.downArrow || key.pageDown) setScroll((p) => Math.min(maximum, p + (key.pageDown ? height : 1)));
    });
    const degraded = [cache.collection, cache.installation, cache.manager, cache.runner].some(
        (c) => c.status === 'unavailable' || c.status === 'stale',
    );
    const helpLines = [
        'Keyboard help',
        'Tab / Shift+Tab  Focus sections',
        'Up / Down  Scroll one row',
        'Page Up / Page Down  Scroll one viewport',
        'Home / End  Start / end of body',
        'r  Refresh, presses during a read coalesce',
        '? / Esc  Close help',
        'b  Results',
        'q  Quit',
        'Ctrl+C  Interrupt',
        'Opening or leaving this view does not start or stop background work.',
    ];
    const shown = help
        ? helpLines
              .flatMap((s) => wrap(s, width))
              .slice(0, height)
              .map((text) => ({ text }))
        : small
          ? wrap('Resize to at least 60 columns and 16 rows. r refresh | ? help | q quit', width)
                .slice(0, height)
                .map((text) => ({ text }))
          : lines.slice(position, position + height);
    const header = [
        `WPC ${hello.release}  Overview  | ${busy ? 'Refreshing' : degraded ? 'Degraded' : 'Ready'}`,
        `Collection ${hello.profile.state_dir}`,
        cache.header ? at(cache.header) : `Loading | ${hello.timezone}`,
        '',
    ];
    if (!active) return null;
    return (
        <Box flexDirection="column" width={width} height={Math.max(1, rows)}>
            {header.slice(0, Math.max(0, Math.min(4, rows - 2))).map((line, i) => (
                // biome-ignore lint/suspicious/noArrayIndexKey: Keys identify fixed, stateless header row slots.
                <Text key={`h${i}`} wrap="truncate-end">
                    {safe(line)}
                </Text>
            ))}
            <Box flexDirection="column" height={height} overflow="hidden">
                {shown.map((line, i) => (
                    <Text
                        // biome-ignore lint/suspicious/noArrayIndexKey: Keys identify stateless terminal row slots, not records.
                        key={i}
                        wrap="truncate-end"
                        color={
                            hello.color !== 'never' && 'heading' in line && line.heading === focus
                                ? theme.accent
                                : undefined
                        }
                    >
                        {line.text}
                    </Text>
                ))}
            </Box>
            <Text wrap="truncate-end">
                {small ? 'r refresh | ? help | q quit' : 'b Results | Tab sections | r refresh | ? help | q quit'}
            </Text>
            <Text wrap="truncate-end">
                {help
                    ? 'Esc returns to overview'
                    : `${names[focus]} | Rows ${position + 1}-${Math.min(lines.length, position + height)} of ${lines.length}`}
            </Text>
        </Box>
    );
}
