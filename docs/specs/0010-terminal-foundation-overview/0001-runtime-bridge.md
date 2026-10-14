# Runtime and process connection

## Summary

Go decides whether to open the interface and starts the matching bundled Node program. Ink owns keyboard input and screen rendering while Go supplies saved data through private pipes. Both processes exit together, restoring the terminal without affecting the service.

## Startup and command boundary

Add the existing common configuration flags to the root invocation: `--config`, `--state-dir`, and `--recheck-interval`. Preserve explicit flag precedence and validation. These root flags are local to the root action; do not change the existing subcommand flag definitions. Existing persistent diagnostic flags remain available.

Apply this order after Cobra argument and presentation flag validation:

1. Explicit help, `help`, and `version` follow their existing paths. No UI asset checks or child process startup occurs.
2. Any explicit runtime subcommand follows its existing handler.
3. Bare root invocation checks host OS and architecture, actual stdin and stdout terminal descriptors, and `TERM`. UI admission requires Linux amd64 or arm64, both descriptors being terminals, and a nonempty `TERM` other than `dumb`. Otherwise return ordinary unstyled root help with exit 0 and no configuration or state access.
4. For admitted terminals, resolve and validate configuration once. Resolve and validate the installed UI manifest and required files. Configuration or installation failure returns a safe diagnostic with exit 1 before changing terminal mode.
5. Capture the original terminal settings, start the UI child, and complete its handshake within five seconds. Only then enter the alternate screen and begin the first refresh. Raw input support failure is a startup error, followed by restoration.

The glibc boundary applies to the UI runtime. Failure to execute that runtime is an installation or compatibility error, not silent success. `wpc --help` remains the deterministic way to request help on a supported terminal.

## Runtime and release layout

Use Node 24 LTS, Ink 7, React 19.2 or a compatible later React 19 release, TypeScript, and npm. The verified Ink 7 entry requirements are Node 22 or newer and React 19.2 or newer. Exact patch versions and resolved engine compatibility are recorded and tested in the first build slice. Neither releases nor CI may resolve floating dependencies at build time.

Keep exact direct versions in `ui/package.json`, transitive integrity in `ui/package-lock.json`, and an exact Node version plus official archive SHA256 values for both architectures in a committed runtime manifest. Pin the npm version used by the build. A dependency update changes these records through review. Upstream version observations in the rationale are research evidence, not tested project versions.

Compile TypeScript and JSX to ESM JavaScript with the TypeScript compiler. Distribute the required production `node_modules` tree intact beside that output, including package metadata and assets. Do not add a JavaScript bundler, Bun, Node SEA, or runtime package installation. Production package preparation runs in CI or the development packaging command, never on application startup.

The release tree is:

```text
wpc
lib/wpc-ui/manifest.json
lib/wpc-ui/node/bin/node
lib/wpc-ui/app/package.json
lib/wpc-ui/app/dist/main.js
lib/wpc-ui/app/node_modules/
lib/wpc-ui/licenses/
```

The manifest records schema version 1, protocol version 1, exact WPC version and commit, target OS and architecture, Node version, UI dependency lock digest, and SHA256 digests of runtime and application files. Paths are fixed relative paths within this tree. Reject missing files, malformed manifest, mismatched release identity, wrong architecture, or hash mismatch before execution. Development packaging records `dev`, the build commit when known, and its actual lock and file digests; it uses the same startup checks.

Resolve this tree relative to the real `wpc` executable after resolving a user created launcher symlink. Never search the current directory, `PATH`, npm cache, or home directory for runtime replacements. Managed runtime files must be regular files under the resolved bundle, with no path escape. Preserve the production dependency layout; runtime loading must not depend on links to a developer checkout.

Installation extracts the whole archive into a versioned directory and places a symlink to its `wpc` on `PATH`. Copying only the Go executable remains sufficient for explicit commands but produces the selected startup error for an interactive root invocation. Existing service installation binds its executable as before; UI installation never rewrites a service definition or switches a running service to another version. Apply the existing service upgrade instructions when changing its bound executable.

GoReleaser continues to build the Go executable with CGO disabled and the existing version metadata. Native Linux jobs prepare and verify each matching UI tree before archive creation. Include third party licenses and the existing archive checksums. No Darwin or musl UI artifact is included. Glibc 2.28 or newer is an explicit installation requirement, and acceptance also requires successful runtime execution on the supported test distributions.

The minimum userland verification target is Debian 10 with glibc 2.28 and its stock dynamic loader and C++ runtime, for each target architecture. Pin the baseline root filesystem by digest in the first compatibility slice. Run the completed archive in that userland on a native machine of the matching architecture with a PTY and no network or host Node. Do not replace its libraries with a newer distribution's libraries merely to make the test pass. Native service and terminal integration also runs on Ubuntu 26.04 for each architecture; baseline containers do not replace that proof. The baseline is an isolated compatibility fixture, not a recommendation to use an old distribution for everyday operation.

Record the actual kernel and the pinned Node distribution's kernel and shared library prerequisites in the runtime manifest and installation guide during the first slice. Glibc is a necessary userland requirement, not a promise that every kernel and distribution combination works. If the selected Node or dependency package cannot run on the baseline, resolve the package choice against this spec before building further; do not silently raise the accepted glibc floor.

## Private protocol

The Go parent starts the bundled Node binary directly with an absolute JavaScript entry path. There is no shell command string. In the child, stdin and stdout remain the user's terminal. Descriptor 3 carries UI requests to Go, and descriptor 4 carries Go replies to the UI. The parent closes its unused pipe ends immediately. The child cannot keep unrelated application descriptors alive.

Use UTF-8 JSON objects separated by a single newline. Requests are at most 4 KiB including the newline; replies are at most 1 MiB. A bounded reader rejects an oversized or incomplete frame without accumulating arbitrary data. Protocol version is the integer `1`; request IDs are increasing decimal strings unique within the session. Each request has `version`, `id`, `method`, and `params`. Each reply echoes `version` and `id` and has exactly one of `result` or `error`.

| Method | Required inputs | Output | Access | Important failures |
| --- | --- | --- | --- | --- |
| `hello` | `params.release`, `params.commit`, `params.protocol` | Matching release, protocol, capability `overview`, presentation policy, and selected profile metadata | Inherited session pipes | `ui_version_mismatch`, `ui_protocol_invalid`, `ui_startup_timeout` |
| `overview` | Empty `params` object, after successful hello | Header time, collection component, and service group defined below | Same account, fixed parent resolved configuration | Component state or service codes; `ui_protocol_invalid`, `ui_read_timeout` |

Only one request may be outstanding. A duplicate ID, second hello, unknown method or field, unexpected reply, or request before handshake terminates the protocol with a safe error. Never dispatch an arbitrary Go command by name. There are no mutation, file export, configuration update, or key disclosure methods in version 1.

All numeric domain IDs, counts, and durations crossing this protocol are canonical nonnegative decimal strings. Durations have nanosecond units and fields ending in `_ns`. Signed values, such as UTC offset seconds and native exit codes, use canonical signed decimal strings. Version constants and terminal dimensions may be bounded JSON integers. Times use the `Stamp` object below. Nullable fields are explicit null. Go computes freshness and eligibility. TypeScript validates shapes before rendering and never rounds a large ID or count through `Number`.

### Complete wire records

The records below are closed field sets. `?` means nullable, not absent. `Decimal` is the string `"0"` or an unsigned decimal string without leading zeroes; `SignedDecimal` additionally permits a leading minus for a nonzero value. An opaque service identity is a validated string, not a numeric ID. `Code` is a value in the existing validated state, run reason, schedule reason, service, or diagnostic catalogues, or a terminal code named in this spec. Each field accepts only its applicable existing catalogue. Unknown codes are protocol failures, never arbitrary displayed text. Domain enums retain their existing accepted values.

| Record | Exact fields |
| --- | --- |
| `Stamp` | `utc: string`, `local: string`, `utc_offset_seconds: SignedDecimal` |
| `Error` | `code: Code` |
| `HelloResult` | `release: string`, `commit: string`, `protocol: 1`, `parent_pid: Decimal`, `capabilities: ["overview"]`, `color: "auto" or "never"`, `refresh_delay_ns: Decimal`, `timezone: string`, `profile: Profile` |
| `Profile` | `state_dir: string`, `config_path: string?`, `suggestions: Suggestion[2]` |
| `Suggestion` | `action: "sources" or "crawl"`, `command_text: string` |
| `OverviewResult` | `header_at: Stamp`, `collection: Component<Collection>`, `service: ServiceGroup` |
| `Component<T>` | `availability: "available" or "missing" or "unavailable"`, `observed_at: Stamp`, `age_ns: Decimal?`, `clock_anomaly: boolean`, `cache_policy: "replace" or "retain" or "discard"`, `error_code: Code?`, `data: T?` |
| `Collection` | `as_of: Stamp`, `max_age_ns: Decimal`, `total: Decimal`, `latest_outcomes: OutcomeCounts`, `fresh_confirmed: Decimal`, `stale_confirmed: Decimal`, `unknown_freshness: Decimal`, `recent_runs: RecentRun[0..10]`, `older_runs_available: boolean`, `schedule: Schedule` |
| `OutcomeCounts` | `unchecked: Decimal`, `confirmed: Decimal`, `invalid: Decimal`, `inconclusive: Decimal` |
| `RecentRun` | `id: Decimal`, `kind: "crawl" or "validation"`, `origin: "manual" or "scheduled" or "unknown"`, `status: RunStatus`, `started_at: Stamp`, `finished_at: Stamp?`, `recovered_at: Stamp?`, `reason_code: Code?`, `clock_anomaly: boolean`, `crawl_counts: CrawlCounts?`, `validation_counts: ValidationCounts?` |
| `CrawlCounts` | `saved_candidates: Decimal`, `saved_sightings: Decimal` |
| `ValidationCounts` | `selected`, `pending`, `active`, `completed`, `skipped`, `interrupted`, `confirmed`, `invalid`, `inconclusive`, `unresolved`, `unstarted` (all `Decimal`) |
| `Schedule` | `checkpoint_at: Stamp?`, `clock_regressed: boolean`, `validation_recovery_pending: boolean`, `crawl: CrawlSchedule?`, `candidates: ScheduleCounts`, `validation_wait: ValidationWait?` |
| `CrawlSchedule` | `coverage: SourcePlan[1..32]`, `due_at: Stamp`, `reason: Code`, `basis_at: Stamp`, `interval_ns: Decimal`, `basis_run_id: Decimal?`, `active_run_id: Decimal?` |
| `SourcePlan` | `channel: string`, `page_limit: Decimal` |
| `ScheduleCounts` | `total: Decimal`, `active: Decimal`, `due: Decimal`, `waiting: Decimal`, `next_due_at: Stamp?` |
| `ValidationWait` | `reason: Code`, `basis_at: Stamp`, `retry_at: Stamp`, `observed_at: Stamp`, `interval_ns: Decimal` |
| `ServiceGroup` | `identity: ServiceIdentity`, `collection_match: "match" or "different" or "unknown"`, `bound_state_dir: string?`, `installation: Component<Installation>`, `manager: Component<Manager>`, `runner: Component<Runner>` |
| `ServiceIdentity` | `state: "verified" or "missing" or "changed" or "unknown"`, `installation_id: string?`, `cache_policy: "retain" or "discard"` |
| `Installation` | `installation_id: string`, `phase: string`, `autostart_enabled: boolean?`, `state_dir: string` |
| `Manager` | `loaded: boolean`, `running: boolean?`, `pid: Decimal?`, `last_exit_code: SignedDecimal?`, `last_signal: Decimal?`, `active_state: string?`, `pending_job: boolean?`, `autostart_observed: boolean?` |
| `Runner` | `lock_held: boolean?`, `owner_pid: Decimal?`, `installation_matches: boolean?`, `stale: boolean?`, `clock_anomaly: boolean`, `observation: RunnerObservation?` |
| `RunnerObservation` | `phase: string`, `started_at: Stamp`, `updated_at: Stamp`, `stopped_at: Stamp?`, `crawl_phase: string`, `validation_phase: string`, `failure_code: Code?` |

`Stamp.utc` is UTC RFC3339 with microsecond precision. `Stamp.local` is parent formatted `YYYY-MM-DD HH:mm:ss` in Go's resolved `time.Local`; its numeric offset is seconds east of UTC. `timezone` is the parent resolved zone label for initial context, while each stamp's offset remains authoritative through offset changes. `header_at` is sampled at reply preparation, even when both data sources fail. Zero source timestamps become null only where the record permits null; a required missing timestamp fails its component.

The two profile suggestions are `sources` then `crawl`. Go produces their command text from the existing command surface and the exact explicitly selected profile arguments, using safe shell quoting. `config_path` is the resolved configuration path when one exists, otherwise null. This is metadata, not configuration contents. Profile metadata is available after hello even when the collection cannot be read.

For each component, Go computes `age_ns` as reply preparation time minus `observed_at`, in nanoseconds. A negative age or a source clock anomaly sets `clock_anomaly=true` and `age_ns=null`. The UI captures its own monotonic receive time and adds subsequent elapsed time only for display. This age includes parent processing time but not the brief pipe delivery delay; it is an observation age, not an exact latency measurement. Freshness always uses the collection's fixed `as_of` and never this display age.

The `hello` request's `commit` is an empty string when absent in both matching builds. Its protocol field is the integer 1. JSON errors have only the `Error` shape; fixed frontend message mappings supply explanations. Frame rejection before a trustworthy request ID is known closes the channel and becomes a parent final diagnostic rather than a fabricated reply ID.

`parent_pid` comes from the Go process's actual PID. The UI verifies it against its direct parent at startup and uses it only for the keyboard interruption behavior below. It is never an identity source for the background service.

### Read and shutdown deadlines

The parent runs collection and service observation independently under a ten second request budget. Collection reading has a five second context. Service observation has a five second context, including account resolution and native manager calls. A section timeout returns an unavailable component with a stable code when its entered work can finish cleanup; the other section may still succeed. Normal replies wait for entered readers to finish cleanup and close. A context deadline is not permission to abandon a native operation and open a replacement reader.

Both parent and UI start independent twelve second watchdogs when an overview request is accepted or sent. They do not wait on the read goroutine. If no complete reply arrives, cancel the whole session with `ui_read_timeout` and enter the bounded shutdown below. A cancellation resistant read therefore cannot keep the session alive forever. No replacement request or reader starts after this cancellation.

No push stream or reconnection loop is introduced. Pipe EOF or child exit cancels pending reads. A broken protocol output is latched and never written again. Unknown or malformed response data cannot become a healthy display.

## Terminal ownership and shutdown

The UI owns terminal rendering after handshake. Parent diagnostic events are captured as safe session facts instead of being printed over the screen. Expected component failures arrive in replies and are shown in their sections. A fatal parent or child failure ends the UI, restores the terminal, and then uses the existing diagnostic renderer once.

Do not pipe raw Node stderr into the terminal or diagnostics. Drain it with a fixed memory bound and discard its raw contents; classify child failure from process and protocol facts. Unexpected JavaScript errors produce only an approved code. Disable Node warnings that would bypass the renderer, and handle application exceptions through this boundary. Child stdout remains UI rendering only.

Pass only the environment needed for terminal and locale behavior, including `TERM`, `COLORTERM`, `LANG`, `LC_ALL`, `LC_CTYPE`, and `TZ` when present. Supply the resolved no color choice explicitly. Do not inherit Node injection variables such as `NODE_OPTIONS` or `NODE_PATH`. No configuration contents or credentials enter argv or the environment. The parent retains any environment needed for its existing configuration and service resolution.

Normal quit (`q`) unmounts Ink, restores screen state, closes the channel, and returns 0. Disable Ink's competing automatic Ctrl+C exit behavior. On keyboard Ctrl+C while the parent channel is connected, the UI forwards SIGINT to its verified direct Go parent before starting cleanup. The parent records interruption through the same signal handler as external SIGINT. A signal received by the child from its parent only starts child cleanup and is never forwarded again. If the channel is already closed or the parent has exited, the UI restores its own terminal state and exits 130 without signaling another PID.

Parent SIGINT and SIGTERM cancel the session and signal only its UI child. The first interruption recorded by `cmd/wpc` fixes exit 130 for SIGINT (including forwarded keyboard Ctrl+C) or 143 for SIGTERM. Later errors, signals, and watchdog expiry do not replace that code. Without a recorded interruption, normal quit returns 0 and a fatal failure returns 1. These rules apply equally to normal and forced shutdown.

The first shutdown trigger fixes one monotonic deadline; repeated signals and failures never reset it. Cancel reads and close protocol admission immediately. Allow at most two seconds for UI cleanup, then kill only that child if it has not exited. A process supervisor independent of database cleanup restores the captured terminal settings, cursor visibility, alternate screen, and enabled input modes as soon as the child stops. With usable terminal descriptors, restoration follows no later than the end of that two second child allowance. It never waits for a database close or native manager call. Keep restoration idempotent so child and parent cleanup can both run.

At five seconds from the first shutdown trigger, `cmd/wpc` forces process exit if any owned read, native command, pipe drain, or child wait still prevents normal return. Terminate any still owned native diagnostic child as well; do not signal arbitrary process groups or service processes. This watchdog applies only to this read session and never changes writer or validator shutdown contracts. Forced exit uses `ui_shutdown_timeout` and exit 1, unless an already recorded SIGINT, Ctrl+C, or SIGTERM requires 130 or 143. It does not report graceful reader closure. The operating system releases the remaining read process resources.

The watchdog attempts a safe final diagnostic only through an available sink and never lets logging delay its fixed process exit deadline. Restoration does not retry a stream already known to be broken; restoring input settings remains possible independently. A broken or hung terminal may prevent visible screen restoration, but cannot extend process lifetime. The UI also exits on parent channel EOF. No shutdown operation acquires a writer or signals the background service.

On terminal hangup, cancel reads, close pipes, reap the child, and return a failure without retrying failed output. Forced termination of the entire process group by SIGKILL cannot guarantee terminal restoration; do not claim otherwise. The required crash case is a failed UI child while its Go parent remains available to restore the terminal.

## Failure codes and observability

Add safe terminal categories to the existing diagnostic mapping: `ui_assets_missing`, `ui_assets_invalid`, `ui_runtime_unavailable`, `ui_version_mismatch`, `ui_protocol_invalid`, `ui_startup_timeout`, `ui_read_timeout`, `ui_shutdown_timeout`, and `ui_process_failed`. Messages explain the next step using fixed text. They do not include raw loader errors, JavaScript stacks, protocol bytes, or manifest content.

No routine polling log or new metrics sink is needed. Debug may identify lifecycle phase and the safe code through the existing diagnostic catalogue, with no raw payload. While Ink is active, these facts stay inside its error presentation; final diagnostics follow spec 0009 after restoration. No technical protocol details appear in the normal overview.

## Rationale

Private inherited pipes let Go remain authoritative without a port, service registration, or extra persistent process. Keeping Node and production modules visible in the archive is easier to inspect and maintain than embedding their assets into a special executable. Repeated subprocess reads are the simpler alternative but repeat startup work and provide a weaker foundation for the later interactive slices. (basis: existing Go boundaries, your process channel choice, and the verified Ink and Node documentation cited in the umbrella rationale)
