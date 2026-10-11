# 0008. Background operation and diagnostics

**Date**: 2026-10-11
**Status**: Proposed

## Summary

You can install one background service for your account, start it, inspect its work, and stop it. The operating system starts it at login and supervises the existing runner. Private status records and bounded logs explain failures without exposing keys or requiring a working database.

## Structure

1. [User service](0001-user-service.md) defines installation, native supervision, ownership, configuration binding, and shutdown.
2. [Operational diagnostics](0002-operational-diagnostics.md) defines observations, status, logs, local checks, and their data sources.

Both children belong to scope feature 9. This index owns their shared contract and build sequence. The accepted scheduler and validator recovery contracts remain in force.

## Requirements

**User story**: As the local operator, you can leave collection and validation running, understand whether work is progressing, and stop or repair the service without losing saved results.

| ID | Acceptance criterion |
| --- | --- |
| AC-1 | Built in service commands manage one service per account on macOS and Linux arm64, using user launchd and systemd respectively. No root service, automatic privilege elevation, or detached process fallback is introduced. |
| AC-2 | Installation validates an existing private configuration file and an available user manager before publishing managed files. It enables startup at the next login but does not start work immediately. Start is a separate action. |
| AC-3 | The service retains explicit executable, configuration, state, and shared validator paths. Enabled validation also retains the Docker executable and local socket. It rereads settings on each start, rejects changes to bound paths, and does not inherit a different Docker context from a later shell. |
| AC-4 | Start, stop, restart, enable, disable, and uninstall have repeatable outcomes. Stop affects the current session and preserves login startup. Disable changes future startup without stopping the current process. A differing installation requires stop and uninstall first. Uninstall preserves configuration, candidate history, and diagnostics. |
| AC-5 | Unexpected process crashes receive paced native restarts. Known fatal application errors stop automatic retries and remain visible as failures, including configuration, ownership, state, and diagnostic errors. Ordinary source failures and temporary validator waits retain the scheduler's existing recovery behavior. |
| AC-6 | One managed runner holds the service lifetime lock and the existing state writer lock. Competing services, foreground runners, and manual writers cannot bypass state or validator ownership. A losing writer performs no source request or Docker mutation. |
| AC-7 | Graceful stop retains the shared 60 second remote cleanup and five second local finalization windows. The native manager may force termination after 75 seconds. A managed process watchdog applies that same outer bound to internally initiated failure shutdown. Unfinished work remains recoverable, and restart never treats process death as proof of remote cleanup. |
| AC-8 | Private versioned JSON retains installation metadata and only the latest runner observation. Updates occur on phase changes and every 15 seconds. Status marks observations older than 60 seconds stale, and never infers process liveness from a PID or timestamp alone. No SQLite migration is needed. |
| AC-9 | Status shows service installation, startup intent, manager observation, runner observation, enabled lanes, saved last and next runs, queue counts, collection outcomes, latest run totals, and saved source or validator failure reasons. Complete last terminal summaries remain visible while new work runs. Each fact names its observation time and source. Disabled or blocked work is never promised a future execution time. |
| AC-10 | Database status fields use one read snapshot and one evaluation time, including all 338 controlled candidates. Status and doctor never migrate, initialize, recover, or modify candidate state. Missing or unreadable components produce explicit unavailable fields while other diagnostic components remain usable. |
| AC-11 | Status and doctor provide equivalent text and schema version 1 JSON. Successful status reporting exits 0 even when the service is stopped or unhealthy. Doctor exits 1 for failed checks after delivering its complete report. Invalid arguments fail before state or manager access. |
| AC-12 | Service logs retain at most 20 MiB across five files, including the current file. Recent inspection and optional following work across rotation and restart, with explicit gaps when retention removes unread entries. Logs contain controlled fields and stable codes, never keys, credentials, raw source content, or raw vendor responses. |
| AC-13 | Doctor checks local paths, configuration, native service files, manager access, saved state, and the configured local Docker daemon and image. It creates no container or registration, fetches no image, contacts no source or provider, and makes no repairs. |
| AC-14 | Log or observation write failure cancels managed work and initiates normal cleanup. Partial installation, uncertain file publication, manager failures, and interrupted control commands remain diagnosable and safe to retry without overwriting foreign files or discarding recovery state. |
| AC-15 | Help and version retain their absence of configuration, state, manager, and network access. Foreground run and existing report commands retain their output and exit contracts. Native definition arguments and service diagnostics disclose no candidate keys. |
| AC-16 | Runtime verification proves the service lifecycle on both arm64 platforms, including native login activation, duplicate ownership, controlled crash restart, permanent error handling, rotation, stop deadlines, and recovery. A Linux container without a real user manager is insufficient evidence for native supervision. |

## Decision

**Chosen option**: Built in native user service management around the existing foreground application runner, with private JSON observations and application owned bounded logs.

You confirmed this design on 2026-10-11 after independent review and incorporation of all eleven approved fixes. It is linked to scope feature 9 as a new feature record, not a replacement for spec 0007. Implementation remains pending, so its status stays `Proposed` until implementation begins.

Use Go standard library facilities for native commands, XML, JSON, file ownership, and rotation. A general service library is the runner up, but these two adapters still need explicit lifecycle and failure policies. No additional dependency, community skill, or MCP server is selected.

### Shared contract

The native manager owns process supervision. A private managed entry point runs the same application coordinator as `wpc run` in that process. It adds diagnostic sinks, observations, and native exit classification. It does not introduce a second scheduler, a parent process supervising a child, or an IPC server.

The service installation is account scoped. Its state directory remains a profile selected at installation. Private diagnostic records are separate from SQLite and the validator registry. They cannot authorize a write, a provider attachment, or a cleanup success. Candidate history and scheduling remain authoritative in SQLite; remote recovery remains authoritative in the existing shared validator journal.

Keep all process exit mapping in `cmd/wpc`, parsing and rendering in `internal/cli`, scheduling in `internal/app`, and SQL in `internal/store`. Add `internal/service` when the first service path needs native adapters, private service files, and log rotation. Small interfaces cover native process control and filesystem fault injection. Domain status aggregates belong in `internal/state` and contain no native command output or driver types.

## Feature design

### Public surface

All actions use the current local account. The `service` parent shows help. There are no named services, remote targets, alternate users, service passwords, or key disclosure flags.

| Command | Inputs | Result | Important errors |
| --- | --- | --- | --- |
| `service install` | Required `--config PATH` | Installation identity and enabled login startup, without starting work | `service_config_invalid`, `service_manager_unavailable`, `service_conflict` |
| `service start` | None | Already running, started, or accepted but still starting | `service_not_installed`, `service_disabled`, `service_start_failed`, `service_identity_unverified` |
| `service stop` | None | Already stopped or stopped, with cleanup observation if known | `service_manager_unavailable`, `service_stop_timeout`, `service_identity_unverified`, `service_conflict` |
| `service restart` | None | Complete stop followed by start | Same failures as stop and start; no start after an unproven stop |
| `service enable` | None | Login startup enabled; no immediate start | `service_not_installed`, `service_conflict` |
| `service disable` | None | Login startup disabled; current process unchanged | `service_not_installed`, `service_conflict` |
| `service uninstall` | None | Managed definition and installation record removed | `service_running`, `service_conflict`, `service_operation_incomplete` |
| `service status` | `--format text\|json` | Complete available operational report | `service_access_error`, `output_error`; component errors also appear in the report |
| `service logs` | `--lines N`, `--follow`, `--format text\|json` | Recent records and optional later records | `service_access_error`, `service_log_invalid`, `output_error` |
| `service doctor` | `--format text\|json` | Local prerequisite and consistency checks | Complete failed checks exit 1; output errors remain distinct |

Formats default to text. Logs default to 100 entries; `--lines` accepts 1 through 10000. Other commands reject positional arguments and configuration overrides. They use the installed binding even if the current default configuration is broken. Install accepts no schedule or validator overrides, since the selected file is the source of runtime settings.

Control results use one concise text record with `action`, `changed`, `installation_id`, and the resulting observed state. No control JSON format is added in this slice. A failure after a native or file mutation includes `changed=true` and `operation_incomplete=true` when its final result is uncertain. Successful delivery failure retains that outcome rather than claiming rollback. Status, doctor, and finite log output are prepared before delivery; follow mode streams complete records.

### Data model and value sourcing

| Record | Identity and relationship | Authority |
| --- | --- | --- |
| Installation manifest | One account slot, random installation ID | Bound paths, managed native definition, latest control operation, intended login startup |
| Removal receipt | One replaceable receipt for the last removed installation ID | Acknowledged native and definition removal, allowing retry after manifest deletion |
| Latest runner observation | One replaceable record, installation ID plus invocation ID | Last phase and policy observed by that invocation; not process liveness |
| Diagnostic entry | Installation ID, invocation ID, sequence within invocation | Bounded operational evidence; not candidate or cleanup history |
| Saved work | Existing candidate, run, source, batch, attempt, and schedule relationships | Work results, eligibility, outcome counts, and recovery |

The children define exact fields, nullability, derivations, and retention. No second copy of candidate keys, run history, or eligibility is stored in service metadata. Locks coordinate processes but are not records of process identity.

| Produced value | Source |
| --- | --- |
| Account and service root | Effective UID and native account database lookup, independent of caller `HOME`, with fixed platform rules in the service child |
| Installation and invocation IDs | Independent 128 bit random values from `crypto/rand`, encoded as 32 lowercase hex characters |
| Bound paths and native definition identity | Validated install inputs and resolved local paths saved in the manifest |
| Login startup intent | Last acknowledged managed enable or disable operation; external manager overrides remain separate observations |
| Live managed ownership | Owner PID from the existing lifetime record lock plus checked native identity and matching invocation observations |
| Startup readiness | A new invocation relative to the captured baseline, with agreeing observation, native PID, and lock owner samples |
| Repeated uninstall identity | Retained removal receipt after acknowledged definition removal, validated against current native and file absence |
| Last runner phase, enabled lanes, and policy | Observation written by the current managed invocation |
| Last runs, outcome counts, and source failures | Existing saved work read in the status transaction |
| Due counts and next eligibility | Existing scheduling rows evaluated at the status snapshot's `as_of` |
| Last local validator wait | Saved scheduling wait and its original observation time, not a live provider probe |
| Stop and restart outcome | Native control result, service lock release, latest matching observation, and explicit unknowns |
| Log retention and gaps | Five file budget, invocation sequence, and reader offsets |
| Doctor readiness | Bounded local probes, each with its own result and observation time |

### Security and invariants

Use the current account and owner private files. Managed directories use `0700`; manifests, observations, definitions, and logs use `0600`. Refuse unexpected ownership, permissive managed files, symlinks at managed regular file paths, and unknown record versions. Existing executable symlinks may be resolved once during installation; the recorded executable is the resolved regular file path. Configuration keeps its existing prohibition on symlinks. Native manager created enablement links are the narrow platform exception, verified against the installed unit.

Native tools receive argument arrays without a shell. Diagnostics use controlled fields. No service command exposes keys. Doctor and status never instantiate a validating session. No command escalates privileges, enables Linux lingering, changes the host WARP registration, or removes validator recovery files.

### Critical scenarios

The executable procedure is in [verify.md](verify.md). The main thread to prove is install, explicit start, scheduled crawl, status from another process, stop, and saved inspection after restart. Later slices add both native managers, failure recovery, and complete diagnostics.

## Build plan

Use Tracer Bullet delivery and the Beta workflow. Each milestone proves a complete user path rather than a separate technical layer.

1. Prove one macOS collection service through install, explicit start, the shared runner, a private observation, status, stop, and saved inspection. Add the private service formats and no database migration. Covers AC-1, AC-2, AC-3, AC-6, AC-8, AC-15.
2. Prove that same path under a real Linux user manager. Complete login activation, enable and disable, repeatable controls, path binding, stopped uninstall, and native supervision policies. Covers AC-1, AC-2, AC-3, AC-4, AC-5, AC-16.
3. Complete status from one database snapshot, including the controlled 338 candidate collection, last runs, source failures, lane observations, missing components, text, and JSON. Covers AC-8, AC-9, AC-10, AC-11, AC-15.
4. Prove bounded private logs from the managed runner through recent inspection, following, rotation, and restart. Add read only doctor probes and diagnostic write failure handling. Covers AC-11, AC-12, AC-13, AC-14, AC-15.
5. Exercise crashes, permanent errors, duplicate service and writer ownership, configuration drift, interrupted control operations, uncertain publication, and the shared cleanup deadline on both platforms. Covers AC-3, AC-4, AC-5, AC-6, AC-7, AC-14, AC-16.
6. Run `/check verify background operation and diagnostics`, then `/test background operation and diagnostics`. Run the repository gate and target builds. Covers AC-1 through AC-16.

## Consequences

**Benefits**: You gain one operating interface across both platforms, useful diagnosis when state cannot open, and bounded local logging. The existing scheduler and remote recovery rules remain the work engine.

**Costs**: Native service behavior needs verification on both operating systems. Process and database observations cannot form one atomic snapshot. Bound executable and Docker paths need a new installation when moved. Retained logs can lose old evidence. On macOS, an inactive registered job whose loaded identity cannot be proven requires native inspection and removal before the CLI proceeds. This can occur after a permanent startup failure.

**Limits**: Login startup depends on the native user manager. macOS needs a GUI login domain; Linux session and lingering policy remain owned by the host. Power loss or forced termination cannot promise completed cleanup. A permanent error before the executable can run may only be visible through native manager diagnostics.

## Follow-up

1. Keep boot services, Linux lingering management, multiple named services, remote control, alerts, live configuration reload, configurable log retention, and log export bundles outside this slice.
2. Record the actually verified operating system and service manager versions during implementation. Unsupported native output or unavailable managers remain explicit, not guessed.
3. After implementation, `/sync` can record service commands, private formats, and verified lifecycle behavior in durable project context.

## Rationale

Reasoning and alternatives are in [rationale.md](rationale.md).
