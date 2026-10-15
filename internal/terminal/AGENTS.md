# Terminal process connection

The contract is [spec 0010](../../docs/specs/0010-terminal-foundation-overview/index.md), especially its [runtime connection](../../docs/specs/0010-terminal-foundation-overview/0001-runtime-bridge.md). This package owns bundle admission, private pipes, child supervision, and terminal restoration. Cobra and configuration resolution stay in `internal/cli`, saved reads in `internal/app`, rendering in `ui`, and signals and process exit in `cmd/wpc`.

[Spec 0011](../../docs/specs/0011-terminal-results-export/index.md) extends this boundary with results, details, explicit reveal, and reviewed file export. It governs protocol 2 while the overview shape and terminal ownership rules remain intact.

## Admission and bundle identity

[session.go](session.go) admits only Linux amd64 or arm64 with real stdin and stdout terminal descriptors and a nonempty `TERM` other than `dumb`. The CLI applies this check before configuration or asset access. Explicit help, version, and commands bypass UI startup.

[manifest.go](manifest.go) resolves the real executable through launcher symlinks and reads only its adjacent `lib/wpc-ui` tree. Manifest schema is 1 and protocol is 2; mixed protocol bundles fail admission. The manifest binds release, commit, OS, architecture, pinned Node version, lock digest, and every payload digest. Required assets must be regular files within the bundle. Missing, altered, unlisted, escaping, or symlinked payloads fail admission. There is no host Node or global installation fallback.

The parent executes the bundled Node directly, without a shell. The child receives only the selected terminal and locale environment plus the resolved no color policy; it never inherits `NODE_OPTIONS`, `NODE_PATH`, or configuration contents. Raw child stderr is discarded.

## Private protocol

[protocol.go](protocol.go) accepts newline terminated UTF-8 JSON requests up to 4 KiB including the newline, and emits replies up to 1 MiB. Required fields reject null except nullable refresh focus; duplicate fields, case aliases, and unknown fields fail admission. Request IDs are strictly increasing canonical decimal strings, at most 128 digits. Only one request may be outstanding. `hello` must match the release, commit, and protocol, followed by the first `overview` before methods in [results_dispatch.go](results_dispatch.go). Paths are accepted only for export review. Malformed requests are fatal; valid stale handles return safe operation errors.

[reply_completion.go](reply_completion.go) waits for an entered reply write before deciding whether the next request overlaps. The child can read reply bytes before the writer returns; preserve the existing deadline while waiting for completion.

[wire.go](wire.go) and [results_wire.go](results_wire.go) encode closed records from saved state. Domain IDs, counts, and nanosecond durations remain exact decimal strings, with signed strings for signed fields. Stamps include UTC microseconds, parent formatted local time, and the numeric offset. Observation ages and clock anomalies remain independent per component. Only `candidate.reveal` may return one key, bound to the active view, detail, and candidate, with a 256 KiB encoded reply limit. Ordinary replies exclude keys, request tokens, raw configuration, and arbitrary errors. Service cache retention is permitted only across the same verified installation identity.

[results_dispatch.go](results_dispatch.go) owns one preparation job and its poll and cancel methods. Busy starts fail without replacement work. View and mark activation share a cancellation gate; a committed result remains successful even if cancellation arrives during commit. Export reviews bind the fixed view, mark revision, selection, disclosure, and destination, and are consumed before starting. [results_export.go](results_export.go) supplies typed export boundaries to the CLI adapter; parent receipts preserve publication, durability, and remaining cleanup paths independently of child acknowledgement.

## Shutdown

The startup deadline stays active until the first valid overview request proves that the child accepted hello. Startup has five seconds; an overview request has an independent twelve second watchdog. EOF or any protocol failure closes admission and cancels entered reads, without a restart loop or replacement reader.

Results requests retain the twelve second request watchdog; preparation jobs have no total duration limit and remain observable through bounded polling. The first job cancellation starts a separate five second watchdog. A stuck job ends the session instead of abandoning its resources and accepting replacement work. Normal cleanup waits for entered work before closing snapshots; forced shutdown retains known or uncertain publication facts through the invocation diagnostic path.

The first shutdown trigger fixes the deadline. Child cleanup gets at most two seconds, then only that child may be killed. [term_linux.go](term_linux.go) restores captured settings, cursor visibility, input modes, and the alternate screen independently of read cleanup. Normal return waits for entered readers; the separate `cmd/wpc` watchdog forces exit after five seconds if cleanup or diagnostics stall. Forced exit returns failure unless an earlier SIGINT or SIGTERM fixed 130 or 143. Normal quit returns 0. Final errors use the existing invocation sink once, after restoration.

## Checks

You can run `rtk proxy go test -race ./internal/terminal ./internal/cli ./cmd/wpc` for process and protocol regressions. Bundle tests use synthetic files through public admission without executing them. Linux package tests require `WPC_UI_PACKAGE` pointing to a complete matching executable and bundle; an ordinary test run without it skips the packaged checks. [Packaging instructions](../../docs/terminal.md#local-packaging) describe `scripts/check-ui-package.sh` on each native architecture and the stock baseline. Compilation or a host test pass alone does not establish packaged terminal support.

_Drafted by /sync from the introducing change, worth a quick human pass._
