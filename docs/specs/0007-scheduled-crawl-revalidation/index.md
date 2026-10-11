# 0007. Scheduled crawl and revalidation

**Date**: 2026-10-11
**Status**: In Progress

## Summary

You can leave `wpc run` running to collect candidates and refresh their validation results on separate schedules. SQLite remembers eligibility, and the shared validator journal remembers provider cooldowns across profiles. Small validation batches reuse the existing check engine. Service installation and live process management remain in the next scope slice.

## Structure

1. [Durable scheduler](0001-durable-scheduler.md) defines eligibility, persistence, coordination, restart, and clock behavior.
2. [Shared validator cooldown](0002-validator-cooldown.md) defines the shared recovery gate, registry migration, and single candidate probes.

Both children belong to scope feature 8. This index owns their shared configuration, CLI, acceptance criteria, and build sequence. Child contracts extend the existing manual crawl and validation paths; they do not introduce a second implementation of either operation.

## Requirements

**User story**: As the local operator, you can keep collecting and checking candidates without issuing each command yourself, restart without losing eligibility, and inspect why work is due.

| ID | Acceptance criterion |
| --- | --- |
| AC-1 | `run` stays in the foreground until cancelled. Crawling and validation are independently enabled, both by default. Crawl and routine recheck intervals default to `1h` and `6h`; the failure interval defaults to `1h`. Each interval used by `run` accepts one minute through 30 days. Invalid startup settings fail before state or external work. |
| AC-2 | One crawl and one validation batch can progress concurrently. A slow batch cannot hold up crawl requests. There is at most one active crawl, one active batch, and one active check per candidate. The configured worker and mutation pacing limits remain effective. |
| AC-3 | Discovery makes a previously unchecked candidate eligible immediately. At each batch boundary, selection orders eligible candidates by due time, then candidate ID. A batch contains at most twice the configured worker count, four by default and eight at most. There is no total collection cap, missed interval queue, or goroutine per saved candidate. |
| AC-4 | Confirmed and invalid candidates become due one routine interval after terminal check completion. An inconclusive check becomes due one failure interval after terminal completion. Within batch retries retain their existing bounded policy. Rediscovery and inconclusive results never refresh earlier definitive evidence. |
| AC-5 | Startup dispatches overdue work once and retains future due times. It creates fresh runs rather than resuming interrupted source cursors or unused check attempts. Existing candidates and manual history contribute to initial scheduling. Interruption does not lose candidate eligibility. |
| AC-6 | A crawl becomes due one crawl interval after terminal completion, including ordinary source failure. Exact matching manual crawl coverage advances its schedule; a partial selection does not. Manual checks advance the selected candidates. Changed enabled coverage is due immediately unless a matching manual crawl has already satisfied the changed configuration. |
| AC-7 | Startup applies changed intervals to saved timing bases. Disabling a lane preserves its pending work. Reenabling it makes overdue work eligible once. Automatic dispatch pauses when UTC is behind the saved scheduling checkpoint, and forward jumps do not replay elapsed intervals. Evidence timestamps remain unchanged. |
| AC-8 | Docker unavailability and unproven remote cleanup pause validation and receive bounded future recovery attempts while crawling continues. No attachment occurs before cleanup is proven. Permanent configuration, identity, permission, and journal errors are explicit and do not enter an automatic mutation loop. |
| AC-9 | Docker throttling establishes a durable cooldown shared across profiles and respected by manual checks. After its deadline, one candidate probes the provider. A healthy response plus proven cleanup restores normal validation; another throttle or failed probe establishes another cooldown. Restart, another profile, and an older executable cannot bypass this gate. |
| AC-10 | `run` owns the state writer for its lifetime. Competing manual writers and duplicate runners report busy without external work. Saved reads remain available. All SQLite and journal mutations retain the existing ownership and uncertain acknowledgement rules. |
| AC-11 | `schedule show [candidate-id]` returns saved scheduling facts as text or schema version 1 JSON without migration, Docker calls, or eligibility changes. Summary counts use one database snapshot. A candidate view explains its due time, basis, interval, and history references without revealing its key. |
| AC-12 | Each terminal crawl or validation batch produces one stdout summary with its saved run ID and counts. Diagnostics use stderr and stable codes. Disabled lanes, empty candidate sets, output failure, cancellation, and source failure have defined behavior. Output failure triggers cleanup and stops the runner. |
| AC-13 | Schema version 5 and validator registry version 3 preserve existing history. Migration failures leave a usable earlier version or an explicitly blocked newer version, never a fabricated clean state. Eligibility changes commit with the discovery, terminal item, or recovery that causes them. |
| AC-14 | Cancellation stops admission, cancels external work, and shares the existing 60 second remote cleanup and five second local finalization allowances across active lanes. Completed evidence survives. Store or journal uncertainty stops unsafe mutations and leaves truthful recovery state. |
| AC-15 | Controlled checks prove every criterion on macOS and Linux arm64, including selection from all 338 candidates, source and validation overlap, crash recovery, and competing profiles. Real provider calls use separately authorized keys and preserve host registration. |

## Decision

**Chosen option**: Use Go standard library timers over explicit persisted eligibility, with one application coordinator and the existing SQLite store, source adapter, and Docker batch engine.

You confirmed this design on 2026-10-11 after independent review and the seven approved fixes. It is linked to scope feature 8. Its status remains `Proposed` until implementation begins.

The runner keeps one writer open. Manual commands remain wrappers that acquire their own writer and call the same operation cores. The shared Docker owner is acquired per batch or recovery attempt and released during idle periods. The registry gate remains authoritative across those ownership intervals.

The accepted boundary is duration based scheduling. There are no cron expressions, service installers, hot configuration reload, local control socket, remote API, or automatic host key application. No new library, community skill, or MCP server is required.

### Shared contract

The scheduler child owns profile eligibility; the validator child owns permission to begin remote validation. Candidate eligibility alone never authorizes an attachment. The effective dispatch condition is an enabled lane, due candidate, unoccupied batch slot, healthy profile clock, acquired validator ownership, proven cleanup, and an admitting shared gate.

There is no transaction across SQLite and the journal. Admission records the conservative journal intent before remote mutations. For a live invocation, acknowledged terminal SQLite persistence precedes opening its gate. Abandoned invocation recovery is a separate path: after acquiring the released owner lock and proving remote cleanup, another profile may replace an abandoned phase with a conservative cooldown without opening the abandoned profile's database. Expiry permits a fresh probe, whose own acknowledged result is required before opening the gate. The abandoned SQLite run remains unfinished until its own writer recovers it; another profile never rewrites its evidence or claims that it completed.

## Feature design

### Configuration

Retain the existing private JSON format and defaults, JSON, explicit flags precedence. Read configuration once per invocation. The following names are the public contract.

| JSON field | `run` flag | Default | Validation |
| --- | --- | --- | --- |
| `crawl_enabled` | `--crawl-enabled` | `true` | Boolean; explicit false overrides JSON |
| `validation_enabled` | `--validation-enabled` | `true` | Boolean; explicit false overrides JSON |
| `crawl_interval` | `--crawl-interval` | `1h` | Go duration, `1m` through `720h` |
| `recheck_interval` | `--recheck-interval` | `6h` | Existing positive duration parsing; `run` additionally requires `1m` through `720h` |
| `failure_interval` | `--failure-interval` | `1h` | Go duration, `1m` through `720h` |

All three intervals are validated by `run` even when a lane is disabled. Both lanes disabled is `schedule_disabled`. Enabled crawling requires at least one enabled configured source. Enabled validation requires `validator=docker` and the existing valid immutable image setting. The default unsupported adapter therefore requires `--validation-enabled=false` for collection alone. This is a capability error before opening state, not a stream of unsupported scheduled attempts.

`run` also accepts the existing configuration, state, validator, worker, attempt, mutation interval, and candidate timeout flags. It uses every enabled configured source and its configured page limit. There are no `run --source`, `run --page-limit`, candidate selection, batch size, or schedule reset flags. Use configuration to change automatic coverage. Existing manual command flags keep their meaning, and the new enable flags affect only `run`.

Existing commands retain positive `recheck_interval` values outside the runner range. Scheduling arithmetic retains the exact duration in nanoseconds and rounds a computed deadline upward to the next UTC microsecond. This avoids silently changing existing freshness parsing. Settings that cannot be represented safely as a timestamp are rejected.

No new environment variables, credentials, service accounts, or runtime image downloads are introduced. Docker setup remains the existing prerequisite for actual validation.

### API surface

All surfaces use the local account and private filesystem boundary. There is no network authorization protocol.

| Action | Inputs | Outputs | Important errors |
| --- | --- | --- | --- |
| `wpc run` | No positional arguments; flags above | Terminal run summaries until cancellation | `schedule_disabled`, `validation_unsupported`, `state_busy`, fatal state or journal error |
| `wpc schedule show` | `--config`, `--state-dir`, `--format text\|json` | Saved crawl schedule, candidate totals, earliest pending due time, local validation wait, clock checkpoint | `state_not_initialized`, `state_migration_required`, `output_error` |
| `wpc schedule show <candidate-id>` | One positive decimal ID; same flags | One candidate eligibility explanation and whether its latest saved item is active | `state_record_not_found`, invalid arguments, `output_error` |
| Existing `crawl` | Existing source and page flags | Existing report, now tagged with manual origin | Existing errors; matching terminal coverage also advances scheduling |
| Existing `check` | Existing full candidate selection and policy | Existing report, now tagged with manual origin | Also `validator_cooldown`, `validator_probe_required`, `validator_clock_regressed` |
| Existing `validator recover` | Existing validator settings | Existing cleanup report | Cleanup remains allowed during cooldown; it does not clear a provider restriction |

The `schedule` parent shows help. Format defaults to text. Validate the format and optional ID before configuration or state access. Configuration is read only to locate state and validate its existing format; schedule inspection does not apply current intervals to saved rows. It never instantiates the Docker adapter or reads the shared journal.

### Inspection output

Use one SQLite read transaction and one application supplied `as_of` timestamp for the whole response. First perform a read of `scheduler_state` to establish the transaction's snapshot; a deferred `BEGIN` alone is insufficient. Only then sample the supplied application clock, normalize it to UTC microseconds, and use that fixed `as_of` for every remaining query and comparison in the same snapshot. This prevents an ordinary intervening writer commit from appearing as clock regression. Close the reader before delivering prepared output through the existing output staging mechanism. Text and JSON expose equivalent facts. Null JSON values render as `none` in text. Times use UTC RFC3339 with microsecond precision and a `Z` suffix. Durations use their canonical Go duration string. IDs and counts are JSON integers.

Both JSON forms have `schema_version: 1`, `view` (`summary` or `candidate`), `as_of`, and `clock_regressed`. The schema belongs to schedule inspection and does not change export schema version 1.

The summary has `checkpoint_at`; `validation_recovery_pending`, a boolean; `crawl`, either null or an object with `coverage`, `due_at`, `reason`, `basis_at`, `interval`, `basis_run_id`, and `active_run_id`; `candidates` with `total`, `active`, `due`, `waiting`, and `next_due_at`; and `validation_wait`, either null or an object with `reason`, `basis_at`, `retry_at`, and `observed_at`. Coverage is an ordered array of `channel` and `page_limit` objects. `active` counts candidates with pending or active items in an active batch, not only workers currently executing. `due` excludes those candidates and counts `due_at <= as_of`; `waiting` is the remaining count. `next_due_at` is the minimum due time among nonactive candidates, including overdue ones, or null. Thus `total = active + due + waiting`.

The candidate form has `candidate_id`, `due_at`, `reason`, `basis_at`, `interval`, `basis_run_id`, `basis_attempt_id`, `active_run_id`, and `eligible_by_time`. The last field is `due_at <= as_of` and no active item; it is not a promise that a lane is enabled or the shared validator gate is open.

`validation_wait` is the last local observation. A shared gate can change through another profile, so this view does not claim current Docker readiness or process liveness. Summary counts remain useful during a clock pause; `clock_regressed=true` explains why automatic dispatch is suspended. A migrated empty database returns zero counts and a null crawl schedule. A database that does not exist remains an error and is not created by inspection.

### Runner output and diagnostics

Emit exactly one text line per terminal child run, after its final durable state is known:

```text
kind=crawl origin=scheduled run_id=12 status=completed reason=none saved_candidates=18 saved_sightings=24
kind=validation origin=scheduled run_id=13 status=completed reason=none selected=4 confirmed=2 invalid=0 inconclusive=2 unstarted=0
```

Counts come from the saved reports, not worker counters. Partial failure reports include committed totals. A run without acknowledged final state is not reported as terminal. Interleaving follows completion order. Empty validation selections produce no run and no stdout line. Startup, lane pauses, recovery outcomes, and resumption use stderr. Log pause transitions and recovery attempts, not every timer tick. Logs contain IDs, channel identities, times, counts, and stable codes, never keys or raw provider responses.

An ordinary source or candidate failure does not terminate `run`. Fatal errors return exit code 1. Preserve process signal exits 130 for SIGINT and 143 for SIGTERM. Broken stdout or diagnostic writes initiate cancellation and cleanup; a failed stream is not written again. The application retains the failure even if a secondary cleanup operation also fails.

### Value sourcing

| Produced value | Source |
| --- | --- |
| Enabled lanes and startup intervals | Effective JSON and explicitly changed flags, with defaults above |
| Crawl coverage identity | Ordered normalized enabled source plans, including page limits |
| Candidate first eligibility | First persisted discovery observation, never publication time |
| Routine or failure timing basis | Terminal item or legacy attempt timestamp; explicit recovery time after interruption |
| Due time and displayed interval | Saved basis plus the exact applied duration, with upward microsecond rounding |
| Matching manual crawl | Equality of saved ordered source plans with the effective full configured plan |
| Selection order and size | Eligibility index on due time and ID; `2 * check_workers` or one probe candidate |
| Active run references | Existing running crawl and validation item relationships |
| Outcome and freshness | Existing attempt evidence and freshness evaluation, independent of scheduling |
| Shared cooldown | Validator gate saved under shared ownership, using its originating failure interval |
| Local validation wait | Typed acquisition or recovery outcome observed by this runner, or a copied shared deadline |
| Recovery obligation | `scheduler_state.validation_recovery_pending`, set before acquisition and cleared only by a typed successful cleanup inspection |
| Clock pause | Current UTC compared with the saved profile checkpoint; shared gate has its own checkpoint |
| Summary counts and next due time | One SQLite snapshot using the response's fixed `as_of`, sampled after the snapshot is established |
| Run summaries and origin | Saved run, crawl report, batch report, and new `runs.origin` |
| Policy after an interval edit | Startup settings applied to saved timing bases; no historical evidence rewrite |

### Security and invariants

Preserve private file modes, ownership checks, local Unix Docker endpoints, immutable images, key disclosure restrictions, and host isolation. No schedule output flag exposes keys. Read operations do not acquire writer or validator ownership. A losing writer performs no source or Docker work. Shared registry permission, identity, and version errors fail closed.

SQL remains inside the store. Network work never holds a SQL transaction. One coordinator serializes state changes, and existing request tokens make replay of an identical local mutation safe. Replaying a token never authorizes repeating a source request or registration. Strong consistency applies within a database transaction; the conservative gate ordering handles the separate journal.

### Critical test scenarios

1. A controlled crawl discovers candidates, a scheduled batch checks them, and inspection in another process explains their next due times. Verify AC-1, AC-3, AC-4, AC-11, AC-12, AC-13.
2. Hold validation work and retry waits open while another scheduled crawl completes. Verify worker ceilings, one active batch, and serialized mutation admission. Verify AC-2, AC-10.
3. Restart after each durable boundary, change intervals and coverage, and compare manual and scheduled history. Verify AC-5, AC-6, AC-7, AC-13.
4. Throttle one profile, attempt manual and scheduled checks from another, crash a probe, and recover without bypassing cooldown or cleanup. Verify AC-8, AC-9, AC-14.
5. Deny private file access, contend for locks, regress UTC, close output pipes, and inject uncertain acknowledgements. Verify AC-7, AC-10, AC-11, AC-12, AC-14.
6. Make all 338 candidates due and traverse repeated bounded selections until each receives one terminal check, with a frozen clock and long next intervals. Verify AC-3 and AC-15.

## Build plan

Follow the project Tracer Bullet approach and Beta verification workflow. These are complete paths through the required layers, not separate database, backend, and CLI phases.

1. Prove one foreground scheduled crawl through settings, schema version 5, shared writer operation core, stdout summary, and `schedule show` after restart. Include the complete target schema so migration remains one atomic version change. Verify AC-1, AC-6, AC-10, AC-11, AC-12, AC-13.
2. Prove one automatically selected candidate through the real batch engine, saved eligibility, and a subsequent due time. Use controlled adapters first; the production command still rejects unsupported validation. Add history initialization, manual participation, and exact coverage matching. Verify AC-3, AC-4, AC-5, AC-6, AC-13.
3. Prove registry version 3 conversion, a durable cooldown, one probe, and manual behavior through the existing Docker owner boundary. Add interrupted gate recovery before enabling unattended real checks. Verify AC-8, AC-9, AC-10, AC-13, AC-14.
4. Allow crawl and batch work to overlap through the shared mutation coordinator. Complete bounded selection across the collection, lane controls, configuration restart behavior, and local recovery waits. Verify AC-1, AC-2, AC-3, AC-7, AC-8, AC-10.
5. Complete clock pauses, fault recovery, prepared inspection output, signal and pipe handling, and shared shutdown deadlines. Exercise both arm64 platforms and the controlled 338 candidate dataset. Verify AC-5, AC-7, AC-9, AC-11, AC-12, AC-13, AC-14, AC-15.
6. Run `/check verify scheduled crawl and revalidation`, then `/test scheduled crawl and revalidation`, retaining meaningful boundary regressions. Run the repository gate and builds. Verify AC-1 through AC-15.

## Consequences

**Benefits**: Saved eligibility survives restarts, old candidates receive fair selection, and source collection can continue while validation is unavailable. Existing run reports remain the execution history.

**Costs**: Small batches can delay a new candidate behind a slow batch. The two persistence stores need conservative ordering. Shared throttling can delay otherwise independent profiles, and clock correction may require operator action. Manual writes require stopping the runner.

**Compatibility**: Schema version 5 and registry version 3 block older binaries. A rollback needs compatible software or restoration of a complete private backup after remote state is reconciled. Container images retain worker protocol 2; this feature does not justify rebuilding the tested image.

## Follow-up

1. Keep service installation, live process status, and start or stop management in scope feature 9. It must use this foreground command and account for the selected signal exits and persistent restrictions.
2. Keep history retention, calendar schedules, live reload, user queue controls, and adaptive intervals outside this slice.
3. After implementation, durable project context should describe the new schema, shared registry, and command behavior through `/sync`.

## Rationale

Reasoning and alternatives are in [rationale.md](rationale.md). Runtime acceptance steps are in [verify.md](verify.md).
