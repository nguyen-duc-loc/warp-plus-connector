# Terminal UI

The contract is [spec 0010](../docs/specs/0010-terminal-foundation-overview/index.md). Ink renders the read only overview; Go owns configuration, saved data, freshness, service observations, and process supervision. Browsing keys, exporting, and starting work are separate features.

Design follows [design.md](design.md); presentation constants live in [src/theme.ts](src/theme.ts).

## Runtime and build

[package.json](package.json) pins Ink 7.1.1, React 19.2.8, and TypeScript 7.0.2. Node 24.20.0 and npm 12.0.2 are pinned in [runtime.json](runtime.json), together with architecture specific official archive hashes and the native compatibility baseline. `package-lock.json` fixes transitive dependencies. The supported UI release targets are Linux amd64 and arm64 with glibc 2.28 or newer; macOS is a development host only.

TypeScript compiles JSX and modules to ESM in `dist/`; there is no JavaScript bundler. Release packaging preserves production `node_modules`, package metadata, assets, and licenses beside the compiled app and bundled Node. Test modules are omitted from the shipped tree. Startup never installs packages or contacts the network. The full extracted directory tree is the installation unit.

From the repository root, you can use:

```sh
rtk proxy npm --prefix ui ci --ignore-scripts --no-audit --no-fund
rtk proxy npm --prefix ui run check
rtk proxy npm --prefix ui run build
rtk proxy npm --prefix ui test
```

Tests use Node's built in runner against compiled `dist/*.test.js`; build after changing test sources. The Go commit hooks do not run UI checks. [scripts/prepare-ui.py](../scripts/prepare-ui.py) requires the exact Node and npm pins, installs from the lock, and prepares `.ui-build/<arch>/lib/wpc-ui`. [Terminal packaging](../docs/terminal.md#local-packaging) covers both targets, identity stamping, payload validation, and native package checks.

## Protocol and failures

[src/channel.ts](src/channel.ts) uses cancellable event driven sockets over inherited request descriptor 3 and reply descriptor 4. It enforces one pending request, bounded frames, strict UTF-8, duplicate field rejection, matching reply IDs, and independent hello and overview timeouts. Failure closes both pipes and exits; there is no reconnection loop.

[src/schema.ts](src/schema.ts) validates every closed record before rendering, including nullable fields, exact decimal strings, timestamp validity, component availability, service identity, and outcome totals. IDs and counts never pass through `Number`. Keep [src/codes.ts](src/codes.ts) aligned with the Go diagnostic catalogue and each field's permitted values. Keys, provider content, arbitrary errors, and request tokens are outside the protocol.

[src/main.tsx](src/main.tsx) verifies release, commit, direct parent PID, and presentation metadata before Ink takes terminal ownership. Only the UI renders while it is active. Keyboard Ctrl+C signals the verified connected Go parent once; child signals only initiate child cleanup. Cleanup unmounts Ink and closes the channel, with the parent's restoration and exit deadlines as a separate safeguard.

## Refresh and display

[src/screen.tsx](src/screen.tsx) starts the first refresh on entry. Later automatic reads start five seconds after completion. Manual presses during a read coalesce into one pending refresh. Reads never overlap. Focus and scroll survive refresh, with scroll clamped when content shrinks.

[src/cache.ts](src/cache.ts) keeps successful sections independently. Retained stale data keeps its original observation and monotonic receive time; identity changes or loss discard service caches. Loading, missing, initialized empty, unavailable, stale, and clock anomaly states remain distinct. Saved schedule facts never prove that a runner is live.

Collection, Recent work, Schedule, and Service share one scrollable body with fixed header and shortcuts. At fewer than 60 columns or 16 rows, the resize notice preserves refresh, help, and quit. At 100 columns, recent work uses the wide layout. `--color never` and nonempty `NO_COLOR` remove decoration while labels and focus markers retain meaning. Escape dynamic controls before layout; body text also escapes non ASCII characters for predictable terminal widths. Do not add images, downloaded fonts, or custom terminal backgrounds.

_Drafted by /sync from the introducing change, worth a quick human pass._
