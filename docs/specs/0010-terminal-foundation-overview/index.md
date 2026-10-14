# 0010. Terminal foundation and overview

**Date**: 2026-10-14
**Status**: In Progress
**Content confirmed**: 2026-10-14, after independent review and application of the five recommended fixes. Implementation and runtime verification remain pending.

## Summary

You can open `wpc` in a supported terminal to see collection readiness, recent work, schedules, and service observations. Ink renders the screen while Go reads your existing private state. The Linux release includes its JavaScript runtime, and explicit commands keep their current behavior. Opening, refreshing, and leaving the overview never starts or stops background work.

## Structure

1. [Runtime and process connection](0001-runtime-bridge.md) defines startup, installation, the private protocol, and terminal ownership.
2. [Overview and saved reads](0002-overview.md) defines the screen, field sources, snapshot rules, and unavailable states.

Both children implement this one scoped feature. They share the requirements, version contract, and build sequence below. [Verification](verify.md) records the required proof without claiming it has passed.

## Requirements

**User story**: As the local operator, you can see whether your collection has fresh confirmed evidence, what recently happened, and which saved or service conditions need attention.

1. **AC-1**: Bare `wpc` launches the real Ink overview on supported Linux amd64 and arm64 terminals. Its selected configuration and state directory follow existing resolution rules.
2. **AC-2**: Redirected input or output, `TERM=dumb`, and unsupported host platforms produce deterministic plain root help without loading configuration, opening state, inspecting the service, or starting Node. Explicit help and version retain that isolation. Explicit commands retain their current output and exit contracts.
3. **AC-3**: Each Linux archive contains a matching Go executable, Node runtime, compiled UI, production dependencies, licenses, and version manifest. Installation works without host Node, npm, network access at startup, or a global UI installation. UI support requires glibc 2.28 or newer. Missing or incompatible UI files cause a safe startup error; explicit commands remain usable.
4. **AC-4**: The overview leads with fresh confirmed and total candidates. It also shows stale confirmations, unknown freshness, and latest completed outcome counts. Its evaluation matches existing candidate evidence rules, including a later inconclusive check, an exact freshness boundary, and future evidence times. Totals cover the complete collection.
5. **AC-5**: Recent work shows the latest ten saved runs across crawl and validation in descending run ID order. It labels kind, manual or scheduled origin where known, saved status, times, and applicable result counts. A further saved row produces a visible limited history notice. Legacy missing facts are not invented.
6. **AC-6**: Collection totals, recent work, and schedules come from one database snapshot and evaluation time. Saved schedule eligibility, pauses, and waits are distinct from proof of a live runner. Service installation, manager, and runner observations retain their independent availability and times.
7. **AC-7**: The selected configuration supplies the collection even when the installed service uses another directory. The service section labels that difference. A missing or unreadable service does not hide available collection data, and collection failure does not hide available service observations.
8. **AC-8**: The first refresh begins on entry. Later automatic refreshes begin five seconds after completion of the previous refresh. `r` requests refresh without overlapping reads. Focus and scroll position survive refresh, with clamping when content shrinks.
9. **AC-9**: Loading, initialized empty state, absent state, unavailable data, stale retained data, and clock anomalies are distinct. Failed sections retain only clearly marked previous successful data. No failure becomes a zero count or healthy status. Absent state offers suggested CLI commands without creating files or starting work.
10. **AC-10**: One scrollable compact screen supports section focus, keyboard scrolling, shortcuts, and alternate screen restoration. At less than 60 columns or 16 rows it shows a resize notice while quit, refresh, and help remain usable. Resizing restores content and a valid focus position.
11. **AC-11**: `--color never` and a nonempty `NO_COLOR` remove decorative color. Text labels, focus markers, and headings preserve meaning without color. Dynamic text cannot inject terminal controls. English text and terminal characters require no downloaded fonts or image assets.
12. **AC-12**: Go and Ink communicate through a bounded versioned private pipe protocol. Keys, raw configuration, arbitrary error chains, provider content, and request tokens never cross it. Malformed, oversized, mismatched, or interrupted protocol traffic fails safely without restarting the UI in a loop.
13. **AC-13**: Only the UI renders while it owns the terminal. Normal quit returns 0, Ctrl+C or SIGINT returns 130, and SIGTERM returns 143. Failure returns 1. Shutdown restores the usable terminal after at most two seconds of child cleanup and ends the parent within five seconds even when read cleanup stalls. Normal shutdown closes readers; forced process exit releases remaining process resources without claiming graceful reader closure. Unrelated processes and the background service remain untouched.
14. **AC-14**: Overview reads perform no application state initialization, migration, recovery, writer acquisition, source retrieval, Docker probe, registration, service control, or new persisted UI state. They work alongside the existing writer and retain private path and account checks.
15. **AC-15**: Real packaged startup, navigation, refresh, failure handling, terminal restoration, and offline installation are verified on both Linux architectures, including the glibc 2.28 userland baseline. Existing Go checks and new UI checks pass. Compilation, a current distribution alone, or upstream documentation alone does not establish acceptance.

## Decision

**Chosen option**: Ship Ink with a bundled Node runtime and TypeScript compiled modules. Keep the Go process as parent, configuration authority, and saved read coordinator. Give its UI child two private pipes in addition to the terminal streams. (basis: your confirmed choices, [scope feature 12](../../scope/scope.md), and the existing application and store boundaries)

This extends spec 0001 only for bare terminal startup and interactive release contents. Its single Go executable remains the explicit command and service entry point. Specs 0002 through 0009 retain their storage, evidence, scheduling, service, privacy, and diagnostic contracts. This does not supersede their completed features.

**Implementation recommendations**: Use one snapshot read per collection refresh, decimal strings for large integer transport values, a closed protocol schema, and production modules with their assets preserved. These choices prevent inconsistent counts, JavaScript precision loss, unintended data exposure, and missing dependency assets. The alternatives and costs are in the rationale.

No additional community skill or MCP discovery is requested. Your recorded decline remains in force.

## Feature design

### Shared boundaries

`cmd/wpc` continues to own process signals, exit, and terminal streams. `internal/cli` owns Cobra dispatch and safe final diagnostics. A concrete `internal/terminal` package owns child process setup, protocol validation, and terminal lifecycle. `internal/app` coordinates overview reads; `internal/store` owns all SQL; `internal/state` holds domain summaries. `internal/service` supplies account service observations without controls or Docker. UI source lives in `ui/` with its own npm manifest and lockfile.

These packages appear when their first working path needs them. Do not move business rules into TypeScript or turn the private channel into a public API. No database migration, new credentials, listening socket, or persistent UI preferences are part of this feature.

### Consistency and identity

One overview reply contains a collection component and a service group with installation, manager, and runner components. Collection content is atomic within its snapshot. Service evidence is observed independently and can be newer or older. Each component carries availability, its observation time, a safe error code when unavailable, and either complete data or null. The service group always carries identity and cache invalidation metadata, even when none of its data is available. The UI retains a previous successful component only in its own memory.

The parent resolves configuration once. Collection refresh does not apply edited configuration. Restarting the UI applies edits. Selected paths stay fixed for the session. Service installation identity is rechecked on every service observation under the existing service rules; evidence from a changed installation cannot be presented as current evidence for the new installation.

### Security and access

The effective local account and the existing private file checks are the access boundary. The UI is trusted shipped code, but its protocol is still restricted to the two read methods in the runtime child spec. Neither method accepts a path, SQL, command, candidate key, or arbitrary file name from JavaScript.

No compliance or hosted identity system is added. No routine refresh logs or service archives are written. Existing diagnostic options govern safe startup and final errors, and a failed diagnostic stream is never retried.

### Critical test scenarios

The scenarios in [verify.md](verify.md) cover every acceptance criterion. Particularly important are fresh evidence after an inconclusive result, collection totals above 100 and IDs above JavaScript integer precision, a service bound elsewhere, concurrent writer commits during refresh, a UI crash in raw terminal mode, and a release with no host Node installation.

## Build plan

The project uses Tracer Bullet and Beta. Prove real data through the complete package and terminal path before expanding the screen. No schema migration is scheduled.

1. Build the smallest installable path on both Linux architectures: the Go root admission check, pinned Node and Ink package, handshake, one actual total from a reader, terminal rendering, and normal quit. Record the exact tested dependency versions and archive layout. This settles runtime compatibility with real evidence. Covers AC-1 through AC-3, AC-12 through AC-15.
2. Extend that same read path to the complete collection snapshot: evidence totals, ten recent runs, saved schedule facts, and missing or empty state. Render the real readiness and activity sections without fixture data in production. Covers AC-4 through AC-6, AC-9, AC-14.
3. Add independently observed service components, profile comparison, bounded refresh, retained stale data, and safe component failures. Prove a running writer can continue while the overview refreshes. Covers AC-6 through AC-9, AC-12, AC-14.
4. Complete the compact screen, focus, help, resize behavior, no color presentation, protocol fault containment, and signal or crash cleanup. Covers AC-8 through AC-13.
5. Integrate UI compilation, lockfile installation, package checks, and native terminal verification into the existing release gate. Verify both offline archives and existing commands, then add meaningful regressions from the behavior checks. Run the Go gate and UI checks, retaining platform evidence. Covers AC-1 through AC-15.

## Consequences

You gain a useful view of the existing collection without operating another service or installing Node yourself. The same Go rules supply CLI and UI results. The protocol can gain separately specified actions later without granting mutation access now.

Archives become larger and must be installed as a directory tree. Runtime and JavaScript dependency updates become release responsibilities. The interactive UI has a glibc requirement that the Go executable did not previously impose. A five second refresh interval is a sampling policy, so the view is not a live event stream.

The first view contains only the ten most recent runs, with an explicit notice when more exist. It does not replace complete reports or result commands. Results browsing, export, manual work controls, configuration editing, and service lifecycle controls remain separate scope items.

## Follow-up

1. Record exact package pins, checksums, licenses, and tested runtime versions during the first build slice. A failed compatibility check requires a documented adjustment before extending the UI, not an unrecorded runtime switch.
2. After implementation, sync the new UI and release conventions into the appropriate project context. This architecture pass does not edit `AGENTS.md`.
3. Design scope features 13 and 14 separately. This feature grants no key disclosure or mutation capability to the UI.

## Rationale

Reasoning, alternatives, and verified references are in [rationale.md](rationale.md).
