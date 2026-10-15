# Terminal process connection

The contract is [spec 0010](../../docs/specs/0010-terminal-foundation-overview/index.md), especially its [runtime connection](../../docs/specs/0010-terminal-foundation-overview/0001-runtime-bridge.md). This package owns bundle admission, private pipes, child supervision, and terminal restoration. Cobra and configuration resolution stay in `internal/cli`, saved reads in `internal/app`, rendering in `ui`, and signals and process exit in `cmd/wpc`.

[Spec 0011](../../docs/specs/0011-terminal-results-export/index.md) extends this boundary with results, details, explicit reveal, and reviewed file export. [Spec 0012](../../docs/specs/0012-terminal-crawl-check-controls/index.md) adds reviewed manual work and saved reports through protocol 3 while preserving the overview and disclosure boundaries.

## Admission and bundle identity

[session.go](session.go) admits only Linux amd64 or arm64 with real stdin and stdout terminal descriptors and a nonempty `TERM` other than `dumb`. The CLI applies this check before configuration or asset access. Explicit help, version, and commands bypass UI startup.

[manifest.go](manifest.go) resolves the real executable through launcher symlinks and reads only its adjacent `lib/wpc-ui` tree. Manifest schema is 1 and protocol is 3; mixed protocol bundles fail admission. The manifest binds release, commit, OS, architecture, pinned Node version, the `app/pnpm-lock.yaml` digest, and every payload digest. Required assets must be regular files within the bundle. Missing, altered, unlisted, escaping, or symlinked payloads fail admission. There is no host Node or global installation fallback.

The parent executes the bundled Node directly, without a shell. The child receives only the selected terminal and locale environment plus the resolved no color policy; it never inherits `NODE_OPTIONS`, `NODE_PATH`, or configuration contents. Raw child stderr is discarded.

## Private protocol

[protocol.go](protocol.go) accepts newline terminated UTF-8 JSON requests up to 4 KiB including the newline, and emits replies up to 1 MiB. Required fields reject null except fields explicitly nullable in their method schema; duplicate fields, case aliases, and unknown fields fail admission. Request IDs are strictly increasing canonical decimal strings, at most 128 digits. Only one request may be outstanding. `hello` must match the release, commit, and protocol, followed by the first `overview` before methods in [results_dispatch.go](results_dispatch.go). Paths are accepted only for export review. Malformed requests are fatal; valid stale handles return safe operation errors.

[reply_completion.go](reply_completion.go) waits for an entered reply write before deciding whether the next request overlaps. The child can read reply bytes before the writer returns; preserve the existing deadline while waiting for completion.

[wire.go](wire.go) and [results_wire.go](results_wire.go) encode closed records from saved state. Domain IDs, counts, and nanosecond durations remain exact decimal strings, with signed strings for signed fields. Stamps include UTC microseconds, parent formatted local time, and the numeric offset. Observation ages and clock anomalies remain independent per component. Only `candidate.reveal` may return one key, bound to the active view, detail, and candidate, with a 256 KiB encoded reply limit. Ordinary replies exclude keys, request tokens, raw configuration, and arbitrary errors. Service cache retention is permitted only across the same verified installation identity.

[results_dispatch.go](results_dispatch.go) owns one preparation job and its poll and cancel methods. Busy starts fail without replacement work. View and mark activation share a cancellation gate; a committed result remains successful even if cancellation arrives during commit. Export reviews bind the fixed view, mark revision, selection, disclosure, and destination, and are consumed before starting. [results_export.go](results_export.go) supplies typed export boundaries to the CLI adapter; parent receipts preserve publication, durability, and remaining cleanup paths independently of child acknowledgement.

[work_dispatch.go](work_dispatch.go) owns work review handles and connects Start, polling, cancellation and acknowledgement to `app.WorkSession`. Start consumes its review before dispatch; changed Results views or marks invalidate reviews. The operational slot is separate from preparation, so work polling and cancellation remain available during report or export preparation. Work and preparation cancellation are independent. Ordinary work records expose only typed saved facts, bounded activity and safe guidance, never keys or recovery identities.

[report_dispatch.go](report_dispatch.go) opens complete saved reports through the common preparation job. Report handles bind one collection snapshot and observation time; refresh validates structural focus and activates a replacement only through the commit gate. Failed live access retains the old report and labels it; collection replacement invalidates it. Sources, pages, items and attempts remain reachable through bounded pages without writer or Docker access.

## Shutdown

The startup deadline stays active until the first valid overview request proves that the child accepted hello. Startup has five seconds; an overview request has an independent twelve second watchdog. EOF or any protocol failure closes admission and cancels entered reads, without a restart loop or replacement reader.

Results requests retain the twelve second request watchdog; preparation jobs have no total duration limit and remain observable through bounded polling. The first job cancellation starts a separate five second watchdog. A stuck job ends the session instead of abandoning its resources and accepting replacement work. Normal cleanup waits for entered work before closing snapshots; forced shutdown retains known or uncertain publication facts through the invocation diagnostic path.

The first shutdown trigger fixes the deadline. Child cleanup gets at most two seconds, then only that child may be killed. [term_linux.go](term_linux.go) restores captured settings, cursor visibility, input modes, and the alternate screen independently of owned work cleanup. Normal return waits for entered resources. The shared `app.OperationGuard` gives the `cmd/wpc` watchdog five seconds when idle or at most 75 seconds with owned work, measured from an earlier operation stop when present. Repeated signals and cancellation never extend that deadline. Remote cleanup retains its 60 second allowance and local finalization its five second allowance. Forced exit returns failure unless an earlier SIGINT or SIGTERM fixed 130 or 143. Successful intentional cancellation and cleanup permit normal exit 0; unresolved cleanup or uncertain persistence remain failures. Final work and export receipts use the existing invocation sink once, after restoration.

## Checks

You can run `rtk proxy go test -race ./internal/terminal ./internal/cli ./cmd/wpc` for process and protocol regressions. Bundle tests use synthetic files through public admission without executing them. Linux package tests require `WPC_UI_PACKAGE` pointing to a complete matching executable and bundle; an ordinary test run without it skips the packaged checks. [Packaging instructions](../../docs/terminal.md#local-packaging) describe `scripts/check-ui-package.sh` on each native architecture and the stock baseline. Compilation or a host test pass alone does not establish packaged terminal support.

_Drafted by /sync from the introducing change, worth a quick human pass._
