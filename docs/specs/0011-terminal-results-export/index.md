# 0011. Terminal results and export

**Date**: 2026-10-15
**Status**: Proposed
**Content confirmed**: 2026-10-15, after independent review and application of both approved fixes. Implementation and acceptance verification remain pending.

## Summary

You can browse every saved candidate, inspect its sources and attempts, and export either the matching collection or candidates you marked. Results stay fixed until you refresh. Go prepares private snapshots on disk and creates complete export files, while Ink handles navigation and explicit key disclosure.

## Structure

1. [Results and details](0001-results-details.md) defines fixed snapshots, filters, marks, history, and screen behavior.
2. [File export](0002-file-export.md) defines selection, review, private preparation, publication, and cancellation.

This umbrella owns the shared process, protocol, access, and lifecycle contracts. [Verification](verify.md) defines the required evidence, without claiming implementation or acceptance.

## Requirements

**User story**: As the local operator, you can understand saved evidence and obtain the exact collection you selected without losing records or disclosing keys accidentally.

| ID | Acceptance criterion |
| --- | --- |
| AC-1 | Overview opens Results through the keyboard. Results initially includes all saved candidates in ascending ID order. All 338 fixture candidates are reachable exactly once, including records beyond page boundaries. Empty, absent, inaccessible, and incompatible state are distinct. |
| AC-2 | Latest outcome, last definitive result, and freshness filters use spec 0006 semantics. Every matching row remains reachable. Counts, summary fields, filter membership, and freshness use one fixed collection snapshot and evaluation until explicit refresh. Later writer commits cannot change that view. |
| AC-3 | Marks survive paging and detail navigation. Applying changed filters clears marks. Refresh preserves marks for candidate identities still matching, reports removed marks, and preserves focus by identity where possible. Preferences and marks end with the session. |
| AC-4 | Details show the saved summary, complete paged source provenance, and complete paged attempt history with outcome, reason, times, normalized evidence, backend version, and saved retry facts. Details use a separate fixed snapshot taken on opening, visibly distinguished from the results snapshot. Missing legacy facts are not invented. |
| AC-5 | Keys are absent from ordinary replies and hidden initially. Reveal sends only the current candidate key after an explicit action. Leaving details, refreshing, resizing below the usable minimum, or losing the channel hides it. Export disclosure is a separate explicit choice. |
| AC-6 | Results, details, filters, export review, progress, help, and outcomes are keyboard accessible using the existing compact design. Full IDs and state meaning remain reachable at 60 columns. No color, small terminals, text input, and hostile display characters preserve correct behavior. |
| AC-7 | You can export all matching candidates or the marked subset, without a row limit, as summary JSON or exact key lines to a path you enter. Review shows the exact count, filters, snapshot time, selection mode, format, disclosure, and resolved destination before creation. |
| AC-8 | Export membership and summary values come from the displayed fixed snapshot, including after a failed refresh. JSON preserves schema version 1 and its existing fields, with explicit subset metadata. An empty matching export succeeds. Key lines preserve exact stored values and reject unsafe line characters. |
| AC-9 | Named output retains spec 0006 private preparation, canonical path checks, atomic creation without replacement, and truthful publication facts. Existing files, directories, and symlinks survive races unchanged. Failure before publication creates no destination. Later failure retains any complete published file. |
| AC-10 | Long preparation leaves progress, cancel, help, resize, and quit responsive. There is one preparation job and one outstanding protocol request at a time. Cancellation waits for entered work before normal resource release. Publication uncertainty never triggers an automatic retry. |
| AC-11 | Failed refresh retains the previous snapshot, original evaluation, marks, and position with a visible retained label. Explicitly reviewed exports remain available. Failed detail preparation keeps the list summary and identifies the failed detail phase, without converting failure into empty history. |
| AC-12 | Memory is bounded by pages and individual records rather than collection size. Live database transactions close before interactive browsing or file publication. Temporary state is private, owned by the session, and removed after its users stop; cleanup failures remain visible. |
| AC-13 | Protocol version 2 has closed method and record sets, exact decimal IDs, bounded messages, and safe errors. Paths are admitted only for export review. Keys cross only the reveal reply. SQL, arbitrary commands, raw errors, provider output, and request tokens are never admitted. |
| AC-14 | Configuration resolves once. Reads neither initialize nor migrate state, acquire its writer, crawl, probe Docker, validate keys, nor control the service. CLI list and export behavior remains compatible. Overview refresh pauses while Results owns the screen and resumes on returning. |
| AC-15 | Quit cancels preparation and preserves publication facts. Terminal restoration, process ownership, and signal exit codes retain spec 0010 deadlines. A forced exit or lost result is explicitly uncertain about an export that may have reached publication. The service is untouched. |
| AC-16 | Both final Linux amd64 and arm64 archives pass native terminal checks, including the glibc 2.28 baseline, complete results, disclosure, file publication, cancellation, and existing command isolation. The Go gate and UI checks pass. Beta behavior verification precedes the meaningful regression test pass. |

## Decision

**Chosen option**: Extend the existing Go parent and Ink child with private disk snapshots, paged views, session marks, a dedicated reveal action, and reviewed file export. Reuse the pinned stack and existing export implementation. Add no dependency, live database migration, credential, configuration setting, or persistent export job.

This is scope feature 13. It extends spec 0010 with protocol version 2 and a narrow, explicit disclosure capability. The old overview remains key free. Spec 0006 continues to govern CLI output and file publication; its public JSON schema stays at version 1. The database stays at schema version 5. The worker protocol and validator registry do not change.

The internal recommendations are concrete. Use the existing SQLite driver for indexed temporary records instead of keeping the collection or marked ID set in RAM. Keep preparation in a cancellable parent job instead of lengthening every request deadline. Reuse CLI encoding and publication through an injected function instead of spawning a command or copying its implementation. These choices add session lifecycle work but keep one implementation of evidence and file safety.

## Feature design

### Package ownership

`internal/state` holds records without Cobra, SQL, or terminal dependencies. `internal/store` owns live read transactions and the private temporary SQLite tables used for snapshots, view order, marks, and detail pages. The temporary database is a disposable format with no migration contract. `internal/app` owns filtering, evaluation, snapshot and detail preparation, mark reconciliation, and session resource ownership.

`internal/cli` continues to own export encoding, private output staging, and publication. Refactor its existing helpers into a reusable concrete operation within that package. Inject that operation into the terminal session using a small function boundary with typed options, an immutable row source, and a typed receipt. Neither `internal/app` nor `internal/terminal` imports `internal/cli`.

`internal/terminal` owns request validation, dispatch, one background job, private pipes, watchdogs, and child supervision. Its job calls application or injected export operations; it contains no SQL, freshness policy, or JSON export encoder. `ui` owns rendering and keyboard interaction. The child never reads the database, temporary snapshots, or destination files and never launches a shell or browser.

No new package is required. The existing Node, Ink, React, TypeScript, and Go pins remain the implementation baseline. No additional skill or MCP discovery is requested.

### Shared session lifecycle

Results preparation, refresh, filter application, mark reconciliation, detail preparation, and file export can scan arbitrary numbers of records. Run them through one job slot. The states are `running`, `cancelling`, `succeeded`, `failed`, and `cancelled`. A terminal state is recorded only after entered work and ordinary cleanup finish. The parent allocates increasing positive decimal job IDs, snapshot IDs, view IDs, detail IDs, and review IDs, independently within this session. They are handles, not persisted identities or authorization tokens.

Keep one active results snapshot, one filtered view, one detail snapshot, and one export review. A refresh may hold one replacement while preserving the current snapshot. A replacement becomes active only after preparation and reader closure succeed. Old handles then become stale. Failure before activation leaves the prior view intact. Closing details discards its snapshot and invalidates its handle. Returning to Overview retains Results and marks for this session but closes details and reviews. Quit releases everything after cancelling work.

Each session change has one commit point. Open, refresh, and filter commit when the parent activates the complete replacement view; clear marks commits when its temporary transaction successfully clears the set and updates count and revision. Detail and review jobs commit when their complete handles become active. Check cancellation immediately before committing, and serialize activation with cancellation admission. Before commit, cancellation or failure preserves prior state, except that detail opening has already closed its previous detail by design. After commit, return `succeeded` with the committed result even if cancellation arrives during cleanup. Failure to remove superseded resources is a separate `warning_code`, never a claim that activation failed. Keep the cleanup retry barrier. If a temporary transaction's commit outcome cannot be established, end the session safely rather than returning a fabricated old or new view. File export keeps its distinct publication receipt contract.

The UI sends a job poll immediately after a start response, then 250 milliseconds after each `running` or `cancelling` response, including a cancel acknowledgement. Continue until a terminal result or session shutdown. Polls do not overlap. Progress is a count of prepared records, not an invented percentage. File export knows its expected row count; collection and detail scans may have no known total. The count is recorded by the worker only after a row is successfully prepared. Polling reads bounded in memory facts and never waits on the worker's database or filesystem call.

While a job runs, admit only its poll or cancel, plus session shutdown. Help and resizing are local. A second start or a normal page action returns `ui_busy` without ending a valid session. Cancellation is idempotent for the current job. Retain its terminal result until a new job starts. Do not restart a job after timeout, disconnect, or lost acknowledgement.

Keep the existing five second handshake and independent twelve second request watchdogs. A start response acknowledges job admission, not completion. These watchdogs cover each start, poll, cancel, and bounded page reply; they do not cap the total job duration. There is no arbitrary total collection deadline. The first effective cancel request starts a separate five second cancellation watchdog; repeated requests never reset it. If entered work cannot finish by then, terminate the session through the normal bounded shutdown path, without opening a replacement reader. Quit starts that shutdown immediately.

### Protocol version and methods

Increment the bundled private protocol to `2`, including the Go constant, UI schema, hello exchange, manifest value, package preparation, and compatibility tests. The manifest schema remains 1. Mixed protocol bundles fail admission; there is no negotiation or version 1 fallback. Version 2 retains the existing `overview` data shape. Hello retains the existing fields, with `protocol: 2` and capabilities exactly `['overview', 'results', 'details', 'reveal', 'export']` in that order.

Keep newline terminated UTF-8 JSON, 4 KiB requests, 1 MiB replies, increasing request IDs, one outstanding request, strict closed fields, and existing safe terminal ownership. Required fields reject null unless marked nullable. Domain IDs, offsets, counts, and durations use canonical decimal strings. All IDs are positive signed 64 bit domain values or positive session counters. Offsets and counts allow zero. Preserve the existing `Stamp` record and Go supplied local labels. Unknown methods or malformed records remain fatal protocol errors; valid requests with stale handles return safe application errors.

| Method | Exact params | Result |
| --- | --- | --- |
| `hello`, `overview` | Existing params, protocol changed to 2 | Existing result shapes with the hello changes above |
| `results.open` | `{}` | `JobStarted`, captures all candidates with empty filters |
| `results.refresh` | `view_id`, `focus_id: Decimal?` | `JobStarted`, preserves current filters and reconciles marks |
| `results.filter` | `view_id`, `filters: Filters` | `JobStarted`, applies filters to the existing snapshot |
| `results.page` | `view_id`, `offset` | `ResultPage` |
| `results.mark` | `view_id`, `candidate_id`, `marked: boolean` | `Marks`, sets one mark, never toggles blindly |
| `results.clear_marks` | `view_id` | `JobStarted`, clears the marked set |
| `details.open` | `view_id`, `candidate_id` | `JobStarted` |
| `details.page` | `detail_id`, `section: 'sources' or 'attempts'`, `offset` | `DetailPage` |
| `details.close` | `detail_id` | `Empty` |
| `candidate.reveal` | `view_id`, `detail_id`, `candidate_id` | `RevealedKey` |
| `export.review` | `view_id`, `selection: 'matching' or 'marked'`, `format: 'json' or 'keys'`, `include_keys: boolean`, `path: string` | `JobStarted`, returns an `ExportReview` on completion |
| `export.start` | `review_id` | `JobStarted` |
| `export.discard` | `review_id` | `Empty` |
| `job.poll` | `job_id` | `JobState` |
| `job.cancel` | `job_id` | `JobState` |

`Empty` is `{}`. An operation that requires a missing or stale resource returns `ui_handle_stale`; a candidate outside the view returns `ui_candidate_unavailable`. A mark or filter change invalidates any export review. An export review cannot authorize a different path, disclosure, format, view, or marked set. `export.start` consumes it exactly once.

`results.open` is admitted only when no Results snapshot exists. To revisit a retained snapshot, use its view ID. Reopening details first closes any previous detail. Detail refresh repeats `details.open` after clearing the previous key and detail. These rules keep resource replacement explicit.

A nonnull refresh focus must belong to the old view. `View.focus_id` and `focus_offset` are replacement guidance, applied only when installing a completed view job. Repeated page replies do not reset locally moved focus. Failed jobs preserve the previous position. Successful clear marks jobs return the current view without moving focus.

### Shared wire records

The child specs define `Filters`, `Summary`, `SourceRow`, `AttemptRow`, and export records. Every record below is closed. A question mark means an explicit nullable field. `Decimal` means the decimal string encoding above. Lists have their stated order. Error codes use the shared diagnostic catalogue; arbitrary text is never an error message.

| Record | Exact fields |
| --- | --- |
| `JobStarted` | `job_id: Decimal` |
| `JobState` | `job_id`, `kind: 'open' or 'refresh' or 'filter' or 'clear_marks' or 'details' or 'review' or 'export'`, `state`, `phase: 'collection' or 'view' or 'summary' or 'sources' or 'attempts' or 'destination' or 'encoding' or 'publication' or 'cleanup' or 'complete'`, `completed_count: Decimal`, `expected_count: Decimal?`, `error_code: Code?`, `warning_code: Code?`, `remaining_path: string?`, `result: JobResult?` |
| `JobResult` | Exactly one tagged variant: `{type:'view', value:View}`, `{type:'detail', value:Detail}`, `{type:'review', value:ExportReview}`, or `{type:'export', value:ExportReceipt}` |
| `View` | `snapshot_id`, `view_id`, `as_of: Stamp`, `max_age_ns: Decimal`, `filters: Filters`, `matching_count: Decimal`, `marked_count: Decimal`, `removed_marks: Decimal`, `focus_id: Decimal?`, `focus_offset: Decimal?`, `retained: boolean`, `error_code: Code?` |
| `ResultPage` | `view: View`, `offset: Decimal`, `items: ResultRow[0..50]`, `next_offset: Decimal?` |
| `ResultRow` | `summary: Summary`, `marked: boolean` |
| `Marks` | `view_id`, `candidate_id`, `marked: boolean`, `marked_count: Decimal` |
| `Detail` | `detail_id`, `view_id`, `candidate_id`, `as_of: Stamp`, `summary: Summary`, `source_count: Decimal`, `attempt_count: Decimal` |
| `DetailPage` | `detail_id`, `section`, `offset: Decimal`, `total: Decimal`, `items: SourceRow[0..50] or AttemptRow[0..50]`, `next_offset: Decimal?` |
| `RevealedKey` | `view_id`, `detail_id`, `candidate_id`, `key: string` |

`state` in `JobState` uses the lifecycle values above. A successful terminal job has its appropriate result and null error. A committed session result may also have `warning_code=ui_snapshot_cleanup_failed` and its remaining path. The UI installs that result first, then displays the cleanup warning separately; it must not retain an invalid old handle. All other warning codes are disallowed, and running, failed, cancelled, and export jobs have null warning. A failed or cancelled export may also carry its receipt, since failure does not erase publication. Other failed or cancelled jobs have null result. The phase identifies where failure occurred; `complete` is reserved for success, including success with a cleanup warning. A retained view has its last refresh error, original time, and `retained=true`; successful activation clears both even if cleanup warns. An initial missing collection fails the job with `state_not_initialized` and has no view.

Page offsets are zero based positions in the fixed ordered set, not live database offsets. Requests at the count return an empty page; offsets above it fail with `ui_page_invalid`. The server returns at most 50 rows and at most 768 KiB of encoded item data. It may return fewer rows to fit the byte budget. `next_offset` is the first unreturned position or null at exhaustion; it must advance whenever nonnull. The UI follows returned offsets, not a guessed page size. Bounded page access takes an independent five second read context within the twelve second request watchdog.

Only the reveal reply contains a key. A key whose encoded reply cannot fit 256 KiB is not shortened; return `ui_key_too_large` and offer exact file export. Dynamic nonsecret detail strings use `DisplayText {text:string, shortened:boolean}`. Limit each to 4096 UTF-8 bytes at a code point boundary before encoding and label shortening visibly. This is a display bound, not a stored data change. Apply terminal escaping after validation. Summary records have no arbitrary display strings.

### Errors, access, and shutdown

Reuse existing state, export, cancellation, and diagnostic codes. Add `ui_busy`, `ui_handle_stale`, `ui_candidate_unavailable`, `ui_page_invalid`, `ui_snapshot_failed`, `ui_snapshot_cleanup_failed`, `ui_collection_changed`, `ui_key_too_large`, `ui_export_selection_empty`, and `ui_export_outcome_unknown` to the shared code catalogue and UI mappings. Temporary storage failures use the snapshot code; live read failures retain their existing safe category. Cleanup paths are explicit, escaped metadata, never raw filesystem errors.

The current operating system account and existing private store admission remain the access boundary. Ordinary reads grant no disclosure. Reveal requires a matching open detail and candidate in the current view. Export review is the sole method accepting an output path. An explicit key line format requires `include_keys=true`; JSON defaults to false. No request can pass SQL, a candidate key, configuration, or a command to execute.

No routine polling or read events are logged. Existing diagnostics can record safe operation kind, counts, codes, and publication facts. Keys, snapshots, raw paths from errors, arbitrary backend text, and request tokens never enter diagnostics or service archives. Only safely quoted approved destination and remaining temporary paths may appear in final delivery diagnostics. The failed diagnostic stream latch remains authoritative.

Normal quit cancels the job, stops admission, and unmounts the UI. Retain spec 0010's independent two second child cleanup and terminal restoration allowance and five second process shutdown deadline. The first SIGINT or SIGTERM still fixes 130 or 143. A graceful user quit with successful cleanup returns 0 even when its job was intentionally cancelled. Cleanup failure, fatal protocol failure, or an uncertain export outcome returns 1 without a prior signal. Expected in screen operation errors do not by themselves make a later ordinary quit fail.

An export receipt is recorded in parent memory at the atomic publication point before any UI reply. After restoring the terminal, quit during export reports the resolved destination and known publication fact through the existing final diagnostic sink once. If shutdown forces exit while publication is entered but unfinished, report `ui_export_outcome_unknown` with that destination if the sink is usable; never claim no file exists. SIGKILL, power loss, or unusable diagnostics may prevent this report. The review screen therefore states that an interrupted result can require checking the destination, and no export is retried automatically.

### Value sourcing

| Value | Source |
| --- | --- |
| Profile, local time, color, release identity | Existing parent configuration resolution, clock, presentation policy, and manifest |
| Collection summary, evidence, freshness | Existing candidate and attempt records, fixed application evaluation, existing derivation rules |
| Filter membership and count | Canonical spec 0006 filters applied to the prepared snapshot |
| Position, marks, selected count | Temporary indexed view and unique candidate mark records |
| Detail times and history | New live read transaction at detail opening, existing sightings, attempts, and optional batch attempt detail rows |
| Key disclosure | Exact key held in the current private results snapshot for the requested candidate |
| Export count and membership | Immutable current view plus matching or marked selector captured by review |
| Export path | User input resolved by the existing parent destination policy from the invocation directory |
| Progress | Worker count of successfully prepared records, optional known export total |
| Publication and uncertainty | Parent owned atomic link outcome and subsequent sync, cancellation, and cleanup facts |
| Retained state | Failed refresh outcome plus the unchanged prior snapshot and evaluation |

### Critical test scenarios

The matrix in [verify.md](verify.md) maps each scenario to its acceptance criteria. The decisive cases are a concurrent writer during browsing and export, marks across changed filters and refreshed identities, history beyond page boundaries, a late reveal reply, a destination creation race, cancellation at publication, and a blocked operation during terminal shutdown.

## Build plan

The approach is Tracer Bullet and the workflow is Beta. Each milestone connects real saved data to the packaged terminal. There is no live schema migration.

1. Prove one complete Results path through protocol 2, private snapshot preparation, a real paged list, and one reviewed hidden key JSON file export. Reuse existing publication code and prove ordinary quit. Covers AC-1, AC-7 through AC-10, AC-12 through AC-16.
2. Complete all three filters, exact counts, every page, marks, explicit refresh, retained failure state, and focus reconciliation. Prove concurrent live writes cannot change the current view or its export. Covers AC-1 through AC-3, AC-6 through AC-8, AC-11, AC-12, AC-14.
3. Complete separate detail snapshots, all provenance and attempts, saved retry facts, explicit key reveal, display bounds, and identity changes. Covers AC-4 through AC-6, AC-11 through AC-14.
4. Complete marked subset exports, JSON disclosure and exact key lines, review invalidation, cancellation races, publication receipts, cleanup failure, and interrupted shutdown. Covers AC-3, AC-5, AC-7 through AC-13, AC-15.
5. Verify the full keyboard journey, failure matrix, CLI compatibility, and final native Linux archives. Then add meaningful Beta regression coverage and run the complete Go and UI gates. Covers AC-1 through AC-16.

## Consequences

You get stable browsing and reproducible exports while background writers continue. A separate detail time keeps newer history honest. Export remains an explicit local action with the same file guarantees as the CLI.

Preparing a collection now costs temporary disk space proportional to its rows, including private keys. Refresh briefly needs both old and new snapshots. Process crashes can leave private temporary directories, and filesystem deletion is not a secure erase promise. Detail facts can be newer than the list and need two clearly labeled times. A protocol bump requires a matching packaged executable and UI.

## Follow-up

1. During implementation, extend `ui/design.md` with the confirmed list, detail, and export patterns. Preserve the existing visual language.
2. After implementation, update the terminal, app, store, UI, and root context through `/sync`, including the narrow disclosure extension and protocol version. This design does not edit those context files.
3. Keep manual crawl and check controls in scope feature 14. This feature creates exports and temporary session files but never starts operational work.

## Rationale

Reasoning and alternatives are in [rationale.md](rationale.md).
