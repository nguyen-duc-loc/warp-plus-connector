# 0005. Concurrent checks and bounded retries

**Date**: 2026-10-11
**Status**: In Progress

## Summary

You can check several saved candidates in one command, with a configurable ceiling on active checks. Temporary failures receive a finite number of attempts, and every attempt remains visible after restart. One shared owner coordinates isolated Docker sessions, command pacing, and recovery. Uncertain cleanup stops new work without changing earlier entitlement evidence.

## Requirements

**User story**: As the local operator, you can deliberately select candidates, check them concurrently within known limits, and inspect exactly what completed, retried, or stopped.

| ID | Acceptance criterion |
| --- | --- |
| AC-1 | `check` accepts one or more saved candidate IDs, removes duplicates in first supplied order, and resolves every ID before creating a batch. An unknown ID creates no run, attempt, container, or registration. The complete selection and effective policy are saved before checking starts. |
| AC-2 | The default is two active checks, configurable from one through four. Independent checks demonstrably overlap, while active checks never exceed the selected ceiling and each key has at most one active attempt. Competing batches and recovery commands cannot bypass shared ownership through another profile. |
| AC-3 | Registration and license application commands are serialized across workers. The next such command starts only after the previous command has completed and the configured interval has elapsed. The interval defaults to one second and accepts one through 60 seconds. Polling and cleanup have separate bounds. No exact limit on the vendor client's internal HTTP requests is claimed. |
| AC-4 | Each candidate gets at most three attempts by default, configurable from one through five, within a default 15 minute active candidate budget configurable from one through 60 minutes. Only explicitly classified temporary failures can retry, and only after proven cleanup. Retry waits grow with bounded jitter and release worker capacity. |
| AC-5 | A trustworthy server delay, when supplied by a capable backend, is a lower bound on the shared mutation pause and is never shortened to fit a budget. Throttling without such a delay stops new mutations for the batch and requires a later manual invocation. The pinned Docker adapter reports delay unavailable. |
| AC-6 | Every actual attempt is retained separately. Exhausted or stopped retries remain inconclusive, preserve prior definitive evidence, and do not refresh that evidence. Definitive results, Free accounts, device capacity, unsupported validation, and unclassified failures receive no immediate retry. |
| AC-7 | Cancellation and fatal batch failures stop dispatch, retry waits, and new mutation admission. Allow up to 60 seconds for remote cleanup and then a five second submission and cancellation budget for local finalization. Entered store work retains ownership until it finishes, so these budgets do not promise a hard process exit deadline. Unknown cleanup remains durable, blocks further attachments, and is reconciled across every retained session before a later batch. |
| AC-8 | An uncertain store mutation stops all further store mutations in that invocation. Earlier commits survive; unresolved local work is recovered independently of remote sessions. Restart marks old work interrupted and never resumes a batch or spends its unused attempt budget. |
| AC-9 | `check` prints each committed final candidate result as it finishes, followed by a batch summary. `check show <run-id>` reads saved selection, policy, item status, attempts, retry details, and totals after restart, including all 338 items in a controlled batch. Output errors stop work and propagate failure. |
| AC-10 | Defaults, private JSON, and explicit flags retain their existing precedence and validation. Help and version have no runtime side effects. Saved reads do not invoke Docker. Keys remain absent from results, host arguments, environment, labels, and diagnostics. |
| AC-11 | The additive SQLite migration preserves old history. The new backend safely reconciles a legacy single session before converting the recovery journal. Old binaries and incompatible images fail closed rather than starting work against the new journal. |
| AC-12 | Verification proves real Docker overlap, global mutation pacing, cleanup, saved results after restart, and unchanged host registration using separately authorized keys. Controlled fault cases and retained race tests prove the failure boundaries on macOS and Linux arm64. No live acceptance result is claimed from fixtures alone. |

## Decision

**Chosen option**: Extend the existing application with one batch coordinator, a bounded Go worker pool, staged Docker worker operations, and durable selection and retry history. Use channels, contexts, timers, and the existing store and private file helpers. No new dependency or local service is required.

This is the build contract for scope feature 6. You confirmed the spec on 2026-10-11 after independent review and correction of its six findings. Its lifecycle stays `Proposed` until development begins.

The coordinator owns scheduling decisions, SQLite mutations, result emission, and shared journal updates. Workers return typed results and request mutation work through the coordinator. SQL stays inside `internal/store`, outside network operations. One shared Docker owner lock covers recovery and the entire batch, including retry waits.

### Changes to earlier contracts

This feature extends spec 0003 rather than replacing its crawl or evidence contracts. The following changes apply to the new checking path when built.

| Earlier contract | New checking contract |
| --- | --- |
| `check <candidate-id>` creates one attempt | `check <candidate-id>...` creates one saved batch with one item per distinct selected candidate and separate attempts for retries |
| One shared owner permits one Docker session | One shared owner permits up to the effective worker limit of isolated sessions within one batch |
| Host journal contains one manifest | The same protected journal path contains a versioned registry of retained session manifests |
| Worker starts registration and attachment in one operation | Host separately authorizes registration and attachment; observations and cleanup can overlap |
| No automatic retry | A new attempt after proven cleanup is permitted only by the explicit classification and budget rules below |
| Cancellation requests cleanup briefly and returns | Batch shutdown collects cleanup for up to 60 seconds, then finalizes local history for up to five seconds |

Spec 0002 still governs evidence, writer ownership, timestamps, and preserved history. Spec 0004 still governs Telegram crawling; its sequential requests, failure behavior, and absence of automatic retries are unchanged. Scheduling, automatic selection, export formats, and background supervision remain later scope features.

## Feature design

### Configuration and limits

Add these fields to the existing strict JSON object and flags only to `check`. Whole seconds are required for the mutation interval; the candidate budget accepts Go duration strings within its bounds.

| JSON field | Flag | Default | Accepted range |
| --- | --- | --- | --- |
| `check_workers` | `--workers` | `2` | Integer 1 through 4 |
| `check_max_attempts` | `--max-attempts` | `3` | Integer 1 through 5, including the first attempt |
| `check_mutation_interval` | `--mutation-interval` | `"1s"` | Whole seconds from 1 through 60 |
| `check_candidate_timeout` | `--candidate-timeout` | `"15m"` | Positive duration from 1 through 60 minutes |

Validate supplied JSON values before applying flag overrides, as with source settings. Reject fractions for integer settings, `null`, unknown fields, and values outside bounds before state or external work. Existing validator settings and the `unsupported` default remain. No new environment variable is introduced.

Policy version 1 also saves these fixed effective values: active attempt limit 240 seconds, vendor command limit 60 seconds, attribution polling limit ten seconds, polling interval 250 milliseconds, ordinary attempt cleanup limit 60 seconds, remote shutdown limit 60 seconds, local finalization limit five seconds, retry base five seconds, and retry upper cap 60 seconds. Container limits remain 512 MiB, one CPU, and 256 processes per session. The worker protocol becomes version 2; the pinned WARP version remains `2026.8.2100.0`.

### CLI and application surfaces

| Surface | Inputs | Outputs and behavior |
| --- | --- | --- |
| `wpc check <candidate-id>...` | One or more positive decimal `int64` IDs, common profile flags, validator flags, and the four limits above | One final item record per candidate, in completion order, then one saved batch summary |
| `wpc check show <run-id>` | One positive decimal run ID and common profile flags | Saved policy and run status, every item in selection order, each item's attempts in attempt number order, then totals |
| `wpc validator recover` | Existing validator flags and common profile flags | Reconcile every retained session without registering or attaching; `cleanup=complete` only when none remain unresolved |
| `App.CheckBatch` | Context, ordered candidate IDs, effective settings, and a final item output callback | Saved batch identity and terminal summary, plus a typed error on incomplete or inconclusive work |
| `App.ShowCheck` | Context, run ID, report output callback | Paged saved history through a reader, without recovery or external calls |

Keep `App.Check` as a compatibility wrapper for a single item if existing callers need it; it uses the same batch engine and policy, and returns the final attempt. It cannot provide a path around ownership or retries. The old backend interface may remain for the unsupported adapter and controlled tests, behind an application adapter. Docker uses a batch session interface with acquisition, preparation, registration, attachment and observation, cancellation, cleanup, and release operations. A shared Docker object must not expose an independently locking `Check` call to each worker.

Malformed IDs are argument errors before configuration loading. Resolve all valid IDs before saving a batch; database opening retains normal migration and recovery side effects. Deduplicate in first supplied order. Do not add a total result cap or an automatic selector. A batch containing one item follows the same path. An empty selection is an argument error.

Use the existing `key=value` result style. Final item rows contain `candidate_id`, `run_id`, `item_status`, `stop_reason`, `attempts`, and the existing final attempt fields when present. An item without an attempt renders `attempt_id=none outcome=unchecked`; this does not create an unchecked attempt in storage. The batch summary includes `run_id`, run status and reason, selected, pending, active, completed, skipped, interrupted, confirmed, invalid, inconclusive, unresolved, and unstarted counts. Counts come from saved rows, not worker counters. Entitlement counts use each item's latest attempt only, including unresolved attempts for an active report; an item with no attempt contributes to unstarted. Entitlement counts therefore need not partition execution status counts.

`check show` includes the policy snapshot, item timestamps, each attempt's number, classification, chosen wait and eligible time, and existing evidence fields. Render absent fields as `none` and times as UTC RFC3339 with stored precision. For legacy validation runs, report `batch_metadata=unavailable` and page through their existing attempts without inventing a selection or policy. A crawl run is a wrong kind error. Active reports describe saved progress, not a frozen view of future completions.

A successfully read historical report exits 0 even if the recorded batch failed. A checking invocation exits 0 only if every selected item finishes with a definitive supported result. Any inconclusive, skipped, interrupted, or infrastructure failure returns 1; signals retain 130 and 143. A completed batch can contain inconclusive outcomes because run status describes execution. Result callbacks run only on the coordinator and propagate output errors. Once output fails, make no further output calls, stop checking, and perform permitted cleanup and finalization.

### Saved model

Add migration `004_validation_batches.sql`, schema version 4. Existing tables and old records remain intact. Times use the store's UTC microsecond representation. Durations use positive integer microseconds. All new rows remain retained under the existing history policy.

| Table or record | Key and relationships | Fields and rules |
| --- | --- | --- |
| `validation_batches` | `run_id` is both primary key and foreign key to `runs`; one per new validation run | Required `backend_id`, `backend_version`, and `policy_json`. The JSON is a versioned, typed encoding of every effective setting above, validated at the store boundary. Immutable after creation. |
| `validation_items` | Primary key `(run_id, candidate_id)`, foreign keys to batch and candidate; unique `(run_id, ordinal)` | Required positive `ordinal` and `status`; nullable `stop_reason`, `started_at`, `finished_at`, `recovered_at`. Status is `pending`, `active`, `completed`, `skipped`, or `interrupted`. |
| Existing `validation_attempts` | Existing attempt ID and run and candidate links | Existing terminal outcome and evidence contract. Keep the unique unresolved attempt constraint. |
| `validation_attempt_details` | `attempt_id` primary key and foreign key; `(run_id, candidate_id)` references its item; unique `(run_id, candidate_id, attempt_number)` | Required positive `attempt_number`; nullable `failure_class`, `retry_wait_us`, `retry_eligible_at`. Wait and eligible time are either both present or both absent. |
| Shared Docker journal | Existing private `session.json`, with version 2 and a bounded `sessions` collection | Every entry retains session, container, daemon, image, profile, run, candidate, attempt, submission state, creation time, and cleanup receipt. No candidate key. |

The store checks that detail links equal the referenced attempt's run and candidate. Attempts for new batch runs require a pending item for attempt 1 or an active matching item with a saved retry plan for later attempts, and sequential attempt numbers. Legacy attempt APIs reject new batch runs if they cannot satisfy this contract. The backend identity for each attempt matches its batch snapshot. Enforce foreign keys, uniqueness, lifecycle checks, and typed policy validation through both SQL constraints and store operations where appropriate.

`pending` has no lifecycle timestamps. The first committed attempt changes it to `active` and fills `started_at`. Retry waits leave the item active. `completed` requires at least one resolved attempt, no unresolved attempt, and a finish time. Its stop reason is absent only for a definitive result; otherwise use `nonretryable`, `attempt_limit`, `candidate_timeout`, `cancelled`, or the batch stop code. `skipped` has no attempt or start time, but has a finish time and stop reason. `interrupted` has a recovery time and reason `interrupted`, no invented finish time, and may retain its real start time. Terminal items never resume.

Attempt details are inserted atomically with the attempt. Completion atomically saves the existing terminal attempt fields, its classification, and either the next retry plan or item completion. A planned retry records the actual chosen wait and due time before waiting. If a later batch stop prevents that retry, preserve the historical plan and terminate the item with the stop reason. Never invent an attempt for work that was only queued.

Store operations have typed domain inputs in `internal/state`: `StartValidationBatch` saves run, policy, and the full selection atomically; `BeginBatchAttempt` saves intent; `CompleteBatchAttempt` resolves an attempt and its scheduling disposition; `FinishValidationItem` closes an item with no unresolved attempt when its retry wait expires or work is stopped; `StopValidationBatch` closes remaining eligible items and the run atomically after active attempts are resolved; `FinishValidationBatch` requires all items terminal. Stable request tokens and exact payload replay retain the existing idempotence rules. Conflicting replays fail, and an uncertain acknowledgement never authorizes a second payload.

Readers provide a batch header with aggregate counts in one read transaction, paged items, and paged attempt details. Use pages of 100 and existing maximum page size 1000, with no total cap. New cursor collections bind the run ID and selection or attempt ordering; sealed batch membership is fixed at creation. An active report labels its header as a point in time; later pages can reflect later saved progress. A terminal report is stable.

### Admission, dispatch, and fairness

For Docker, resolve configuration, acquire the profile writer, validate selected candidates, then acquire the shared validator owner lock without waiting. Inspect the local daemon, image protocol, registry, and labeled containers. Reconcile retained work before starting the new batch. The lock acquisition order is always profile writer then validator owner; `validator recover` acquires only validator ownership and never opens another profile's writer. A busy owner or failed preflight creates no new batch or attempt. Unsupported checks require no Docker ownership or inspection.

After saving selection and policy, fill at most the configured number of worker slots. A slot covers an attempt from durable intent through container preparation, checking, cleanup, receipt persistence, and owned container removal. A retry cannot release the slot until cleanup and local removal are acknowledged. At no time may tracked live containers exceed the worker ceiling. A retained container whose removal failed stops the batch even when remote cleanup succeeded, so it cannot quietly consume extra capacity.

Keep pending items in persisted selection order and retry candidates ordered by eligible time, then selection order. When both queues have ready work, alternate a new item and a retry, starting with a new item. Empty or not yet eligible queues yield to the other. Refill only on the coordinator; the pending database selection is not a goroutine per candidate. Worker and result channels are bounded by the worker limit. The coordinator can retain compact scheduling metadata for the finite supplied selection, while history and result reads remain paged.

An item's monotonic deadline starts when its first attempt is admitted and includes all later retry waits, queue waits, and attempts. Time before its first attempt does not consume that budget. On expiration while waiting, finish it with `candidate_timeout` without adding an attempt. Each attempt has an active deadline equal to the earlier of that candidate deadline and 240 seconds after admission. Preparation and waiting for mutation admission consume this active deadline. Cleanup has its separate budget.

Definitive evidence is eligible only if the host receives and validates its private observation or final receipt before the active deadline and before the first batch stop. Record eligibility in coordinator memory using the host monotonic receipt time; the worker UTC evidence time is for history and cannot establish timeliness across different clocks. The worker keeps its normalized observation in every later status snapshot, allowing the host to receive it before cleanup finishes. Evidence first received after the boundary remains private and the public attempt is inconclusive, even if its worker timestamp appears earlier. This conservative rule avoids inventing synchronized clocks.

Once evidence is eligible, expiration or a later global stop forbids further checking and retries but does not discard that evidence. If cleanup is proven and its receipt becomes durable within the remaining cleanup allowance, save the definitive result under the normal store rules. Container removal failure can still fail the batch while retaining that proven result. Uncertain store acknowledgements prohibit further writes even for eligible evidence. The batch retains its failed or cancelled status independently of successful item results.

### Staged worker and pacing contract

Retain isolated containers, no mounts, no host network, no tunnel connection, immutable images, and private stdin delivery. Version 2 worker operations are `initialize`, `register`, `attach`, `status`, `cancel`, and `recover`. The old all in one `check` operation is rejected for version 2 sessions. Initialization, registration, attachment, and recovery take the worker operation lock. Each external mutation is phase guarded and cannot repeat an entered operation.

`status` reads the atomic private session snapshot without acquiring the operation lock; it never changes state. `cancel` durably writes a separate private cancellation marker without that lock; it never rewrites the session snapshot. These paths retain ownership, size, permission, and symlink validation. A worker cancellation watcher checks the marker at least every 100 milliseconds and cancels only the active operation context. Registration, disconnect, attachment, and attribution polling use that context, check cancellation before each new vendor command, and honor its cancellation while a command is running. Cleanup uses a separate context and is not cancelled by this marker. A cancelled mutation remains uncertain until recovery proves cleanup.

`initialize` saves the key and session identity in `prepared` without contacting the provider. Host intent records which operation it is about to submit before invoking it. Registration and attachment requests carry the remaining active duration through structured stdin; the host derives it from its monotonic deadline immediately before dispatch. Each worker operation applies that duration to a fresh local monotonic context and retains the 60 second vendor command bound. The host context still bounds its own wait and signals cancellation at its deadline. No serialized wall clock deadline grants authority to resume a session.

`register` is allowed only from prepared. The following durable phases distinguish remote creation, identity capture, and readiness for attachment. Persist each transition before the next vendor operation. Failure preserves the last saved phase and any real identity; it cannot clear fields to make a session appear unused.

| Worker phase | Required saved facts | Recovery action |
| --- | --- | --- |
| `prepared` | Session and input, no registration identity, no external mutation intent | Close without registration or attachment |
| `registration_pending` | Creation intent, no registration identity yet | Inspect the same isolated registration; capture a valid identity and delete it. Missing registration alone remains uncertain except for the existing exact creation throttling proof. Never create again. |
| `registration_created` | Explicit creation success acknowledgement, no registration identity yet | Inspect and persist the actual identity before deletion; missing or malformed identity remains uncertain |
| `registration_identified` | Matching nonempty registration and device identities captured from this container | Inspect those exact identities and delete them, including when disconnect failed or its acknowledgement was lost |
| `registered` | Captured identities and successful disconnect acknowledgement | Await host attachment admission during normal work; recovery performs identity checked cleanup only |
| `attachment_pending`, `observed`, or `cleanup_pending` | Captured identities plus any persisted completion marker, observation, or cleanup intent | Preserve observation, reconcile the exact registration, and perform cleanup only |
| `cleaned` | Durable cleanup receipt or the narrow proven creation rejection | No remote mutation; host persists receipt and removes the owned container |

After saving `registration_pending`, issue creation once. On explicit success save `registration_created`, obtain and save identity in `registration_identified`, disconnect, then save `registered`. A failure between capture and persistence uses the earlier phase and reinspection of the same container. A failure after identity persistence cannot adopt a different registration. A pending or created phase can acquire identity only from that exact initialized container, whose private protocol permits no competing creation. The existing deletion acknowledgement and subsequent missing registration proof still apply. The narrow existing creation throttling proof remains the only exception that can close rejected creation without deletion.

`attach` is allowed only from registered. It saves `attachment_pending`, applies the license once, then records an `attachment_finished` marker exposed through private status. The same worker continues observation and cleanup under its lock; the host polls `status` while its attach execution runs. `attachment_finished` means the vendor command has terminated, including its subprocess cleanup, not that entitlement or remote cleanup is known. The marker persists through later worker phases and includes the normalized operation reason and failure class. The host processes that classification before releasing mutation serialization: unsafe outcomes or throttling without a trustworthy delay latch a stop immediately, without waiting for cleanup. A failure to establish command completion likewise stops further mutations. Intermediate observations cannot publish confirmation.

The coordinator serializes registration and attachment submissions through one queue in request arrival order, with selection order breaking a simultaneous tie. After observing command completion, set the next admission time to the host monotonic observation time plus the configured interval. Waiting after completion is deliberately stricter than waiting after start. Registration completion is the operation response or its durable registered or cleaned status; attachment completion is the marker above. The first mutation needs no initial interval. If completion is ambiguous, do not release the gate for new work.

Workers never request registration or attachment autonomously. Admission checks the batch stop latch and relevant context immediately before submitting a phase guarded command. A mutation admitted before a stop may already be executing and requires cleanup; the guarantee forbids any new admission after the latch. A worker also checks its cancellation marker immediately before a mutation. A global stop sends cancellation to every tracked session, including prepared and registered sessions.

Poll registration information at most once per 250 milliseconds for ten seconds. Failed or incomplete attribution remains inconclusive. The existing identity checks and exact matching license plus `unlimited` account type remain the only confirmation proof. Cleanup and read commands do not use mutation admission, so throttling cannot block deletion. They remain bounded by per command and cleanup deadlines. No retries are added inside a vendor mutation or deletion command.

An idle registered session waits for host authorization while the host tracks its active deadline. Do not add an autonomous attachment. If the host disappears, the retained session is cleaned by the next recovery command. A submitted attach worker may finish observation and cleanup independently, as today. Cleanup receipts and normalized evidence survive host disconnects; local missing state alone never proves remote deletion.

### Retry policy

The adapter returns a typed failure class alongside outcome, reason, and evidence: `temporary`, `throttled`, `terminal`, or `unsafe`. Success has no failure class. An optional positive server delay belongs to the throttled result and is measured from receipt of the provider response. Raw messages never reach policy code. Application code cannot infer retryability by substring matching diagnostic text.

| Observation | Classification and action |
| --- | --- |
| Matching Unlimited evidence with proven cleanup | Definitive confirmation, complete item |
| Proven definitive rejection from a capable backend | Definitive invalid result, complete item; do not invent such a mapping for the current Docker adapter |
| License operation timeout with attributable registration and proven cleanup; or explicit attribution polling timeout with proven cleanup | `temporary`, eligible for a new isolated attempt within budgets |
| Caller cancellation, active candidate deadline, or attempt deadline before any remote submission | Finish or retry only as specified by the deadline rules; candidate cancellation or expiration never retries; an attempt active timeout with time left is temporary only if no remote mutation occurred or cleanup is proven |
| Verified provider throttling and proven cleanup | `throttled`; use trustworthy delay if available, otherwise stop the batch |
| Free account, capacity, unsupported adapter, unknown provider response, malformed evidence, unsafe runtime configuration | `terminal`, finish inconclusive without retry |
| Unknown mutation completion, registration identity mismatch, missing recovery material, unproven cleanup | `unsafe`, stop batch and retain recovery state |

Add reason `validation_observation_timeout` for polling that exhausts its ten second attribution budget without a matching license. It can be temporary only when every successful response retained the same registration and device identity and cleanup subsequently succeeded. A malformed or unclassified response stays terminal, and an identity mismatch is unsafe. Keep `validation_timeout` for a timed out command. Existing generic `validation_provider_error` and `validation_unattributed` are never silently made retryable.

For retry number `r`, where `r=1` follows attempt 1, compute lower bound `L=min(5s*2^(r-1),60s)` and upper bound `U=min(10s*2^(r-1),60s)`. Draw a uniform duration in `[L,U]` using a concurrency safe random source controlled by the coordinator. With five attempts the waits range from 5 to 10, 10 to 20, 20 to 40, and 40 to 60 seconds. Inject the draw and monotonic clock for deterministic verification. Save the draw once; replay must not redraw.

A retry requires a remaining attempt, unexpired candidate budget, a permitted classification, proven cleanup, removed container, and a healthy batch. If the chosen wait reaches or exceeds the candidate deadline, finish the item with `candidate_timeout` immediately; do not shorten the wait or issue a probe. If both attempt count and time are exhausted, report `attempt_limit` first. A new attempt uses a fresh attempt ID, request token, container, and registration. A store replay uses its original token and never creates a replacement external attempt.

For a trustworthy server delay, the coordinator waits the full supplied duration from its own receipt time, without subtracting transport or cleanup time. It sets a shared monotonic mutation pause to the maximum of the current pause and that deadline. This conservative rule requires no synchronized worker clocks. A capable backend must expose throttling and its delay at mutation completion, before admission is released; receiving it again at final cleanup may extend the pause but never shorten it. The effective retry wait is the maximum of jitter and the remaining shared pause. Do not cap a server delay at the jitter cap. Waiting candidates whose deadlines expire finish without further mutation. Already admitted commands may finish and cleanup remains allowed.

The current pinned Docker adapter exposes no verified server delay. It always returns delay absent and therefore stops the batch on throttling, using `validation_rate_limited`. Do not parse invented fields or scrape an unverified delay from logs. The optional delay contract is exercised with a controlled capable backend; adding a real parser requires separate evidence and a later contract update. New profiles and immediate retries within the current batch cannot evade this stop. Cross invocation cooldown persistence is outside this manual slice.

### Cancellation, persistence failure, and recovery

The coordinator latches the first fatal condition and stops new item dispatch, retries, and mutation admission. Conditions include cancellation, unknown cleanup, untracked container, journal failure, store mutation failure, local container removal failure, output failure, and throttling without delay. Ordinary terminal candidate failures let other items continue. Send cancellation and collect cleanup across all active sessions under one shared 60 second deadline, not 60 seconds per session. Do not replace it on each error or retry.

Ordinary cleanup has a separate 60 second allowance per attempt. In the worker it begins when checking ends and cleanup starts, using a fresh local monotonic context. The host begins its collection allowance when it first receives an observation, cleanup phase, or final receipt, or when it first requests cleanup after an error or deadline, whichever comes first. Host collection includes worker completion, durable receipt persistence, container removal, and registry entry removal. Status polling before that transition remains under the active attempt deadline, so a lost transition cannot leave the host waiting indefinitely. A remote worker's earlier cleanup start is never extended by a later host request.

On a batch stop, clip every existing host collection deadline to the shared shutdown deadline. A session not yet cleaning gets only the shared remaining host allowance. Worker cleanup already in progress keeps its original deadline; do not restart it to obtain another 60 seconds. The operation lock prevents simultaneous recovery and observation. Recovery waits only within the remaining collection deadline and never replays registration or attachment. At the deadline, retain unresolved manifests and return failure. A surviving worker may still save its cleanup receipt within its own original allowance for later recovery.

After remote collection, known safe local finalization gets one independent five second context for submissions and cancellation. It is not a promise that `Close` returns within five seconds. Existing entered SQLite work can outlive caller cancellation, and the store keeps its writer lock and resources until that work finishes. Issue no new mutations after a timeout or uncertain acknowledgement, wait for safe store close, and only then release its ownership. The existing subprocess reap allowance can also add up to two seconds. This feature preserves those resource safety rules rather than adding forced process termination or early lock release. The same principle applies to entered private file operations: their operating system completion is not a hard real time guarantee.

If store acknowledgements are still reliable, save eligible definitive results whose cleanup has been proven, resolve the remaining unfinished attempts as inconclusive with the actual stop reason, mark unstarted items skipped, and finish the run failed or cancelled. Preserve already committed final items. A cleanup failure cannot promote privately observed confirmation to public evidence. A fully exhausted set of candidate attempts without a fatal batch condition finishes the run completed, while the CLI still returns failure for inconclusive items.

If any store mutation returns an uncertain error, issue no further store mutations, including finalization or replacement completion payloads. Remote cleanup and private journal writes are still required because they own separate external state. A bounded final read can expose committed progress without authorizing another write. A journal persistence error likewise prohibits new external mutations but does not justify deleting containers or discarding worker receipts. Cleanup operates only on identities and phases already durably established.

The shared journal remains at the platform default validator directory, independent of profile settings, protected by the existing nonblocking `owner.lock`. Version 2 stores a maximum of four session entries and remains present even when empty. Registry replacement retains private temporary files, synchronization, atomic rename, and directory synchronization. Only the coordinator writes it. Every new container intent is committed before creation; every returned cleanup receipt is committed before owned container removal and entry removal.

A failed registry replacement is an uncertain acknowledgement. Rename may have succeeded before directory synchronization failed, so the visible file may contain the new payload. Stop new work and do not overwrite it with a presumed old snapshot. Retain session recovery material and stop destructive local removal unless cleanup and its host receipt were already durably acknowledged. On the next recovery, acquire the owner lock, read and validate the actual version and payload, and reconcile its recorded identities and labeled containers. Missing or invalid state blocks work; uncertainty never authorizes a new attachment.

Retain per session registered operation identity and attachment completion marker in the worker journal. New checks reconcile all registry entries and inspect all labeled containers under the same owner. An untracked container, daemon mismatch, malformed entry, missing container without a cleaned receipt, or unknown cleanup blocks the whole owner. Reconcile entries in creation order, with session identity as tie breaker, and attempt cleanup of other safely identified entries even if one stays unresolved. Startup and explicit recovery share one five minute total recovery deadline across entries. No registration or attachment occurs during recovery.

SQLite writer recovery marks unresolved attempts inconclusive and nonterminal batch items interrupted, preserving completed and skipped items, retry plans, policy, and all real timestamps. The run becomes interrupted under the existing rule. It does not clear remote journal entries, recreate lost confirmation, resume pending items, or debit a later batch's budget. `check show` remains a read and therefore can show a still running record after a crash until a writer performs recovery.

### Security and observability

The existing operating system account is the only actor. State and journals stay private, with the existing ownership and symlink checks. Retain the local Unix Docker endpoint, immutable Linux arm64 image, no host registration fallback, and no secret output. A shared owner serializes profiles even when the same key has different candidate IDs in their databases. Within a profile, exact key uniqueness and the unresolved attempt constraint enforce key identity.

Use `log/slog` on stderr for batch admission, attempt admission and completion, retry planning, mutation admission and completion, and batch stop. Fields are run, candidate, attempt, session, attempt number, active count, effective ceiling, fixed operation name, elapsed duration, wait duration, and stable reason. No keys, account identifiers, raw vendor output, or credentials. Monotonic elapsed values establish concurrency and spacing; UTC timestamps serve saved history. No metrics service or new logging settings are introduced.

New stable diagnostics include `validation_batch_inconclusive`, `validation_candidate_timeout`, `validation_rate_limited`, and `validation_batch_stopped`. Retain existing specific configuration, runtime, privacy, state, protocol, and cleanup codes. The first fatal coordinator event determines the batch stop reason; later cleanup failures remain visible per item and in the recovery journal. Cancellation retains signal exit semantics regardless of that ordering.

### Value sourcing

| Action | Value | Source |
| --- | --- | --- |
| Select | Ordered candidates and keys | Deduplicated positional IDs resolved through saved candidate rows |
| Start batch | Policy and backend identity | Fully resolved settings, fixed policy version 1 values, and backend identity including immutable image |
| Dispatch | Worker ceiling, active count, candidate deadline | Saved policy, coordinator admissions and releases, host monotonic first admission time |
| Mutate | Earliest next command | Prior observed command completion plus interval, extended by any trusted shared delay |
| Retry | Attempt number and remaining count | Saved item attempt details and policy maximum |
| Retry | Failure class and delay | Typed backend result; current Docker server delay is absent |
| Retry | Wait and due timestamp | Bounded random draw and server delay; runtime monotonic wait plus application UTC clock for the saved display time |
| Finish | Entitlement and evidence time | Attributable worker observation and cleanup receipt under the unchanged evidence rule |
| Finish | Eligibility across a timeout or batch stop | Coordinator monotonic receipt time of validated observation, compared with its active deadline and stop latch; no comparison of host and worker wall clocks |
| Report | Status, stop reason, totals, attempt history | Committed run, item, attempt, and detail rows; never inferred from current settings |
| Report | Definitive freshness | Existing candidate evaluation with one fixed `AsOf` and effective recheck interval, unchanged by retries |
| Recover | Session identity and permitted cleanup | Durable registry and worker phases, actual container inspection, and matching cleanup receipts |

### Critical verification scenarios

The executable checklist is in `verify.md`. Its scenarios cover selection and configuration (AC-1, AC-10), observed overlap and pacing (AC-2, AC-3), finite retries and delay handling (AC-4, AC-5, AC-6), cancellation and uncertain state (AC-7, AC-8), complete saved reads (AC-9), migrations and privacy (AC-10, AC-11), and the separately authorized live run (AC-12).

## Migration plan

**Strategy**: Add the batch path alongside existing reads and recovery, prove a single item through it, then make both single and multiple ID checks use that engine. This is a local personal CLI; no simultaneous binary rollout or permanent second checking engine is needed.

1. Apply schema version 4 in one transaction. Add new tables without backfilling invented batch metadata for legacy runs. A migration failure leaves version 3 intact. Readers require the current schema as today.
2. Under validator ownership, detect the legacy version 1 journal. Reconcile its existing container with the recorded image and version 1 cleanup protocol before any conversion. Failure blocks new work and retains recovery material; already acknowledged cleanup progress remains valid. Do not attempt a new attachment or replace the image during recovery.
3. After durable legacy cleanup and owned container removal, atomically replace the journal with an empty version 2 registry. Keep the version 2 file present. A version 1 binary rejects its incompatible manifest instead of interpreting absence as permission to create a session.
4. Require a newly built immutable image advertising worker protocol 2 for all new sessions. Retain read and cleanup support for version 1 receipts only on the legacy recovery path. New version 2 receipts require exact session identity and protocol version. The WARP package version does not change.

**Rollback**: A confirmed rolled back SQL migration retains schema version 3; an uncertain commit is inspected through the normal store recovery rules. Registry replacement has a separate uncertain acknowledgement boundary: a failure after rename can leave version 2 visible, so inspect the actual journal on the next recovery instead of restoring a presumed version 1 snapshot. After successful upgrades, reverting the executable alone is not a downgrade: an old binary must reject the newer schema and journal. Keep the new recovery capable binary available to clean retained sessions. Restoring a local database backup never clears remote recovery ownership, and deleting journals is not a rollback procedure.

**Risks**: A process can die between durable intent and an external acknowledgement. Preserve that uncertainty rather than recreate the operation. The worker protocol and image must change together; a mismatch is a preflight error. Legacy history remains readable with metadata explicitly unavailable.

## Build plan

Tracer Bullet delivery starts with one saved batch item through every layer, then adds real overlap and retry behavior. Each milestone can be reviewed on its own; no milestone authorizes skipping the final live verification.

1. [ ] Prove one explicit ID through policy resolution, migration 004, saved selection, one unsupported attempt, final output, and `check show` after restart. Preserve old history, single ID behavior, and side effect free help. Satisfies AC-1, AC-6, AC-9, AC-10, AC-11.
2. [ ] Prove one real session through protocol 2 initialization, separately admitted registration and attachment, private completion markers, cleanup, and the version 2 registry. Include legacy cleanup conversion and incompatible binary and image rejection before increasing workers. Satisfies AC-3, AC-7, AC-10, AC-11.
3. [ ] Extend the same path to two then four workers, shared mutation admission, bounded channels, complete selection reads, callback output, and fair slot reuse. Measure overlap and the configured ceiling with controlled work. Satisfies AC-1, AC-2, AC-3, AC-9.
4. [ ] Add typed classification, finite retries, saved retry plans, candidate and attempt deadlines, server delay handling, and the current Docker throttling stop. Prove unchanged definitive evidence and no repeated mutation within a session. Satisfies AC-4, AC-5, AC-6.
5. [ ] Complete cancellation, global stop races, uncertain store and journal acknowledgements, cleanup deadlines, multi session recovery, privacy, and output failure handling. Satisfies AC-2, AC-7, AC-8, AC-9, AC-10, AC-11.
6. [ ] Run `/check verify concurrent checks and bounded retries`, including the separately authorized live Docker demonstration. Then run `/test` for meaningful retained regression coverage and the repository formatting, vet, race, and lint gate, with both supported arm64 builds. Satisfies AC-1 through AC-12.

## Consequences

You gain useful overlap with explicit resource and retry budgets, complete saved intent, and clear separation between failed execution and entitlement. The same conservative owner coordinates all profiles.

Mutation serialization limits throughput even with spare workers. More active containers consume more local resources and temporary device slots. Ambiguous cleanup or throttling can stop an entire batch. Retry timing cannot compensate for unknown vendor behavior, and the current pinned adapter cannot expose a server delay. The journal and image upgrade require coordinated recovery support rather than an executable only rollback.

## Follow-up

1. Complete: independent review found six gaps, you approved their corrections, the reviewer confirmed closure, and you accepted the final spec on 2026-10-11. Scope feature 6 links the build milestones.
2. Live verification requires keys explicitly authorized at that stage and available temporary registration capacity. No live checks were performed during design.
3. Source retries, automatic candidate selection, scheduling, cross invocation cooldowns, simultaneous batches, and export formats remain outside this feature.
4. After implementation, durable context needs the new batch, protocol, migration, and recovery contracts. This design does not edit `AGENTS.md`.

## Rationale

Reasoning and alternatives are in [rationale.md](rationale.md).
