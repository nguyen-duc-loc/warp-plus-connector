# Operation ownership and protocol

## Summary

You review a fixed plan before Go starts work. Go keeps the operation alive through cancellation and cleanup while the UI polls small typed observations. A lost reply never authorizes repeating an operation.

## Review and admission

The shared requirements and process deadlines are in `index.md`. Use the existing `App.Crawl`, `App.CheckBatch`, `crawlOwned`, and `checkOwned` cores through a concrete manual operation coordinator in `internal/app`. Extract shared setup where needed; do not invoke Cobra handlers, start a second executable, call `App.Run`, or recreate retry scheduling in the terminal bridge.

`work.options` reads resolved configuration only. It returns every configured source, including disabled ones. Crawl review calls the existing source selection rules, preserves configuration order, rejects empty or disabled selection, and applies an optional common limit. Review is available before a collection exists. Record whether the collection was absent or bind its canonical directory and database file identity when present; this metadata read does not initialize state.

Check review requires an active Results view and a nonempty selection. Focused selection has exactly its supplied candidate ID. Marked and matching selection follow ascending view ordinals and ignore the visible page boundary. Capture view ID, snapshot time, filters, mark revision when applicable, ordered candidate IDs, exact key identity, and collection identity. Snapshot enumeration is cancellable preparation. Failure or cancellation preserves the previous valid review until its own invalidation rule applies. Temporary memory failure is an explicit failed preparation, never truncation.

Check review is unavailable when the configured validator is unsupported. It exposes the configured immutable image as bounded display text, workers, attempts, pacing, and candidate timeout. It does not claim that Docker is reachable, an image is installed, or remote recovery is complete. Those facts are learned after Start. Keep the full effective policy for later saved reporting. Review explains that writer admission can recover older interrupted records and checks can reconcile existing validator obligations before new attachments.

Start atomically validates the review, reserves the operation slot, consumes the review, and activates the process guard, then starts asynchronous work. It returns promptly without waiting for a writer, network, or Docker. No operation is dispatched on a failed slot reservation. The callback or reply write is not the owner of execution. Repeated Start with a consumed review is a stale handle error, not a replay or new operation. The UI records start intent before sending and never retries it after timeout or pipe failure.

Before mutation, the operation verifies its cancellation gate and obtains the existing exclusive writer lock. Extend store admission with an expected collection binding checked while that lock is held and before initialization, migration, or recovery. This prevents a stale review from recovering or writing a replacement database. An absent crawl binding permits creation only if state is still absent; if a collection appeared since review, require a fresh review. It is not permission to replace an invalid directory. Normal store privacy and alias checks remain authoritative.

For checks, normal writer recovery may now run. Resolve every selected candidate under this same writer and compare its exact key with the reviewed identity. A mismatch creates no new batch, acquires no validator owner, and causes no new external action. It may follow recovery of earlier records, as disclosed in review. Do not rely on a separate read before writer acquisition for this comparison. No transaction spans a network request.

After identity checks, reuse the existing shared validator acquisition and saved batch admission. If provider cooldown requires a probe, require exactly one candidate, apply the existing one worker and one attempt probe policy, and show the saved effective policy in progress. A larger selection receives `validator_probe_required` and explicit guidance to review one candidate. Never silently choose one candidate or bypass the cooldown. All other configured policy bounds and saved retry classifications remain unchanged.

For crawling, reuse schedule preparation and immutable crawl plan creation before fetching. A fresh explicit crawl always starts current preview pages; it never resumes a cursor from another run. Configuration order, source pacing, source failure continuation, rate limit abort, and message commit rules stay with the existing engine.

## Lifecycle and observations

The operation lifecycle is `running`, `cancelling`, then `succeeded`, `failed`, or `cancelled`, followed by acknowledgement and release. `succeeded` means the engine completed without a command error and its resources closed. A completed check with inconclusive items remains `failed`, while its saved run can be `completed`. Show these separately. Cancellation with proven finalization is `cancelled`; uncertain persistence or unresolved cleanup is `failed` even if cancellation was requested. A committed successful result that won before cancellation remains successful.

Phases are `admission`, `recovery`, `crawl`, `check`, `retry_wait`, `cleanup`, `finalization`, and `complete`. These are live observations, not new durable run statuses. The final phase is `complete` only after owned resources close. Completion can include a failed or cancelled result. Each mutation and typed engine transition increments a monotonic revision under the operation owner. Readers copy one closed observation without waiting for network or store work.

Add a narrow typed observer to the shared operation cores. The application coordinator publishes acknowledged run creation, committed source/page/message counts, batch item and attempt transitions, retry decisions, and stop/cleanup phases. Its observer writes to bounded parent memory and never renders or performs pipe I/O. Do not derive progress by parsing zerolog lines or arbitrarily polling a writer connection from another goroutine. Routine polling emits no diagnostics.

Maintain crawl counters as the existing engine acknowledges commits, respecting distinct run candidate counts rather than summing page counts. If computing a distinct count requires a store query, use the operation's serialized store boundary. Count successfully committed sightings separately. Page intent is an issued intent, not proof that Telegram received a request. Label the current source and page as activity. After a lost mutation acknowledgement, preserve last acknowledged counts, set persistence uncertain, and stop further SQL. An optional bounded final read can add authoritative facts but cannot authorize another write.

Check progress includes the existing `ValidationTotals`, attempts started and completed in this batch, retry waiting count, earliest remaining retry wait, and up to four active workers. Waiting candidates do not consume worker slots. Worker rows contain candidate ID, attempt number and activity phase, with no key or validator session ID. The earliest wait is derived from the coordinator's actual monotonic retry deadlines; committed `RetryEligibleAt` is a separate wall clock field in saved reports. Do not infer elapsed wait from a wall clock that may move backward.

Detailed worker phases come from a new typed, optional activity callback at the validator boundary. The Docker adapter emits the approved phase immediately before its existing preparation, registration, attachment, polling, or cleanup call. The coordinator binds that callback to its own candidate and attempt identity and discards callbacks after the attempt generation ends. Other adapters can remain at preparing until completion. This callback carries no process output or remote identifier and changes no worker protocol or command scheduling.

The observer owns enough counters to publish a coherent revision after each acknowledged change. It does not retain an unbounded activity feed. Intermediate revisions can be coalesced; saved attempt history remains complete. Observer delivery must be nonblocking with respect to the UI. An internal observation failure is a typed operational failure and triggers cancellation, never unbounded buffering or ignored persistence errors.

`work.cancel` closes new dispatch admission immediately, sets the first stop time once, and requests the existing engine cancellation. It returns the current observation without waiting for cleanup. Cancellation may race an already entered store operation; ownership stays held until it returns. Repeated cancellation changes neither deadlines nor cleanup budgets. Cancelling a terminal operation returns its existing terminal observation unchanged.

The outcome remains queryable until `work.acknowledge`. Acknowledgement is permitted only after terminal lifecycle and complete resource closure. It releases the operation slot and session membership memory but does not erase its durable run. The parent retains the latest ordinary receipt for final reporting, and separately retains any receipt with unresolved cleanup. A later ordinary receipt can supersede only the former. Unsafe persistence and cleanup latches survive acknowledgement. No automatic retry is introduced.

Uncertain SQL blocks all later operational starts in this terminal session. Reads and export may continue when independently safe. Show restart and saved report guidance; opening another operation is not a recovery mechanism. Unknown remote cleanup blocks new attachments using the existing shared validator gate. After safe ownership release, a later explicit check may use the existing automatic recovery path, but the UI has no separate recovery action. Crawling does not bypass an uncertain SQL latch merely because it uses no validator.

### Retained cleanup and recovery proof

Before releasing an operation with unresolved remote cleanup, preserve its receipt and a private binding to the validator scope and obligations it created or left uncertain. The binding uses the existing canonical recovery location, verified daemon identity, and session manifest identities held by the validator owner. It is parent only and never enters the wire or diagnostics. An admission failure that creates no new obligation refers to the already retained origin rather than replacing it with that failed start.

Add a typed recovery observation at the validator acquisition boundary. A proof is produced only while holding the same exclusive validator owner, after reconciliation has proven remote deletion, required container removal, and an acknowledged durable journal update for every retained obligation in that scope. Return the verified scope and resolved private obligation identities to the application coordinator before new attachment admission. If obligation identity was uncertain, proof must cover the full reconciled scope and the existing recovery contract must establish that no unknown obligation remains. Missing or replaced journal files, `CleanupInspected` alone, a successful service status read, cooldown expiry, and a successful later crawl are not proof. If a complete proof cannot be established, retain the receipt and the attachment block.

The coordinator atomically marks matching retained obligations resolved and clears their cleanup latch when the final obligation is proven resolved. This can happen even if later admission fails for a separate reason such as provider cooldown or missing image. It changes the session cleanup fact, not the original saved run outcome. It never clears uncertain SQL, uncertain local closure, or another independent failure latch. If recovery happened outside this process, only a subsequent explicit check's authoritative reconciliation can update this session's retained fact; browsing never invokes recovery.

At most one unresolved origin is retained in addition to the current ordinary receipt. This follows from one operational slot and the existing full scope recovery gate: a new batch cannot create fresh remote obligations until all earlier obligations are resolved. Repeated failed recovery starts share that origin. The implementation must enforce this invariant and preserve the prior receipt on an impossible second unresolved origin rather than evicting it. The conservative fallback is `ui_work_recovery_required`, blocking later operational starts until restart.

## Private protocol

Increment the manifest, handshake, Go and TypeScript schemas, release preparation, and package checks to protocol 3 together. Keep manifest schema 1, database schema 5, validator registry 3, and worker protocol 2. Required capabilities are exactly `overview`, `results`, `details`, `reveal`, `export`, `work`, `reports`, in that order. Existing protocol 2 method semantics and shapes carry forward except for the explicit preparation job extensions below. First `hello` and first `overview`, strict IDs, closed fields, and one outstanding request remain required.

Requests remain at most 4 KiB including newline; replies at most 1 MiB. Bounded lists use at most 50 rows and at most 768 KiB of encoded item data. Decimal encodings, stamps, safe codes, nullability, `DisplayText`, and five second read contexts within twelve second request watchdogs follow spec 0011. Source selection contains at most 32 channel names and fits the request bound; check requests carry a selector handle, never every selected ID or key.

All methods below use the same inherited session pipes and account boundary. Unknown fields, wrong types, invalid enum values, or malformed decimal syntax terminate the protocol. Well formed stale handles, unavailable candidates, invalid numeric ranges, and busy states return safe operation errors. The UI never retries a mutation on an ambiguous reply.

| Method | Required params | Result | Main errors |
| --- | --- | --- | --- |
| `work.options` | Empty object | `WorkOptions` | Configuration is already validated; safe session failure only |
| `work.review_crawl` | `channels: string[1..32]`, `page_limit: Decimal?` | Existing `JobStarted` | `state_invalid_input`, `ui_busy`, collection/privacy or snapshot code |
| `work.review_check` | `view_id: Decimal`, `selection: focused or marked or matching`, `candidate_id: Decimal?` | `JobStarted` | `ui_handle_stale`, `ui_work_selection_empty`, `ui_candidate_unavailable`, `validation_unsupported`, `ui_busy` |
| `work.review_release` | `review_id: Decimal` | `{released: true}` | `ui_handle_stale` |
| `work.start` | `review_id: Decimal` | `{operation_id: Decimal}` | `ui_busy`, `ui_handle_stale`, `ui_work_recovery_required` |
| `work.poll` | `operation_id: Decimal` | `WorkState` | `ui_handle_stale` |
| `work.cancel` | `operation_id: Decimal` | `WorkState` | `ui_handle_stale` |
| `work.acknowledge` | `operation_id: Decimal` | `{acknowledged: true, unresolved_cleanup: UnresolvedCleanup?}` | `ui_busy`, `ui_handle_stale` |

`candidate_id` is nonnull only for focused selection, and null for other selectors. The candidate must belong to the named view. Duplicate crawl channel inputs are rejected; no client provided order can override configuration order. Page limit null uses each configured limit, otherwise the parsed integer must be 1 through 100.

Extend the existing preparation job `kind` with `work_review`, `report_open`, and `report_refresh`. Add phases `selection` and `report`; existing phases still serve snapshot reads and cleanup. Extend the tagged `JobResult` with `{type:'work_review', value:WorkReview}` and `{type:'report', value:ReportView}`. Keep one preparation job, existing cancellation and commit gating, and successful result activation before cleanup warning rendering. These jobs never carry a work receipt. `WorkState` belongs to the operational slot, never `job.poll` or `job.cancel`.

### Closed work records

Every field is required. A question mark means explicit null is allowed. All counts, IDs, durations and policy integers use `Decimal`. `Code` is an applicable value from the shared closed catalogue.

| Record | Exact fields |
| --- | --- |
| `WorkOptions` | `sources: WorkSource[0..32]`, `validator: unsupported or docker`, `image: DisplayText?`, `check_policy: CheckPolicyWire`, `check_available: boolean`, `check_error_code: Code?` |
| `WorkSource` | `channel: string`, `enabled: boolean`, `page_limit: Decimal` |
| `WorkReview` | `review_id`, `kind: crawl or check`, `created_at: Stamp`, `collection_state: absent or existing`, `sources: SourcePlan[]?`, `page_budget: Decimal?`, `selection: focused or marked or matching or null`, `candidate_id: Decimal?`, `selected_count: Decimal?`, `view_id: Decimal?`, `as_of: Stamp?`, `filters: Filters?`, `policy: CheckPolicyWire?`, `image: DisplayText?` |
| `CheckPolicyWire` | `version`, `workers`, `max_attempts`, `mutation_interval_ns`, `candidate_timeout_ns`, `attempt_timeout_ns`, `command_timeout_ns`, `poll_timeout_ns`, `poll_interval_ns`, `cleanup_timeout_ns`, `shutdown_timeout_ns`, `finalize_timeout_ns`, `retry_base_ns`, `retry_cap_ns`, `memory_mib`, `cpus`, `processes`, `protocol_version` |
| `WorkState` | `operation_id`, `revision`, `kind`, `state`, `phase`, `observed_at: Stamp`, `elapsed_ns`, `stop_elapsed_ns: Decimal?`, `run_id: Decimal?`, `run_created: true or false or unknown`, `run_status: RunStatus?`, `cancellation_requested: boolean`, `persistence: known or uncertain`, `cleanup: not_needed or pending or complete or unresolved`, `error_code: Code?`, `crawl: CrawlProgress?`, `check: CheckProgress?`, `guidance: WorkGuidance`, `unresolved_cleanup: UnresolvedCleanup?` |
| `UnresolvedCleanup` | `operation_id`, `run_id: Decimal?`, `run_created: true or false or unknown`, `error_code: Code` |
| `CrawlProgress` | `selected_sources`, `completed_sources`, `failed_sources`, `skipped_sources`, `page_budget`, `requested_pages`, `completed_pages`, `saved_candidates`, `saved_sightings`, `current_channel: string?`, `current_page: Decimal?` |
| `CheckProgress` | `totals: ValidationCounts`, `attempts_started`, `attempts_completed`, `retry_waiting`, `next_retry_wait_ns: Decimal?`, `policy: CheckPolicyWire`, `workers: WorkerActivity[0..4]` |
| `WorkerActivity` | `candidate_id`, `attempt_number`, `phase: preparing or registering or attaching or polling or cleanup` |
| `WorkGuidance` | `action: none or retry_review or inspect_report or configure_validator or inspect_owner or stop_service or recover_validator or review_single_candidate or restart_session`, `retry_at: Stamp?`, `service_observed_at: Stamp?` |

`run_created` is a string enum, not a JSON boolean. For a crawl review, sources and page budget are present and all check specific fields are null. For check review, sources and budget are null, candidate ID is nonnull only for focused selection, and other check fields are present with a positive selected count. In `WorkState`, exactly the matching progress variant is present after run acknowledgement; both are null before it. `elapsed_ns` uses the parent's monotonic operation start, and stop elapsed is null until cancellation. Policy durations convert existing microseconds to exact nanoseconds, checked for overflow. Validation counts retain the fields defined in spec 0010.

Before run creation is entered, `run_created=false`. During an entered unresolved creation it is `unknown`. Acknowledgement sets it to `true` with the run ID. An engine error is not proof that an entered write rolled back. A verified final read can resolve uncertainty only when it conclusively identifies the attempted run through its private request token; absence from recent history is insufficient. Keys and tokens remain parent only. `run_status` is the last acknowledged saved status and may remain `running` in a terminal failed operation.

Progress counters are last acknowledged cumulative facts. A running record has no final error; a cancelling record can expose the first stop error if cancellation followed a failure. A failed record requires an error. A cancelled record uses `cancelled`. Successful records have null error. A stop request during already committed completion may leave state succeeded with `cancellation_requested=true`. Recovery or admission failure before a run has null progress. Display no zero counts in place of that absence.

`check_available` describes configuration alone: true for configured Docker, false with `validation_unsupported` otherwise. The UI labels this as configuration readiness, not a live probe. Image is null for unsupported and otherwise a bounded image identity. Do not return Docker socket paths, private registry contents, or raw configuration.

`unresolved_cleanup` names the retained originating operation, which can be the current operation or an earlier acknowledged one. Its safe fields come from that origin's receipt. It becomes null only after the recovery proof above clears the last obligation. Polls publish the updated field in the same revision as latch clearing. Acknowledgement returns the remaining value so the UI can retain its warning when there is no active operation to poll. No private scope, daemon, or manifest identity is exposed.

### Request scheduling and display freshness

Use one application level request scheduler shared by Overview, Results, export, reports, and work. One request stays outstanding. After completion, give queued cancellation highest priority, then user initiated actions, then a due work poll, then other automatic reads. Coalesce automatic reads and repeated cancellation; retain at most the newest queued navigation target. Session exit or a signal cancels the connection directly instead of waiting for a queued poll.

Start active work polling immediately after its start reply, then one second after each completed poll. Keep polling when work is off screen, in help, or below minimum size. A continuous sequence of background reads may not starve cancellation or work polls. Foreground preparation uses the existing asynchronous jobs; its polling shares this scheduler rather than holding the pipe until the preparation completes.

If user actions postpone progress, retain the previous observation with its original receive time. Label it delayed after three seconds without a new operation observation; never invent increments or completion. Once a poll enters, the existing twelve second request watchdog applies. A failed connection ends the session and cancels work; there is no reconnection. This one second cadence is a target between requests, not a promise to interrupt an entered database read.

### Failures and guidance

Add `ui_work_selection_empty`, `ui_work_recovery_required`, and `ui_report_unavailable` to both diagnostic catalogues. Reuse `ui_collection_changed`, `ui_handle_stale`, `ui_busy`, `ui_page_invalid`, snapshot errors, existing state codes, source codes, validator codes, and cancellation codes. Finite UI messages map codes to plain explanations; arbitrary errors are never displayed.

For `state_busy`, acquire no replacement writer and do not poll until it becomes free. Independently observe the service using the existing bounded read. Offer `stop_service` only when installation identity is verified, collection match is exact, manager reports running, runner ownership is verified for that installation with a known owner PID, and the observation has neither stale nor clock anomaly flags. Include its observation time and explain that the command stops that observed service. All other cases use `inspect_owner`, without asserting that the service is the blocker. The suggestion is text `wpc service stop`; the UI cannot execute it. It is never presented for a validator ownership conflict from another collection.

Provider waiting includes `retry_at` only when the existing typed admission result supplies it. Missing trusted delay stays unavailable. Source rate limits preserve the engine's skipped source reasons. Unknown cleanup yields recovery guidance, and a required probe yields single candidate review guidance. No screen automatically queues, retries, stops a service, or invokes recovery. Configuration guidance names the existing configuration and explicit commands without adding flags to the root UI invocation.

## Rationale

A distinct operational slot preserves responsiveness while keeping one writer owner. Typed observations avoid tying execution to screen writes, and the existing saved records remain the evidence behind outcomes. Polling extends the current protocol with fewer failure paths than a second event transport.
