# Durable scheduler

## Summary

You get one saved crawl schedule and one eligibility record per candidate. The application wakes when work may be due and selects a small batch from SQLite. A single writer coordinates the independently progressing crawl and validation lanes.

## Persistence contract

Schema version 5 adds `scheduler_state`, `candidate_schedule`, and `runs.origin`. It preserves all earlier rows. There are no deletion or history retention jobs.

`scheduler_state` has exactly one row, keyed by `id=1`. Required `last_observed_at` is a UTC microsecond timestamp. The crawl field group is nullable until a coverage plan is prepared. Within that group, `crawl_coverage_json`, `crawl_due_at`, `crawl_reason`, `crawl_basis_at`, and `crawl_interval_ns` are either all present or all absent; `crawl_basis_run_id` is an optional foreign key to a crawl run. Coverage stores ordered objects with normalized channel and page limit, not a hash whose original input would be unavailable.

The optional local validation wait group has `validation_wait_reason`, `validation_wait_basis_at`, `validation_retry_at`, `validation_wait_interval_ns`, and `validation_wait_observed_at`. It is either entirely absent or entirely present. A shared cooldown observation uses the shared gate basis, remaining deadline, and originating interval; it does not replace that interval with this profile's setting. Other wait reasons and their intervals are defined below.

Required boolean `validation_recovery_pending` records an obligation to inspect retained remote work. Migration initializes it to false. A runner with validation enabled sets it true at startup without replacing a saved wait or its basis. Set it true before each acquisition attempt and when an active batch leaves cleanup pending. Clear it only when the validator reports that retained sessions and untracked container checks completed successfully. Gate denial alone is not proof of cleanup inspection. Persist the flag and any resulting local wait together. Disabling validation preserves the flag without attempting recovery.

`candidate_schedule` has required `candidate_id` as both primary key and foreign key, `due_at`, `reason`, `basis_at`, and `interval_ns`. `reason` is `first_check`, `routine`, or `failure`. `basis_run_id` and `basis_attempt_id` are nullable foreign keys. When present, references must identify the same candidate and run; store validation enforces this relationship. First checks have neither reference and use interval zero. Routine and failure records have a run and attempt reference and a positive interval. The selection index is `(due_at, candidate_id)`.

`runs.origin` is required and limited to `manual` or `scheduled`; existing rows become `manual`. Origin is part of an idempotent creation payload, so a replay that changes it conflicts. Existing run, item, attempt, and source state machines remain authoritative for work in progress. Do not introduce a second active status in eligibility rows.

The store accepts an explicit scheduling policy containing crawl, routine, and failure durations. Runtime writers supply their effective settings before migration and recovery. A compatibility `OpenWriter` wrapper may use domain defaults of `1h`, `6h`, and `1h`; production application paths use the explicit policy entry point. No SQL method reads configuration files or wall time itself.

### Migration and initialization

Migration, initial eligibility backfill, and the schema version update commit together. Use the writer's supplied recovery time and policy. Recover unresolved earlier records using the existing interrupted semantics, then derive eligibility from their recovered history in that same initialization transaction. A failure rolls back the entire version 5 initialization.

For every candidate, find its highest attempt ID. If none exists, use its first observation time and interval zero. Otherwise use that attempt's terminal batch item completion or recovery time when available; for legacy attempts use the attempt completion or recovery time. A confirmed or invalid final attempt gives `routine`; an inconclusive or recovered attempt gives `failure`. Save the chosen attempt and run references. A candidate with no provenance or no truthful terminal basis is damaged state, not a reason to invent a time.

Initialize the checkpoint from the supplied recovery clock. On an existing version 5 database, never lower it. Recover interrupted rows and update their eligibility atomically before allowing new work. Do not change a completed item just because its parent batch needs recovery. A pending item with no attempt retains its earlier eligibility. A started attempt recovered as inconclusive receives the failure interval from its actual recovery timestamp.

Crawl state starts absent. Preparing enabled coverage for the first time may adopt the highest ID terminal historical crawl whose full saved plan exactly matches, including order and page limits. Its terminal or recovery timestamp is the basis. Legacy single page coverage can match only one configured channel at one page with the same normalized source identity. A historical record without enough saved coverage cannot satisfy the match. With no match, use the preparation timestamp, reason `initial`, and interval zero.

Once coverage exists, a different effective plan replaces it with reason `coverage_changed`, preparation time, and zero interval. A manual crawl also prepares the full configured plan before recording its selected plan. Thus a manual crawl of the changed full configuration can satisfy it before the next runner starts. A subset cannot. Reordered configured channels count as changed coverage.

### Atomic mutations

The operation that first saves a candidate also creates its `first_check` scheduling row. Subsequent sightings leave that row untouched. A terminal validation item updates eligibility in the same transaction as the final result, including exhausted retries. A retryable attempt that leaves an item active does not advance eligibility. A terminal crawl updates the saved crawl schedule in the same transaction only when its plan matches the saved full coverage. Ordinary completed, failed, or cancelled crawls use their terminal time plus the crawl interval, with reasons `completed`, `failed`, or `cancelled`. Recovery uses `interrupted` and its explicit recovery time plus that interval.

Candidates whose batch items were skipped before their first attempt retain their earlier due times. Cancellation or interruption after an attempt makes them eligible using the failure rule. A definitive result already committed before cancellation keeps its routine interval. Neither terminal batch status nor a later skipped item can overwrite a more recent real attempt. Compare attempt IDs, not timestamps, when deciding which execution owns the timing basis.

Each mutation retains the existing stable request identity and exact replay rules. Replay returns the originally committed scheduling result; it does not apply a newer policy or clock to the same payload. Store completion inputs therefore include the scheduling policy values used by that execution. Policy recomputation is a distinct startup operation.

### Startup reconciliation

After validating settings and acquiring the writer, compare UTC to the saved checkpoint. Recovery can record facts while UTC is behind, but automatic dispatch and interval recomputation wait until the clock catches up. Do not lower existing checkpoints or alter evidence timestamps.

At an admissible startup time, atomically recompute candidate due times from their saved reasons and basis timestamps. `first_check` remains immediate, `routine` uses the current routine interval, and `failure` uses the current failure interval. Recompute a completed crawl from its saved basis using the current crawl interval. Initial and changed coverage remain immediately due. Recompute local backend or cleanup waits from their original basis using the failure interval; preserve copied shared cooldown deadlines until the registry is reread. Repeating startup with unchanged settings is idempotent and does not move timing bases.

Startup recovery honors a future saved local retry deadline after that reconciliation. With no applicable wait, or an elapsed deadline, the saved recovery obligation can run immediately. A restart itself never replaces a wait basis, clears contention or clock waits, or authorizes an early acquisition. Only an actual permitted attempt with a new observed failure may establish a new local basis. A deliberate failure interval edit can change a backend or cleanup deadline from its existing basis; it cannot shorten an authoritative shared cooldown.

Disabling a lane suppresses dispatch and preserves its records. An enabled crawl lane prepares current coverage; a disabled crawl lane does not replace saved coverage. Interval reconciliation can still update candidate records with validation disabled. Reenabling a lane after downtime follows ordinary due selection.

## Coordination

`internal/app` owns `Run` and the timer loop. Introduce operation cores that accept an already owned writer, explicit origin, and a serialized mutation executor. Manual `Crawl` and `CheckBatch` acquire ownership and call those cores. `Run` opens one writer and calls them without another open or recovery pass.

Use one coordinator to execute short SQLite and journal mutation requests. Source retrieval, validator acquisition, worker operations, pacing waits, and cleanup run outside that coordinator. Existing batch coordination retains its worker limit, retry policy, and admission gate, but its mutations are submitted to the shared executor. Requests return owned results through bounded channels; a requester must not hold a transaction or coordinator callback while waiting for another request. Completion and output callbacks cannot reenter the executor.

There is one crawl task and one batch task at most. Their external work may overlap. Channel capacities and pending coordination requests are bounded by the existing maximum four workers plus the one crawl task. Once a task has entered a mutation, cancellation still waits for that mutation's cleanup and releases its resources correctly. No transaction spans an HTTP request or Docker operation.

### Selection and dispatch

1. When enabled crawl coverage is due and there is no active crawl, save a new complete plan with scheduled origin, then start its requests. A due tick during a crawl does not queue another crawl.
2. When validation is enabled, there is no active batch or acquisition, and the local wait is absent or elapsed, attempt shared validator acquisition and recovery if candidates are due or `validation_recovery_pending` is true. Contention, clock, backend, and cleanup waits all retain an unfulfilled recovery obligation and retry at their saved deadlines even with no candidates. Startup uses this same rule; it has no deadline bypass. Successful cleanup inspection clears the obligation. With no candidates, release ownership without a run or probe; a remaining shared cooldown still remains authoritative. Acquisition does not create a candidate attempt. A blocked result records the local wait below.
3. After ownership and gate admission, select eligible candidates in `(due_at, candidate_id)` order from the writer's current view. Exclude every candidate in a pending or active item of an active batch, as well as unresolved attempts. The fixed maximum is `2 * configured workers`; a probe selects one. Persist the complete selection and effective check policy before dispatch. An empty rechecked selection releases ownership and creates no batch.
4. Keep that batch selection immutable. Rediscovered or newly inserted candidates wait for the next batch boundary. Requery after the batch becomes terminal and ownership is released. Failed candidates already have a future due time, so they do not displace older pending candidates immediately.
5. New scheduled invocations receive new random request tokens. They never replay a previous network operation, resume a crawl cursor, or spend the previous batch's remaining retry budget.

Normal scheduled batches use the configured check policy. A probe uses one worker, one attempt, and the same candidate time budget and mutation interval, recording those effective values. The global worker ceiling remains respected. Small batches limit retained candidate metadata; the database retains one eligibility row per candidate, not one queued job per missed interval. Growing history remains intentional.

### Local validation waits

| Typed condition | Local behavior |
| --- | --- |
| Docker endpoint temporarily unavailable, daemon connection failure, or bounded acquisition timeout | Save reason `backend_unavailable`, observation time as basis, and retry at basis plus failure interval |
| Known retained session cannot yet prove cleanup | Save `cleanup_pending` with the same failure interval; retry cleanup without permitting new registration |
| Shared owner busy | Save `validator_busy` and retry after five seconds; no candidate failure or attempt is invented |
| Shared gate cooling | Copy its saved basis, originating interval, deadline, and reason; save the local observation time separately |
| Shared gate clock regressed | Save `validator_clock_regressed`, the current observation time as basis, the positive duration to its checkpoint as interval, and that checkpoint as retry time |
| Proven successful acquisition and recovery | Clear the local wait; it is not candidate evidence |

The acquisition result includes a typed `CleanupInspected` boolean. It is true only after checking all retained sessions and untracked containers and proving any required cleanup and removal. A cooldown or clock refusal can carry this fact if inspection actually completed first. Clear the recovery obligation on that fact while retaining a needed admission wait. Otherwise leave the obligation true, including after owner contention, timeout, cancellation, and early gate refusal. If there are neither candidates nor an outstanding obligation, an expired wait alone does not create a probe or another acquisition loop.

Each acquisition and recovery attempt inherits the existing five minute Docker acquisition budget. If its monotonic start is `A`, its deadline is the earlier of `A + 5m` and the parent deadline. The budget covers ownership acquisition, endpoint and daemon checks, registry inspection and conversion, retained cleanup, and image checks; those phases do not each receive five minutes. Existing command timeouts remain additional limits. Owner lock contention still returns immediately. This budget is independent of the per candidate budget, which starts only when candidate work is admitted, and of the failure interval between acquisition attempts. Timeout retains truthful journal state and any unfulfilled recovery obligation; use `cleanup_pending` when retained cleanup is known to remain unresolved, otherwise `backend_unavailable`. If `CleanupInspected=true` was already established before a later phase timed out, clear the fulfilled obligation while retaining the backend admission wait.

The validator exposes typed acquisition dispositions; the scheduler never classifies raw stderr or error strings. Wrong ownership, unsafe permissions, unknown registry version, image or daemon identity mismatch, untracked containers, corrupt state, and journal replacement uncertainty are fatal. They terminate the runner after available cleanup rather than being called temporary unavailability. A configured image absent from the local daemon is an explicit setup error, not an automatic image pull.

If a batch ends with unresolved cleanup, its local retry wait is recorded after trustworthy SQLite finalization. If that local write is uncertain, stop all further SQLite mutation and exit; the global recovery journal remains the remote authority. Manual checks keep their explicit return behavior and do not stay running to retry backend acquisition.

## Time and lifecycle

UTC wall time supplies saved timestamps. In process timers and existing request pacing use the monotonic clock. Reevaluate wall eligibility on task events and at least every 30 seconds, even when the next saved deadline is much later. Use the earlier of the next actionable deadline and this polling bound. Ignore due rows for a disabled, occupied, or blocked lane when choosing the next wakeup; use that lane's completion event or gate deadline instead. An empty candidate set waits for discovery or the next bounded reevaluation; it does not spin.

Persist the maximum observed UTC checkpoint on each scheduling mutation, before admitting new external work, and at most once per 30 second observation tick while idle. A clock read below the checkpoint changes the runner to `clock_paused`. Do not advance the checkpoint while paused. Log the transition once; check again on the bounded tick. After UTC reaches the checkpoint, perform any deferred startup reconciliation and resume due selection once. Forward jumps expose due rows once, not one job per elapsed interval.

On regression during active work, suspend new source requests and validator registration or attachment admission. Cancel active lanes and allow cleanup and truthful terminal persistence. HTTP responses or evidence already observed keep the existing acceptance and cancellation rules. Remote cleanup and manual recovery remain allowed. The shared registry independently prevents another profile from bypassing its own clock restriction.

The process states are `starting`, `running`, `clock_paused`, and `stopping`. The validation lane additionally has `waiting`, `acquiring`, `checking`, and `blocked`. These process states are derived from owned work and the saved waits; they are not a second execution history or a claim of live process status in `schedule show`.

First cancellation stops admission and cancels both lanes. Let `S` be that monotonic stop time. All remote cleanup shares deadline `C = S + 60s`; this also caps cleanup for an acquisition that was running when cancellation arrived. Let `F` be the earlier of all cleanup work finishing and `C`. Local finalization may then begin, including when the cleanup allowance expired, with shared deadline `L = min(F + 5s, S + 65s)`. If cleanup finishes at `S + 40s`, local finalization has until `S + 45s`; if cleanup consumes its full allowance, it has until `S + 65s`. No worker or lane receives a fresh allowance. When remote cleanup is unresolved at `C`, preserve its journal and persist only truthful local outcomes. Do not admit new local mutations after `L`.

Retain each owner until its entered operations have stopped using the protected resources. Native store or filesystem cleanup can exceed a context deadline; wait for entered operations without extending permission to start new ones. Do not claim an unconditional wall time guarantee or release a lock around still running writes.

Any uncertain SQLite acknowledgement poisons further SQL mutations across both lanes for that invocation. Cancel source work and validation admission; remote cleanup remains independent. Journal uncertainty poisons further journal writes and new remote mutations, cancels the runner, and leaves retained sessions for later recovery. A fatal output error takes the same stop path. Existing completed commits remain readable.

## Store and application surfaces

Use typed records in `internal/state` for `SchedulePolicy`, `SchedulePlan`, `ScheduleSummary`, `CandidateSchedule`, and `ValidationWait`. Duration values are `time.Duration`; timestamp fields are `time.Time`, with pointers only where this contract permits absence.

The store provides an explicit policy writer entry point, `PrepareSchedule(ctx, plan, at)` for startup reconciliation and enabled coverage preparation, `SelectDueCandidates(ctx, asOf, limit)` for ordered bounded IDs, `RecordValidationState(ctx, optionalWait, recoveryPending)` for atomic local wait and obligation updates, and checkpoint advancement. Terminal APIs incorporate eligibility updates internally. Reader schedule queries accept a caller supplied clock callback, establish their SQLite snapshot through the initial state read, and only then sample the callback to obtain the fixed evaluation time. The store does not choose a clock or query wall time independently. State errors use the existing typed error boundary.

`App.Run(ctx, emitSummary)` owns the lifecycle and only emits saved terminal summaries. `App.ShowSchedule(ctx, optionalCandidateID)` returns a complete prepared view from a read snapshot. The CLI parses and renders; it does not calculate eligibility or read SQL directly.

## Rationale

Persisting explicit eligibility makes selection bounded and explanations direct. Saved timing bases let new configuration change future scheduling without changing evidence. Computing everything from history on every tick is the runner up, but it makes recovery and indexed selection harder to keep consistent. A separate job queue would duplicate the existing run and attempt lifecycles.
