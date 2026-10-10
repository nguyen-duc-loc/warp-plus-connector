# 0002. Persistent key state

**Date**: 2026-10-10
**Status**: Accepted

## Summary

Your store retains every distinct candidate, its source messages, each validation attempt, and run history across restarts. A temporary check failure preserves earlier definitive evidence without making it fresh again. This slice supplies a Go store interface and proves persistence through that interface. CLI commands, crawling, real validation, scheduling, and export presentation belong to later features.

## Requirements

**User story**: As the local operator, you can recover and inspect saved discoveries and evidence without losing provenance or mistaking a temporary failure for invalid entitlement.

| ID | Acceptance criterion |
| --- | --- |
| AC-1 | Through the public Go store interface, initialize state, save a discovery, close the process, and read the same candidate and provenance in another process. Add no CLI commands; help and version retain their existing absence of state, configuration, and network access. |
| AC-2 | Candidate identity is exact text after trimming surrounding whitespace. Case and internal characters remain significant. Rediscovery merges candidates, retains every distinct source message, and links every observing crawl run without duplicate links within a run. |
| AC-3 | Source publication time is optional. Discovery, attempt, evidence, finish, and recovery times remain distinct. Missing values are never invented. |
| AC-4 | Every validation attempt is retained. Reads distinguish unchecked, confirmed, invalid, and inconclusive outcomes, and expose the latest completed attempt separately from the last definitive result. An inconclusive attempt never changes that definitive result. |
| AC-5 | Freshness uses the last definitive confirmation and an explicit evaluation time and interval. An inconclusive retry does not refresh it. Expired confirmation is stale; future dated confirmation is not fresh and carries a clock anomaly. |
| AC-6 | A discovery write commits one message and all its candidates atomically. An interrupted transaction leaves either the complete message write or no part of it, while earlier committed messages remain usable. |
| AC-7 | Only the advisory lock owner initializes, migrates, recovers, or writes state. Lock contention returns busy immediately. Readers can inspect committed state while the writer operates, without migrating or changing application records. |
| AC-8 | The next writer marks unfinished runs interrupted and unresolved attempts inconclusive. Recovery time is explicit and actual finish time remains absent. Completed evidence is unchanged and repeated recovery makes no further changes. |
| AC-9 | A traversal of 338 controlled candidates returns all 338 exactly once. Its first page fixes the highest candidate ID included. Later inserts are excluded, while each subsequent page reads current details. Every history collection is also paginated, with no fixed total cap. |
| AC-10 | New state is private to the operating system account. Existing overly broad permissions cause a clear error without automatic permission changes. Errors and diagnostics do not expose keys, message text, raw validator output, or registration credentials. |
| AC-11 | The writer applies versioned migrations before work. Failed migrations preserve the prior schema and data; newer unsupported schemas fail explicitly. Readers reject schemas requiring migration or absent state. Only the writer initializes missing state; damaged state is never silently replaced. |
| AC-12 | All candidates, sightings, observations, runs, and attempts remain retained. Retrying a mutation with the same operation identity produces no duplicate record; conflicting terminal results fail explicitly. |

## Decision

**Chosen option**: A relational history store with normalized records and derived summaries. Reuse SQLite through `database/sql` and `modernc.org/sqlite`, explicit SQL, WAL mode (a journal that permits concurrent readers), and the single writer contract in spec 0001.

This is a feature spec for scope item 3. You confirmed the complete design on 2026-10-10 after independent review and its approved fixes. Its lifecycle remains `Proposed` until implementation begins.

Internal implementation recommendations are explicit here. Use one embedded, ordered SQL migration sequence with `PRAGMA user_version`, rather than a migration framework for one initial schema. Use the operating system advisory file lock through Go's `syscall.Flock` on the two accepted platforms, rather than adding a locking dependency for this narrow platform set. Record the exact tested SQLite driver version in `go.mod` during implementation; this design does not claim a version has been tested.

Introduce `internal/state` for domain records and `internal/store` for persistence when the first working path needs them. Domain records contain no Cobra, HTTP, or driver types. The store is a concrete type with explicit reader and writer handles; callers may define small interfaces where substitution is useful. No configuration parser or application orchestration package is required for this slice.

## Feature design

### Data model

The table below is the confirmed logical model with two operational details made explicit: recovery timestamps and caller supplied request tokens for safe retries. IDs are SQLite generated positive `INTEGER` values, returned as Go `int64`. IDs are never reused. Required strings are nonempty valid UTF-8 without NUL. Optional values use SQL `NULL`, not empty sentinel strings.

Times are Go `time.Time` at the interface, normalized to UTC and stored as signed Unix microseconds in `INTEGER` columns. Reject zero Go times and values outside the storage representation. Normalization drops monotonic clock metadata and submicrosecond precision. Clock order is not used to identify the latest attempt.

| Record | Required stored fields | Optional stored fields | Keys and relationships |
| --- | --- | --- | --- |
| `candidates` | `id`, `key_text` | None | Primary key `id`; unique `key_text` using binary comparison |
| `sightings` | `id`, `candidate_id`, `source_id`, `message_id` | `message_url`, `published_at` | Primary key `id`; foreign key to candidate; unique `(candidate_id, source_id, message_id)` |
| `crawl_observations` | `run_id`, `sighting_id`, `observed_at` | None | Composite primary key `(run_id, sighting_id)`; foreign keys to run and sighting |
| `runs` | `id`, `request_id`, `kind`, `status`, `started_at` | `finished_at`, `recovered_at`, `reason_code` | Primary key `id`; unique `request_id` |
| `validation_attempts` | `id`, `request_id`, `run_id`, `candidate_id`, `backend_id`, `backend_version`, `started_at` | `finished_at`, `recovered_at`, `outcome`, `reason_code`, `evidence_json` | Primary key `id`; unique `request_id`; foreign keys to validation run and candidate |

Candidate and sighting `first_seen_at` and `last_seen_at` are logical fields computed as minimum and maximum observation times. They are not duplicate stored columns. A candidate and sighting are created only with an observation in the same transaction, so both times always exist. A backward clock movement can make a new observation earlier than a previous one; reads preserve those supplied values.

One candidate has many sightings and attempts. A sighting belongs to one candidate, but the same source message can yield several sightings for different candidates. Crawl runs and sightings have a many to many relationship through observations. Validation runs have many attempts, each for one candidate. No source configuration entity is introduced: source adapters own stable source identifiers and later configuration owns which sources are enabled.

Foreign keys restrict deletion. There are no deletion, pruning, or key replacement operations. Enforce permitted enum values and time nullability with SQL constraints where practical. Enforce run kind and active status inside the same transaction as each child mutation. A partial unique index on `candidate_id` where `outcome IS NULL` permits at most one unresolved attempt for a candidate across all runs.

Indexes support `(candidate_id, id)` on sightings, `(sighting_id, observed_at, run_id)` on observations, `(candidate_id, id)` and `(run_id, id)` on attempts, and `(kind, id)` on runs. The observation primary key supports run history reads. Latest summaries query attempt ID order, not wall clock order. Avoid caches and persisted counters.

### Discovery and identity

`RecordMessage` accepts a running crawl run ID, an opaque stable source ID, an opaque message ID, an observation time, optional publication time and URL, and a collection of candidate strings. Apply Go `strings.TrimSpace` to each key, reject empty results, and compare the remaining bytes exactly. Do not fold case, remove internal whitespace, or apply a speculative WARP format rule. Actual candidate extraction belongs to a source adapter.

Source and message IDs are required and compared exactly; the store does not infer channel identity from a URL. Optional URLs must be absolute HTTP or HTTPS URLs without user information, query, or fragment, preventing accidental storage of credentials in links. The store never fetches them. An adapter may omit a URL that does not meet this contract.

Validate all inputs before committing any part of a message. Deduplicate repeated keys in that input. Upsert candidates and sightings, then insert observations. Existing observation links remain unchanged on retry, including their first supplied time for that run. A sighting keeps its first known nonnull publication time and URL; later discoveries may fill an absent value but do not replace a known one. Empty candidate input produces an empty result and no message record, since this store retains key provenance rather than every crawled post.

Return candidate and sighting IDs in normalized key byte order, along with counts of newly created candidates, sightings, and observation links for that transaction. Repeating a successful write returns the same identities and zero new counts. Do not store message bodies.

Check for a fully committed replay before rejecting a terminal crawl run. If every supplied normalized key already has its sighting and observation link in that run, return the saved identities and zero counts without changing metadata. This permits a retry after commit, lost acknowledgement, and recovery. If even one requested link is missing, a terminal run returns `ErrRunClosed` without partial changes. A retry does not resurrect an interrupted run; new discoveries require a new run.

### Attempts, evidence, and run transitions

Run kinds are `crawl` and `validation`. A run starts `running` and can finish as `completed`, `failed`, or `cancelled`. Recovery alone changes a running run to `interrupted`. Finished runs cannot accept new observations or attempts. A run outcome describes execution, not entitlement; a completed validation run may contain invalid or inconclusive results.

An attempt starts with no outcome. Completion sets `confirmed`, `invalid`, or `inconclusive` once. Every actual retry uses a new attempt and request token. Repeating a store call for the same actual attempt uses the original token. One candidate cannot begin its next attempt until the previous one is resolved; different candidates may be active concurrently within the owning process.

Request tokens are caller generated nonempty strings from `crypto/rand.Text()` and stay stable when retrying the same mutation. The store treats them as opaque. Repeating `StartRun` or `BeginAttempt` with a matching token and the same normalized creation inputs returns the original record, including its present status. A reused token with different inputs returns `ErrConflict`. Repeating an identical terminal completion succeeds without changes; a different completion conflicts. Idempotency comparisons happen before the rejection of an already terminal run or attempt, so retries after successful completion remain possible.

`CompleteAttempt` takes an explicit finish time, outcome, stable reason code, and optional typed evidence. Reason and rejection codes match `[a-z][a-z0-9_]{0,63}`, such as `transport_error`, rather than carrying free text error output. Require a reason for invalid and inconclusive outcomes; a confirmed result has no reason code. The store rejects raw JSON input: it serializes a closed Go evidence type with schema version `1` through `encoding/json`.

The evidence type contains `kind`, `observed_at`, and exactly one relevant fact: `account_type` for kind `account_type`, or `rejection_code` for kind `rejection`. A confirmed result requires `account_type` evidence with the exact value `Unlimited`, matching the first confirmation class in spec 0001. An invalid result requires `rejection` evidence with a nonempty normalized rejection code. An inconclusive result has no definitive evidence payload. Backend ID and version are required when the attempt begins; the unsupported adapter uses its application version, with `dev` allowed for local builds.

The candidate foreign key attributes the stored result to the attempted candidate. The calling validator remains responsible for proving that its external evidence actually belongs to that candidate and isolated registration. The store validates shape and consistency, not entitlement truth. Only controlled fixtures may supply definitive results in this slice. No real validator is enabled, no host WARP registration is touched, and storage success does not pass the feasibility gate. Unknown account types and unproven rejection responses remain inconclusive. The gate must define its normalized rejection codes before a real adapter can produce invalid results.

`FinishRun` requires all its validation attempts to be resolved. A caller cancelling normal work completes unresolved attempts as inconclusive with reason `cancelled`, then finishes the run. Shutdown orchestration and its timeout belong to the background feature. Store cancellation rolls back its current transaction but does not fabricate completion for application work.

After migration, `OpenWriter` performs recovery in one transaction before returning the handle. It captures one recovery time from a supplied UTC clock, sets unresolved attempts to inconclusive with reason `interrupted`, sets running runs to interrupted with that reason, and sets their `recovered_at`. Actual `finished_at` stays null. Previously terminal records are untouched. A repeated recovery is a no operation. Recovery records uncertainty; it does not assert that any future remote attachment was cleaned up or safe to retry. Durable remote cleanup recovery remains owned by the feasibility gate.

### Reads, freshness, and cursors

A candidate summary returns its ID and recoverable key, first and latest discovery times, latest completed attempt, last definitive attempt, and optional unresolved attempt. No completed attempt means `latest_outcome=unchecked`; a recovered attempt counts as completed for this purpose despite an unknown actual finish time. A definitive attempt means confirmed or invalid. A later invalid result supersedes a prior confirmation; all earlier evidence remains accessible in history.

Freshness is derived from the last definitive result. If it is absent or invalid, return `not_applicable`. For a confirmation, use its evidence `observed_at`, an explicit caller supplied `as_of`, and a positive caller supplied `max_age`. Age below `max_age` is `fresh`; age equal to or above it is `stale`. Evidence after `as_of` returns `unknown` with `clock_anomaly=true`, never fresh. The flag describes that evaluation, not a stored state. Once the clock catches up, ordinary comparisons apply. A prior inconclusive attempt does not change the evidence time. Run and attempt reads also expose a derived `clock_anomaly`: finish or recovery before start, or attempt evidence before start or after a known finish. Preserve these event times rather than clamping or ordering by them.

The eventual application supplies `max_age` from effective recheck settings, whose default is six hours in spec 0001. The store does not read configuration or assume that default. Callers keep `as_of` and `max_age` fixed across one traversal.

Every collection uses bounded cursor pages. Default page size is 100, allowed range is 1 through 1000; these are per page bounds, never total result caps. An absent cursor begins a traversal and captures a high water mark inside the same read transaction as the first page. Read ascending IDs greater than the last returned ID and no greater than that mark. Return an absent next cursor when exhausted, including for an empty collection. Each page's rows and summaries share one short read transaction; no transaction stays open between calls.

The typed cursor includes a format version, collection name, parent filter if any, last returned key, and high water key. It also carries `as_of` and `max_age` for candidate summaries. Reject mismatched collections, filters, times, and invalid bounds. Cursors are process portable within the same state database, contain no key values, and are not an authentication mechanism. Applying a cursor to a different database is outside this internal API contract.

`ListCandidates`, `ListRuns`, `ListSightings`, and `ListAttempts` use their record IDs. `ListObservations` requires a crawl run ID and uses ascending sighting IDs within that run. Its mark is the maximum linked sighting ID at the start, not the global sighting maximum. Observations are inserted only while the run is active; if callers need fixed observation membership, they read a terminal run. For an active run, a newly linked older sighting could otherwise appear within an existing bound. Return `ErrRunActive` for observation traversal until that run is terminal. This keeps the promised fixed collection contract without introducing another observation ID.

Records already in a traversal may gain provenance or evidence between pages. This is deliberately not a frozen export snapshot. A consistent snapshot export, advanced filters, and presentation belong to the results feature.

### API surface

All operations except `Close` accept `context.Context`. Reader methods are available on both handles. Mutations are exposed only on the writer handle. Inputs use domain records; SQL rows and driver errors do not escape the boundary. The table defines the callable contract rather than prescribing every Go struct name.

| Operation | Key inputs | Output | Access | Important errors |
| --- | --- | --- | --- | --- |
| `OpenWriter` | absolute `stateDir string`, `clock func() time.Time` | writer after migration and recovery | Current local account; acquires lock | `ErrBusy`, `ErrStateAccess`, `ErrSchemaTooNew` |
| `OpenReader` | absolute `stateDir string` | reader for current schema | Current local account | `ErrNotInitialized`, `ErrMigrationRequired`, `ErrSchemaTooNew` |
| `StartRun` | request token, run kind, start time | run record | Writer | `ErrInvalidInput`, `ErrConflict` |
| `RecordMessage` | crawl run ID, source and message IDs, observation metadata, keys | IDs and new record counts | Writer | `ErrRunClosed`, `ErrInvalidInput`, `ErrNotFound` |
| `BeginAttempt` | request token, validation run ID, candidate ID, backend ID and version, start time | attempt record | Writer | `ErrAttemptActive`, `ErrRunClosed`, `ErrConflict` |
| `CompleteAttempt` | attempt ID, outcome, finish time, reason, typed evidence | terminal attempt record | Writer | `ErrInvalidInput`, `ErrConflict`, `ErrNotFound` |
| `FinishRun` | run ID, terminal status, finish time, optional reason | terminal run record | Writer | `ErrAttemptActive`, `ErrConflict`, `ErrNotFound` |
| `GetCandidate` | candidate ID, `as_of`, `max_age` | summary without embedded history arrays | Reader | `ErrNotFound`, `ErrInvalidInput` |
| `ListCandidates` | page size, optional cursor, `as_of`, `max_age` | summary page and cursor | Reader | `ErrInvalidCursor`, `ErrInvalidInput` |
| `ListRuns` | optional kind, page size, cursor | run page and cursor | Reader | `ErrInvalidCursor`, `ErrInvalidInput` |
| `ListSightings` | candidate ID, page size, cursor | provenance page and cursor | Reader | `ErrNotFound`, `ErrInvalidCursor` |
| `ListAttempts` | exactly one of candidate ID or validation run ID, page size, cursor | attempt page and cursor | Reader | `ErrNotFound`, `ErrInvalidCursor` |
| `ListObservations` | terminal crawl run ID, page size, cursor | sighting references with observed times, and cursor | Reader | `ErrRunActive`, `ErrNotFound`, `ErrInvalidCursor` |
| `Close` | handle | resource release result | Handle owner | Sanitized state I/O error |

Every operation may also return context cancellation or a sanitized state I/O error. Wrong parent kind is `ErrInvalidInput`. Unknown nonzero parent IDs are `ErrNotFound`; invalid ID values are `ErrInvalidInput`. Empty valid collections return an empty page without error. Failed and cancelled runs require a reason code; a completed run has no failure reason. Closing twice is safe. Operations after close return `ErrClosed`.

`ListRuns` returns the stored run fields and the derived clock anomaly flag, without aggregate totals. Run totals and their presentation belong to the later results or diagnostics feature. This does not change the explicitly defined per message insert counts returned by `RecordMessage`.

Expose stable errors compatible with `errors.Is`; preserve cancellation with wrapped context errors. Use a sanitized wrapper for driver failures whose public message omits SQL parameters and raw provider output. No retries occur inside the store except SQLite's bounded contention wait. After an uncertain write error the caller retries the same operation identity rather than creating a replacement run or attempt.

### Ownership, migrations, and security

The caller supplies an absolute state directory. Default path and profile flag resolution remain application responsibilities under spec 0001. There are no new environment variables, credentials, config files, or CLI flags in this slice.

Canonicalize the existing state directory location so path aliases address the same lock. Use `<state-dir>/writer.lock` with a nonblocking exclusive advisory lock held by an open descriptor for the full writer handle lifetime. Never remove the lock file during release. Only contention maps to `ErrBusy`; filesystem and other lock failures map to state access errors. A stale filename is harmless. No network work occurs in the store.

Use a dedicated database handle with one connection for serialized writes. Give read handles their own connection, allowing at most one active read transaction per handle; callers may open several readers. Do not share the write connection with a long lived read cursor. On close, stop accepting calls, wait for entered operations to release resources, close the database, then release the lock. Callers cancel operation contexts before close if they need a bounded shutdown.

If opening, migration, or recovery fails, close every acquired database resource and release the lock before returning an error. Roll back any active transaction first. Do not remove created state files as a cleanup shortcut; a later writer can retry initialization of a valid empty database.

New state directories are mode `0700`; the database and lock file are `0600`. SQLite sidecars must remain `0600` as well, verified on both platforms. Check ownership by the current effective user and reject any group or other access bits on the managed state directory, database, lock, and existing sidecars. Reject symlinked managed database, lock, or sidecar files rather than following them. Do not tighten permissions automatically or inspect and change unrelated ancestor directories. Permission failures identify the path and required mode without exposing its contents.

Open readers with SQLite `mode=ro`, without creation, migrations, recovery, or journal mode changes. Never use SQLite `immutable` mode against a live WAL database. Readers can participate in SQLite shared memory coordination but cannot change application records or create a database or directory. If the existing WAL cannot be read with available filesystem permissions, report state access failure instead of changing permissions or falling back to stale data.

After the writer lock is held, open `<state-dir>/state.db`, enable foreign keys for every connection, select WAL mode, and use `synchronous=FULL` for writes. Configure a five second SQLite busy timeout per connection and honor earlier context cancellation. Verify the actual driver cancellation behavior during development; a failed check requires fixing the connection setup, not weakening the timeout contract. Existing nonempty files that are not supported databases cause an error. Do not delete or recreate damaged state.

Use one initial schema migration for these five tables and constraints. Each future migration is numbered and embedded with the executable. The writer checks `user_version` before changing schema. A version zero database is eligible for initialization when SQLite can read it and its `sqlite_schema` contains no application tables, indexes, views, or triggers. SQLite internal objects whose names use the reserved `sqlite_` prefix do not count as application objects. This includes an empty database left by failed or interrupted initialization, even if its file contains a SQLite header or WAL setup has occurred. A newly created or existing zero length file may initialize through SQLite under the same rule. File length and whether this invocation created the file do not determine eligibility. An unreadable database or a version zero database with any application schema object is rejected without repair or replacement.

A version above the executable's supported version returns `ErrSchemaTooNew` before migration or recovery. Apply all pending SQL and the final version change inside one transaction. A failure rolls back schema and records. Journal setup is connection preparation and is not claimed to roll back with a failed schema migration.

Readers require the exact current schema version. An absent database returns `ErrNotInitialized`; an older supported database returns `ErrMigrationRequired`. Readers do not initialize it as a convenience. WAL durability protects completed writes within the guarantees of the local filesystem and storage hardware, not against disk destruction. No backup, restore, downgrade, or automatic corruption repair is introduced.

Keys remain recoverable in the database and authorized read results, as required by the accepted local export architecture. Filesystem access is the protection; there is no database encryption or remote identity system. Logs and errors use record IDs and fixed reason codes, never full or partial key values. The store emits no routine logs; application code owns diagnostics. Evidence uses a closed structure, URLs reject embedded credential fields, and free text message content and validator output are never accepted. This boundary trusts callers running as the same operating system account; it is not a sandbox against malicious code under that account.

### Value sourcing

| Action | Produced value | Source |
| --- | --- | --- |
| Open | State path, lock result, schema version | Explicit caller path, operating system lock result, database `user_version` |
| Create | Run and attempt identities | SQLite generated IDs; retry identity comes from caller request tokens |
| Discover | Key identity and provenance | Normalized candidate input and source adapter supplied source ID, message ID, optional URL and publication time |
| Discover | First and latest discovery | Minimum and maximum persisted observation times supplied by the application |
| Discover | New record counts | Actual insert results in the current message transaction |
| Complete attempt | Entitlement, reason, evidence time and backend | Typed caller result and the attempt's backend fields; a real adapter requires the separate feasibility gate |
| Summarize | Latest attempt, active attempt, last definitive result | Attempt rows for the candidate ordered by immutable ID; outcomes and nullability select each category |
| Summarize | Freshness and clock anomaly | Last definitive evidence time compared with explicit `as_of` and `max_age`; event time comparisons expose negative durations |
| Recover | Interrupted status and recovery time | Unfinished persisted rows and one timestamp from the clock supplied to `OpenWriter` |
| Page | Membership and continuation | First page query's high water mark, last returned ordered key, collection filter, and cursor format version |
| Inspect runs | Run history and linked records | Retained runs, attempts, observations, and their foreign key relationships |

### Critical verification scenarios

1. Use a temporary store and synthetic keys in two separate processes to save and read a candidate and its source message. Check help and version still leave state untouched. Covers AC-1 and AC-3.
2. Replay one message, rediscover it in another run, discover the same key in another channel, and discover several keys in one message. Include case variants and surrounding whitespace. Confirm identities, observation counts, and all provenance. Covers AC-2 and AC-12.
3. Record confirmation, a later inconclusive attempt, and a later definitive rejection. Query before and after each. Evaluate just before expiry, exactly at expiry, and with future evidence and a backward clock. Covers AC-3, AC-4, and AC-5.
4. Interrupt a message transaction before commit using controlled process synchronization, then reopen. Confirm no partial candidates or links and preservation of an earlier committed message. Interrupt after commit before acknowledging success and retry the same write. Covers AC-6 and AC-12.
5. Hold a writer in one process, open another writer and a reader, and distinguish busy from permission failure. Kill the writer with unresolved work and reopen twice. Completed results survive, unfinished records recover once, and actual finish time remains unknown. Covers AC-7 and AC-8.
6. Traverse 338 records in several pages while a writer adds candidates and updates existing evidence. The initial collection appears once each, new IDs appear only on the next traversal, and existing details may change between pages. Page every history surface as well. Covers AC-9.
7. Exercise permissive files, forbidden symlinks, absent reader state, unknown schema versions, and a deliberately failing migration using a controlled test migration source. Confirm errors, no automatic permission changes, no replacement database, rollback, and absence of key values in diagnostics. Covers AC-10 and AC-11.
8. Repeat start, begin, and finish calls with matching and conflicting tokens or payloads. Reject two unresolved attempts for one candidate while allowing different candidates. Check cancellation and the busy timeout with real SQLite contention. Covers AC-7 and AC-12.
9. Fail or terminate initialization after WAL setup and before the first schema commit, then reopen with a new writer. A readable version zero database without application schema objects initializes successfully. An existing version zero database with an application table, index, view, or trigger is rejected. Covers AC-11.

## Build plan

The project uses Tracer Bullet delivery and a Beta workflow. The first thread crosses initialization, transaction, saved provenance, close, and read in another process. It does not claim to complete the separate crawl and validation loop.

1. Build the first executable store path: add the domain records and SQLite dependency, private state initialization, writer lock, initial migration, start a crawl run, save one message, close, and read its summary through `GetCandidate` and provenance through `ListSightings` in a second process. Keep CLI construction unchanged. Satisfies AC-1, AC-2, AC-3, AC-6, AC-7, AC-10, and AC-11.
2. Extend the working path with repeatable discovery, all provenance and observation reads, request token handling, bounded pages, and cursor membership rules. Demonstrate the 338 candidate traversal. Satisfies AC-2, AC-9, and AC-12.
3. Add attempt creation and completion using controlled evidence fixtures, run completion, derived summaries, freshness, and every attempt history read. Satisfies AC-3, AC-4, AC-5, and AC-12.
4. Complete interruption recovery, error mapping, migration rollback checks, permission refusal, contention bounds, and simultaneous reader verification on macOS and Linux arm64. Satisfies AC-6, AC-7, AC-8, AC-10, AC-11, and AC-12.
5. Run `/check verify persistent key state` through the public store interface, using the scenarios above and subprocesses for locks and crashes. Then use `/test persistent key state` to retain meaningful regression coverage under the existing test preferences. Run the repository formatting, vet, race, and lint gate. Satisfies AC-1 through AC-12.

## Consequences

You retain traceable evidence without letting rediscovery or temporary failure rewrite entitlement history. Short transactions and separate reader handles fit the existing writer ownership model.

History grows without an automatic retention bound. Derived summaries cost queries, and observing old messages in new runs adds observation rows. Bounded pages control response memory but do not freeze details across an entire traversal. These are accepted costs until measured use warrants another decision.

This feature can ship before validation feasibility because its evidence fixtures demonstrate storage behavior only. It cannot advertise confirmed real keys, schedule checks, or recover remote WARP resources.

## Follow-up

1. Complete: the design is confirmed and linked to scope item 3. The independent completeness review and its approved fixes are recorded in the rationale.
2. Record the exact SQLite driver version and prove runtime behavior on both arm64 platforms during development. No additional skill or MCP discovery is requested.
3. The validation feasibility design must provide attributable entitlement evidence, normalized rejection codes, and durable remote cleanup recovery before any real adapter is enabled. It may extend evidence storage through a new migration if the proven backend needs more fields.
4. Later feature specs own source ID normalization, configuration resolution, CLI commands, consistent snapshot export, scheduling eligibility, and bounded application shutdown. They reuse this store contract rather than making its timestamps or outcomes mean something different.

## Rationale

Reasoning and options are in [rationale.md](rationale.md).
