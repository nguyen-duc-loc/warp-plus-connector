# Terminal UI

The contracts are [spec 0010](../docs/specs/0010-terminal-foundation-overview/index.md) for the overview, [spec 0011](../docs/specs/0011-terminal-results-export/index.md) for results, details, reveal, and reviewed file export, and [spec 0012](../docs/specs/0012-terminal-crawl-check-controls/index.md) for manual work and saved reports. Go owns configuration, saved data, freshness, service observations, export bytes, operation admission, cleanup and process supervision.

Design follows [design.md](design.md); presentation constants live in [src/theme.ts](src/theme.ts).

## Runtime and build

[package.json](package.json) pins Ink 7.1.1, React 19.2.8, and TypeScript 7.0.2. Node 24.20.0 and pnpm 12.11.2 are pinned in [runtime.json](runtime.json), together with architecture specific official archive hashes and the native compatibility baseline. `pnpm-lock.yaml` fixes transitive dependencies. Biome 2.5.15 owns UI linting, formatting, and import ordering through `biome.json`. The supported UI release targets are Linux amd64 and arm64 with glibc 2.28 or newer; macOS is a development host only.

TypeScript compiles JSX and modules to ESM in `dist/`; there is no JavaScript bundler. Release packaging preserves production `node_modules`, package metadata, assets, and licenses beside the compiled app and bundled Node. Test modules are omitted from the shipped tree. Startup never installs packages or contacts the network. The full extracted directory tree is the installation unit.

From the repository root, you can use:

```sh
rtk proxy pnpm -C ui install --frozen-lockfile
rtk proxy pnpm -C ui run check
rtk proxy pnpm -C ui run build
rtk proxy pnpm -C ui test
rtk proxy pnpm -C ui run verify
rtk proxy pnpm -C ui run format
```

Tests use Node's built in runner against compiled `dist/*.test.js`; build after changing test sources. The commit gate also runs `pnpm -C ui run verify`, which checks Biome without edits, builds with strict TypeScript, and runs all UI tests. Install dependencies first. Native CI enforces the same checks. [scripts/prepare-ui.py](../scripts/prepare-ui.py) requires the exact Node and pnpm pins, installs from the lock, and prepares `.ui-build/<arch>/lib/wpc-ui`. [Terminal packaging](../docs/terminal.md#local-packaging) covers both targets, identity stamping, payload validation, and native package checks.

Biome uses four spaces for TypeScript and TSX, two for JSON, LF, single JavaScript quotes, and a 120 column formatting target. Its recommended lint rules stay enabled; terminal specific exceptions are explained locally, and fixture tests permit non-null assertions. Generated output and dependencies are excluded.

The pnpm workspace disables lifecycle scripts and enforces exact runtime engines. Release preparation uses the hoisted linker, removes installer bookkeeping and command launchers, and rejects file and directory symlinks. Bundle admission and archive verification bind `app/pnpm-lock.yaml`.

## Protocol and failures

[src/channel.ts](src/channel.ts) uses cancellable event driven sockets over inherited request descriptor 3 and reply descriptor 4. It enforces one pending request, bounded frames, strict UTF-8, duplicate field rejection, matching reply IDs, and independent hello and overview timeouts. Failure closes both pipes and exits; there is no reconnection loop.

The channel queues requests behind the single wire request. Cancellation has highest priority, then user actions, work polls, and preparation or overview polls. Equal priority keeps arrival order; identical polls and cancellation requests coalesce while pending. Work polls resume one second after the previous poll completes. `cancelPreparation` targets only the current preparation job, independently of operational work.

[src/schema.ts](src/schema.ts), [src/results-schema.ts](src/results-schema.ts), and [src/work-schema.ts](src/work-schema.ts) validate protocol 3 records before rendering, including nullable fields, exact decimal strings, timestamp validity, page bounds, job states, publication facts, work policy, receipts and reports. IDs and counts never pass through `Number`. Keep [src/codes.ts](src/codes.ts) aligned with the Go diagnostic catalogue and each field's permitted values. Keys enter only through explicit reveal; provider content, arbitrary errors, and request tokens remain excluded.

[src/main.tsx](src/main.tsx) verifies release, commit, direct parent PID, and presentation metadata before Ink takes terminal ownership. Only the UI renders while it is active. Keyboard Ctrl+C signals the verified connected Go parent once; child signals only initiate child cleanup. Cleanup unmounts Ink and closes the channel, with the parent's restoration and exit deadlines as a separate safeguard.

## Refresh and display

[src/screen.tsx](src/screen.tsx) starts the first refresh on entry. Later automatic reads start five seconds after completion. Manual presses during a read coalesce into one pending refresh. Reads never overlap. Focus and scroll survive refresh, with scroll clamped when content shrinks.

Opening Results waits for the entered overview request, then pauses overview polling. Returning triggers a new overview read and resumes its timer. [src/results-screen.tsx](src/results-screen.tsx) retains one bounded page, exact logical positions, and at most the latest queued navigation target. Results change only on explicit refresh or filter application; Details show their separate observation time. Expected read failures keep prior content with a visible unavailable or retained label.

Reveal completion is bound to the active detail and a local generation. Hide, leave, refresh, help, lost ownership, quit, and crossing below 60 columns or 16 rows clear disclosure. Intent received during a pending reveal invalidates its reply; later work waits for that request. Display keys as escaped quoted text. File export is the exact byte path and has a separate disclosure choice.

Export opens with matching selection, JSON, hidden keys, and an empty path. Printable shortcut letters remain text in the destination field. Review initially focuses Back; only Create file starts publication. Reject paths exceeding the escaped 4 KiB request budget before sending. Progress, help, cancellation, resize, and quit remain available during preparation. Outcomes distinguish created, not created, and uncertain publication, require acknowledgement, and never retry automatically.

[src/controls.tsx](src/controls.tsx) owns crawl setup, focused, marked or matching checks, review, work, saved reports and quit confirmation. Crawl selects configured enabled sources and an optional page limit from 1 through 100; review preserves the parent supplied plan and policy. Back has initial review focus; only the separate Start action starts work. Clear key disclosure before entering work, reports or quit confirmation, and discard late review intent after navigation.

Work completion updates the indicator without moving focus. Ctrl+X cancels work and Ctrl+G cancels preparation, including text fields, help and small terminals. Printable shortcuts do not escape text entry. Quit with active work defaults to Stay; confirmed quit delegates cancellation and cleanup to Go. An unresolved cleanup warning survives ordinary outcome acknowledgement.

Saved reports show one fixed observation until explicit refresh. Tab selects sections, Enter opens a source's pages or a candidate's attempts, Left and Right move between rows, and Page Up and Page Down traverse bounded pages. Failed refresh keeps the prior report and focus with a retained label; collection replacement discards its handle. Recent runs and exact older run IDs share the same saved report path.

[src/cache.ts](src/cache.ts) keeps successful sections independently. Retained stale data keeps its original observation and monotonic receive time; identity changes or loss discard service caches. Loading, missing, initialized empty, unavailable, stale, and clock anomaly states remain distinct. Saved schedule facts never prove that a runner is live.

Collection, Recent work, Schedule, and Service share one scrollable body with fixed header and shortcuts. At fewer than 60 columns or 16 rows, the resize notice preserves refresh, help, and quit. At 100 columns, recent work uses the wide layout. `--color never` and nonempty `NO_COLOR` remove decoration while labels and focus markers retain meaning. Escape dynamic controls before layout; body text also escapes non ASCII characters for predictable terminal widths. Do not add images, downloaded fonts, or custom terminal backgrounds.

_Drafted by /sync from the introducing change, worth a quick human pass._
