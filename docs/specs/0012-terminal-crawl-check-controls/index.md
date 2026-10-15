# 0012. Terminal crawl and check controls

**Date**: 2026-10-15
**Status**: In Progress
**Content confirmed**: 2026-10-15, after independent review, application of both approved fixes, and a focused recheck. Implementation and acceptance verification remain pending.

## Summary

You can review and start a crawl or a batch of selected checks from the terminal, follow its progress, and cancel it. Go runs one manual operation at a time through the existing application engines while you browse or export saved results. Completed work stays in the existing history, and leaving the interface preserves the validator cleanup allowance.

## Structure

1. [Operation ownership and protocol](0001-operation-lifecycle.md) defines review, admission, observations, cancellation, and the private connection.
2. [Controls and saved reports](0002-controls-reports.md) defines screens, selection, complete report snapshots, and their wire records.

These children implement scope feature 14. This index owns their shared requirements, data model, process deadlines, and build sequence. [Verification](verify.md) specifies required evidence and does not claim that it has passed.

## Requirements

**User story**: As the local operator, you can deliberately run collection and validation, understand what was saved or left unfinished, and inspect the outcome later without losing access to your collection.

| ID | Acceptance criterion |
| --- | --- |
| AC-1 | Opening the terminal, browsing, preparing a review, and reading reports never acquire a writer, contact Telegram or Docker, or change scheduling. A reviewed crawl can initialize absent collection state. Explicit help, version, commands, and unsupported terminal admission retain their existing behavior. |
| AC-2 | Crawl setup lists configured sources in configuration order, distinguishes disabled sources, initially selects all enabled sources, and permits a nonempty enabled subset plus an optional common page limit from 1 through 100. Review shows the exact ordered plan. Overrides apply only to this run. A crawl never starts validation. |
| AC-3 | Checks accept the focused Results candidate, its marked subset, or every matching candidate, including all 338 fixture records. Review freezes exact membership, count, order, collection identity, and candidate identities. A changed or missing candidate rejects the whole new batch before validator acquisition. Existing writer recovery may precede candidate comparison. |
| AC-4 | Check review shows the configured effective policy and backend readiness. The terminal never changes check policy, silently enables Docker, substitutes a host registration, or silently reduces a selection for a provider probe. Existing worker, attempt, pacing, timeout, retry, cooldown, and recovery rules remain authoritative. |
| AC-5 | Only explicit Start consumes a valid review. Selection edits invalidate it. Repeated activation, lost replies, busy starts, and stale handles cannot create replacement or duplicate work. One terminal operation owns its resources until its engine and entered cleanup finish; acknowledgement of its outcome is required before another start. |
| AC-6 | Progress shows committed counts, current activity, retry waits, cancellation, and cleanup separately. Concurrent check workers remain visible within their configured limit. Partial success, inconclusive evidence, invalid evidence, unstarted work, rate limits, and uncertain persistence never collapse into a single success or invalid label. |
| AC-7 | You can browse Overview, Results, and Details, refresh explicitly, and export the fixed Results snapshot while work runs. An active work or outcome indicator stays visible. Completion never steals focus. Export and operation cancellation and outcome facts remain independent. Cancel work and Cancel preparation are independently reachable throughout any preparation, including help, text entry, and small terminals. |
| AC-8 | Cancel requests an immediate stop without a confirmation screen and is idempotent. It stops new admission, preserves eligible committed evidence, and shows bounded cleanup. It never releases ownership early, restarts a failed operation, resumes unused attempts, or equates cancellation with no saved work. |
| AC-9 | Pressing q with active work offers Stay and Cancel work and quit, initially focused on Stay. Accepted quit, Ctrl+C, signals, protocol loss, and child failure cancel owned work without detaching it. Active work retains the existing 60 second remote cleanup and five second finalization allowance within a 75 second hard process limit. Terminal restoration remains independent of cleanup. |
| AC-10 | A competing writer prevents the new operation. Fresh verified evidence that the installed service owns this collection permits advisory `wpc service stop` guidance. Unknown, stale, different, or foreground ownership receives truthful generic guidance. No service is stopped, signalled, or sent work by the UI. |
| AC-11 | Recent runs and an entered older run ID open complete saved reports without Docker, writer ownership, migration, or recovery. Sources, pages, selected items, and attempts remain reachable through bounded pages. Legacy unavailable fields stay unavailable. Saved reports still work after restarting the terminal. |
| AC-12 | A report uses one fixed collection snapshot and observation time until explicit refresh. Failed refresh retains the old report with a visible label. Counts and pages never mix snapshots. Replacing the collection invalidates its handles without mutating the replacement. |
| AC-13 | Controls, reviews, progress, reports, errors, and quit confirmation work with the keyboard, no color, and a 60 column by 16 row terminal. Smaller windows retain cancellation, help, quit, and disclosure clearing. Text entry cannot trigger printable shortcuts. |
| AC-14 | Ordinary replies, progress, reports, diagnostics, and errors contain no candidate keys, request tokens, raw provider text, or validator session identifiers. Existing current candidate reveal and export disclosure remain the only disclosure paths. All new records are closed and validated, with exact decimal IDs and counts. |
| AC-15 | The parent retains acknowledged run identity, uncertain write facts, cleanup state, and independent export receipts through reply loss and shutdown. Later work cannot erase an unresolved operation receipt. Only authoritative recovery under exclusive validator ownership clears its cleanup block. Uncertain SQL stops further writes in this session. Unknown remote cleanup blocks further attachments. Final diagnostics identify each outstanding originating run when known, occur only after restoration, use safe typed facts, and never retry a failed stream. |
| AC-16 | Matching protocol 3 release archives pass the real terminal journeys on native Linux amd64 and arm64, including the existing glibc 2.28 baseline. CLI compatibility, Go and UI gates, interruption, ownership conflicts, complete reports, and live validator cleanup have explicit evidence. No live database or validator registry migration is introduced. |

## Decision

**Chosen option**: Extend the existing terminal and application boundaries in place. Go owns one manual operation, Ink renders typed snapshots, and the current source and validator engines execute the work.

Use protocol 3 and the existing private pipes. Poll active work one second after the preceding operation poll completes, through the shared request scheduler. Do not introduce a daemon, service command channel, event stream, task queue, new dependency, or configuration layer. Keep the existing Go, Ink, React, Node, pnpm, SQLite, and Docker pins.

This spec extends specs 0004, 0005, 0007, 0009, 0010, and 0011. Their explicit command, saved evidence, private storage, export, and packaging contracts remain in force. The extensions are terminal initiated manual work, new report reads, protocol 3, and the active operation shutdown window. It does not replace the earlier specs or alter their historical verification claims.

### Package responsibilities

| Area | Responsibility |
| --- | --- |
| `cmd/wpc` | Signals, exit codes, independent terminal restoration reporting and hard process deadline |
| `internal/cli` | Resolve configuration once, construct source and validator adapters lazily, connect typed work and export boundaries, render final diagnostics |
| `internal/app` | Review selection, own a manual operation, reuse crawl and check cores, emit typed observations, prepare report snapshots |
| `internal/store` | Writer admission and collection identity checks, durable history, single transaction report traversal, private temporary report storage |
| `internal/terminal` | Closed protocol, session handles, preparation jobs, operation receipts, request admission, child supervision |
| `ui` | Forms, review, progress, report navigation, focus, local input intent, validated rendering |

The terminal application receives the same Telegram adapter and configured validator as explicit commands. Adapter construction does not perform external work. Docker admission, recovery, registration, and attachment occur only after Start through the existing engine. `validator=unsupported` disables Start check with setup guidance. Scheduled `validation_enabled` controls the runner, not an explicit manual check. No settings reload occurs during the session.

### Data model

All durable entities retain schema version 5 and existing constraints. Temporary records belong to the Go session and are never a queue or restart mechanism.

| Entity | Key and relationship | Fields and constraints |
| --- | --- | --- |
| Run | Existing run ID | Existing kind, `manual` origin, status, times, nullable reason and recovery facts |
| Crawl source and page | Run has ordered sources; source has numbered pages | Existing immutable selected plan, page request and parse facts, status, counts, and reasons |
| Validation batch | One batch per validation run | Existing policy snapshot and backend identity |
| Validation item | Unique run and candidate pair, unique ordinal within run | Existing selection order, disposition, attempt count, optional latest attempt |
| Attempt | Existing attempt ID, linked run and candidate | Existing evidence; optional batch attempt number and retry facts |
| Work review | Session review ID | Required kind and creation time; crawl plan or nonempty ordered check membership; effective policy; collection binding; nullable Results view and mark revision; consumed flag |
| Operation | Session operation ID | Required review ID, kind, revision, lifecycle state, phase, times and cleanup facts; nullable acknowledged run ID, run status and error; at most one unacknowledged operation |
| Unresolved cleanup receipt | Originating session operation ID, nullable acknowledged run ID | Original typed receipt plus private validator scope and obligation bindings; retained independently until authoritative recovery proves resolution |
| Report snapshot | Session report ID, bound to one collection and run | Fixed observation time and header, indexed section rows, exact counts; at most one active report plus one replacement under preparation |

Session IDs are increasing positive integers encoded as decimal strings. They are never reusable after release. A review retains ordered candidate IDs and exact key identity only in private Go memory; keys never cross the protocol for a check. There is at most one work review. A new review replaces the old one only after successful preparation and cancellation admission. Any edit, leaving the review flow, view replacement, or mark change invalidates the applicable review before a start can be sent.

After Start, membership and policy belong to the operation independently of later view or mark changes. The existing check coordinator retains memory proportional to the selected candidates; this feature adds no arbitrary batch cap and does not promise constant Go memory. UI replies and report browsing remain bounded. Report snapshots use private disposable SQLite storage, with directories `0700` and files `0600`, outside live state. Their size follows report history. Live readers close before a prepared report becomes visible.

### Resource ownership and interaction

There is one preparation job, one operational slot, one Results session, and one saved report session. The preparation job extends spec 0011 for work reviews and reports. Operation execution is separate from that job so a running check cannot block browsing or export preparation.

Start requires a valid review, no active preparation job, an empty operational slot, and no latched unsafe write condition. It reserves the operation and consumes the review before dispatch. Work retains its slot through cancellation, engine completion, reader and writer closure, and outcome acknowledgement. Another start returns `ui_busy` without acquiring resources. Work review preparation during active work is rejected; browsing, reports, and exports remain permitted.

Cancelling a preparation job never cancels an operation. Cancelling an operation never cancels export. Session shutdown cancels both and preserves both receipts. A preparation cancellation timeout still ends the session as in spec 0011; it does not shorten an already admitted operation's cleanup allowance. No request starts a replacement reader, writer, or worker while earlier entered work still owns those resources.

### Shutdown and final facts

The process owns a thread safe operation guard before any mutation admission. The guard supplies the process watchdog with the active operation's first stop time, if any. Arm and disarm it atomically with admission and completed resource closure, so a signal racing Start cannot choose the read only deadline after mutation begins.

The first operation cancellation fixes its monotonic stop time. Give remote cleanup at most 60 seconds and local finalization at most five seconds after remote cleanup, capped at 65 seconds from stop. Crawls have no remote allowance to consume and use the existing five second local abort context. At 75 seconds from the first operation stop, force session termination if owned work or resource closure remains stuck. This last allowance covers entered store closure and process reporting; it permits no extra remote mutation, retry, or finalization writes.

Session shutdown without owned work retains the five second deadline. With an operation guard, use the earlier of 75 seconds after session shutdown and 75 seconds after any earlier operation stop. Repeated cancellation, signals, and failures cannot extend either deadline. Once a shutdown deadline is fixed, disarming the guard cannot restart it. Cancelling work while staying in the UI disarms its deadline only after all resources close; subsequent ordinary session shutdown uses the normal five seconds.

On accepted quit, a signal, channel loss, or child failure, close admission and begin cancellation immediately. The child still receives at most two seconds to unmount; parent restoration does not wait for the operation. Parent and child may not print concurrently. A normal Cancel keeps the UI available to show cleanup. A confirmed quit restores the terminal promptly while Go completes cleanup, then emits final facts through the existing diagnostic sink once.

SIGINT, including forwarded Ctrl+C, fixes exit 130; SIGTERM fixes 143. Signal exits bypass quit confirmation. Normal quit after intentional cancellation and successful cleanup returns 0. An unresolved cleanup obligation, uncertain operation persistence, fatal connection failure, or forced shutdown returns 1 without an earlier signal. An acknowledged ordinary source failure or inconclusive check does not by itself make a later quit fail.

Add a safe `terminal_work` diagnostic record that bypasses level filtering, parallel to `terminal_export`. Fields are `operation_kind`, nullable acknowledged `run_id`, `run_created` (`true`, `false`, or `unknown`), nullable saved `run_status`, `cancellation_requested`, `persistence` (`known` or `uncertain`), `cleanup` (`not_needed`, `pending`, `complete`, or `unresolved`), and nullable safe `code`. The record uses the existing envelope and time source. No review ID, request token, key, path, or provider output enters it. `false` means this new run was not created; it does not deny recovery of older runs.

The parent records facts before making them available to the child. A lost Start reply or interrupted run creation can leave `run_created=unknown`; an acknowledged run ID never disappears. A known committed outcome wins a cancellation race, although cancellation and cleanup facts remain separate. An operation is not successful merely because its execution goroutine returned. Late uncertain writes and incomplete closure remain failures. Final output retains both work and export facts and never retries a failed diagnostic destination. SIGKILL, power loss, or unusable diagnostics may prevent a final report.

Keep an unresolved cleanup receipt independently of the latest ordinary receipt. A successful crawl, outcome acknowledgement, report read, empty registry read, or external command's apparent success cannot remove it. The operation child defines the authoritative recovery proof and latch clearing. Final rendering emits the outstanding receipt and the latest ordinary receipt when their originating operation IDs differ, then the independent export receipt when applicable. Emit a shared origin only once, with its strongest remaining failure facts. The existing failed stream latch stops all later records after the first failed write. Unknown run IDs remain explicitly unknown; a newer successful run is never substituted as the cause of an older failure.

### Security and invariants

The existing operating system account, private configuration, store ownership, local Unix Docker endpoint, immutable worker image, and shared validator ownership remain the access boundary. There is no new user identity, permission role, public API, secret, or compliance scope. The UI never supplies a key, raw configuration, command, SQL statement, validator session, or arbitrary provider endpoint. Only export review continues to accept a destination path.

Ordinary wire records contain safe typed values and bounded escaped display text. The existing reveal reply remains the only interactive key reply. Clear disclosure before opening work setup, review, progress, a report, or quit confirmation, as well as under spec 0011's existing rules. In flight reveal intent is invalidated locally before queued navigation or cancellation, so late replies cannot reveal a key on another screen.

### Value sourcing

| Action | Produced or displayed value | Source |
| --- | --- | --- |
| Setup | Ordered sources, enabled flags, configured limits | Parent resolved `Settings.Sources`, including validated disabled entries |
| Crawl review | Exact source plan and page budget | Enabled selection and optional override passed through `SelectSources`; budget is the sum of selected limits, not a promised fetched page count |
| Check review | Mode, order, count, snapshot time and filters | Current Results snapshot, view, marks and focused candidate; rows ordered by view ordinal |
| Admission | Candidate identity and collection identity | Private reviewed key bytes, canonical collection directory and database file identity, compared under writer ownership |
| Review and progress | Policy and backend | Resolved `Settings.CheckPolicy`, validator configuration, acknowledged saved batch policy, and explicit probe admission facts |
| Progress | Run ID, saved counts and terminal status | Acknowledged mutations and saved report reads from the existing application and store, with observer revision |
| Activity | Source/page or candidate/attempt and phase | Typed source and batch coordinator transitions; phase is activity, never evidence of a committed result |
| Wait display | Remaining wait and retry time | Coordinator monotonic deadline and committed retry decision; stored wall time stays separately labeled |
| Reports | Header, sources, pages, items, attempts, reasons and evidence | One established read transaction over existing history, streamed into temporary indexed report rows |
| Guidance | Busy, rate limit, probe, recovery or unsupported explanation | Closed error code plus applicable typed engine facts; optional independently verified service observations |
| Review validity | Handle, collection binding, view and mark revision | Parent session owner and Results session; frontend local generation prevents late intent activation |
| Outcome | Creation, persistence and cleanup certainty | Parent receipt updated at admission, acknowledged commits, engine stop, cleanup, and closure boundaries |
| Outstanding cleanup | Originating operation, optional saved run, and unresolved reason | Independently retained parent receipt; resolution comes only from typed proof after validator reconciliation and acknowledged journal persistence under exclusive ownership |
| Display | Times, ages, formatting, dimensions and no color | Existing `Stamp`, parent clock, UI monotonic receive time, terminal dimensions, and presentation policy |

## Build plan

The approach is Tracer Bullet and the workflow is Beta. Each milestone connects the real application path to the packaged UI. There is no live database migration.

1. Prove protocol 3 and one configured source crawl from setup through review, Start, saved progress, cancellation, and outcome in the packaged terminal. Establish operation admission, typed observations, receipts, and process deadlines together. Covers AC-1, AC-2, AC-5, AC-6, AC-8, AC-9, AC-14, AC-15.
2. Prove one focused candidate check through the existing Docker engine, reviewed policy, identity verification under the writer, real attempt progress, cleanup, and saved outcome. Complete writer and validator conflict guidance and late acknowledgement handling. Covers AC-3 through AC-6, AC-8 through AC-10, AC-14, AC-15.
3. Expand selection to marked and all matching candidates, all configured source subsets, page overrides, concurrent worker activity, retry and cooldown states, and partial outcomes. Preserve browsing, report preparation, independent export, and fair request scheduling. Covers AC-2 through AC-8, AC-10, AC-13 through AC-15.
4. Complete fixed saved report snapshots, recent run entry, older ID entry, every bounded source, page, item and attempt page, legacy facts, retained refresh failures, and outcome acknowledgement. Covers AC-6, AC-7, AC-11 through AC-15.
5. Complete compact keyboard journeys, small terminal and signal behavior, hostile data, uncertainty and shutdown fault cases. Verify both final native archives and explicit command compatibility, then add meaningful Beta regression coverage and run the required Go and UI gates. Covers AC-1 through AC-16.

## Consequences

You get deliberate manual work and complete saved outcomes through the existing operational engines. Background ownership remains explicit and predictable. Polling gives a bounded current view rather than a replay of every intermediate event.

You must stop a competing background runner outside the UI. Check memory still grows with selection, and report snapshots consume temporary disk space. Quitting active work can keep the Go process alive for cleanup after the terminal is restored. Protocol 3 requires matching executable and UI bundles, but rollback to the prior complete bundle needs no data conversion.

## Follow-up

1. During implementation, extend `ui/design.md` with the confirmed work, review, report, and quit patterns. Reuse its existing components and visual rules.
2. After implementation, reconcile root, app, store, terminal, diagnostic, and UI context through the normal sync workflow. This spec does not edit those context files.
3. Retain separate evidence for controlled failure probes and real Telegram, WARP entitlement, and remote deletion. Existing evidence from older worker packages does not prove the current pin.

## Rationale

Reasoning, alternatives, and the implementation inventory are in [rationale.md](rationale.md).
