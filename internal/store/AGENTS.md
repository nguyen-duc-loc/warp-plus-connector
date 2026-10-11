# Persistent state store

The governing contract is [spec 0002](../../docs/specs/0002-persistent-key-state/index.md). [Usage notes](README.md) describe the public interface. Domain inputs and results live in [state.go](../state/state.go), without SQL or driver types. This package stores controlled evidence; it does not validate real WARP entitlement.

## Ownership and storage

[open.go](open.go) owns private paths, writer locking, connection setup, recovery, and close. One writer holds `writer.lock` until entered operations finish and database connections close. The lock file remains on disk. A writer has separate read and write connections; each handle serializes access to its own connection. Readers use `mode=ro` and do not initialize, migrate, or recover state.

New directories use `0700`; managed files use `0600`. Existing broad permissions, wrong ownership, and symlinked managed files cause errors without repair. Directory aliases resolve to the same writer lock. Connections enforce foreign keys and WAL supports concurrent readers; writes use `synchronous=FULL`.

[migrate.go](migrate.go) applies embedded SQL and the schema version in one transaction. [001_initial.sql](migrations/001_initial.sql) defines the five history tables; [002_crawl_coverage.sql](migrations/002_crawl_coverage.sql) adds requested and parsed crawl coverage. Version zero initializes only when no application schema objects exist. Readers require the current version; newer or damaged state is rejected without replacement.

Schema version 3 adds [003_crawl_plan.sql](migrations/003_crawl_plan.sql), preserving old rows without backfill. `crawl_sources`, `crawl_pages`, and `crawl_page_observations` implement [spec 0004](../../docs/specs/0004-configurable-source-coverage/index.md). A failed migration rolls back its tables and version together; an older binary rejects the newer schema.

Schema version 4 adds [004_validation_batches.sql](migrations/004_validation_batches.sql) for [spec 0005](../../docs/specs/0005-concurrent-checks-bounded-retries/index.md), preserving earlier history. `validation_batches`, `validation_items`, and `validation_attempt_details` retain immutable selection, a typed policy snapshot, and separate scheduling facts for every attempt.

Schema version 5 adds [005_schedules.sql](migrations/005_schedules.sql) for [spec 0007](../../docs/specs/0007-scheduled-crawl-revalidation/index.md). `scheduler_state` retains coverage, the clock checkpoint, validation waits, and recovery obligations. `candidate_schedule` retains due times and their bases. Runs record manual or scheduled origin, and completion records retain immutable scheduling policy for replay.

## Saved records and retries

[discovery.go](discovery.go) saves one message atomically. Identity trims surrounding key whitespace and preserves the remaining bytes. Sightings retain their first known publication time and URL; each run and sighting pair retains its first observation time. Discovery times are derived from saved observations.

[crawl.go](crawl.go) creates crawl intent and coverage atomically, records parsed facts once, and reads active or terminal reports with counts from one transaction. Legacy runs have unavailable coverage. Local run recovery does not reconcile Docker registrations; the validator owns its separate remote session journal.

[plan.go](plan.go) saves the complete selected source snapshot before fetching, derives each request from saved coverage, fills parsed facts once, and links message observations to pages in the same transaction as discovery. A message requires a matching active parsed page and its fetched timestamp. Overlapping pages preserve the first observation time and page link. Legacy message and finish APIs reject plan runs.

[plan_finish.go](plan_finish.go) validates page, source, and parent completion. Identical request, parse, and terminal replays succeed even after closure; conflicting inputs fail. Source completion requires a terminal page and a truthful `preview_end` or `page_limit` boundary. Parent completion requires every selected source to complete. An abort updates unfinished children and the parent in one transaction.

[runs.go](runs.go) and [attempts.go](attempts.go) use caller request tokens for repeatable creation. Matching normalized inputs return the current saved record; conflicting inputs or terminal results return `ErrConflict`. One unresolved attempt per candidate is permitted across all runs. A validation run finishes only after all its attempts resolve.

[batch.go](batch.go) saves batch intent atomically and enforces sequential attempt numbers, matching backend identity, saved retry plans, and the attempt ceiling. Completion atomically saves an attempt plus its retry plan or terminal item. Legacy mutation APIs reject batch runs. Recovery marks pending and active items interrupted, preserves completed attempts and retry plans, and never resumes unused attempts.

Times normalize to UTC microseconds. Recovery marks unfinished runs interrupted and unresolved attempts inconclusive, with explicit recovery time and no invented finish time. Earlier completed evidence remains intact.

Plan recovery marks active pages and sources `interrupted`, skips pending sources with `interrupted_before_start`, and preserves terminal children. Recovered records have `recovered_at` and no invented `finished_at`. Request intent does not prove that an HTTP request reached Telegram; later crawls create new plans rather than resuming cursors.

[schedule.go](schedule.go) updates eligibility in the same transaction as discovery, terminal completion, or recovery. `OpenWriterWithPolicy` supplies recovery intervals; `PrepareSchedule` recomputes deadlines from saved bases without rewriting evidence or postponing work on restart. Manual crawls advance only matching ordered coverage. Pending items without attempts retain eligibility; started interrupted work uses recovery time and the failure interval. Due times round upward to UTC microseconds, and clock checkpoints never decrease.

## Reads and evidence

[reads.go](reads.go) separates the latest completed attempt, last definitive result, and unresolved attempt. Attempt ID determines recency. Freshness uses the last definitive confirmation, explicit `AsOf`, and positive `MaxAge`; inconclusive retries do not refresh evidence.

Pages default to 100 rows and allow up to 1000 per call, with no total cap. The first page fixes the highest included ID; later pages read current details. Cursors retain collection, parent, filter, and evaluation. Observation traversal requires a terminal crawl run.

[snapshot.go](snapshot.go) adds `VisitCandidates` for [spec 0006](../../docs/specs/0006-complete-results-export/index.md), with no schema change. It reads ascending IDs in pages of 100 and derives every summary within one read transaction and explicit evaluation. Later writer commits cannot alter that view. Returning false stops normally, callback errors retain their identity, and callbacks must not reenter the reader. Traversal stays synchronous through cancellation so entered callbacks finish before caller resources can be reused. Existing paged APIs retain their separate transaction behavior.

[plan_reads.go](plan_reads.go) reads every saved source and page in configuration and page order. `GetCrawlReport` returns saved plans independently of current settings, while preserving both earlier report formats. Candidate totals use distinct IDs at each report level; page totals cannot be summed to infer distinct run candidates. Parsed post counts include overlapping occurrences and posts without keys.

[batch_reads.go](batch_reads.go) derives report totals from saved rows and pages through items in selection order and attempts in attempt number order. Cursors bind the batch and candidate. Legacy runs have a nil policy; they retain their original history without invented selection metadata. An active report header is a point in time, while later pages may reflect newer commits.

[values.go](values.go) validates typed evidence and normalized reason codes. [errors.go](errors.go) preserves stable errors and cancellation for `errors.Is`, while excluding raw driver text from both messages and unwrap chains. The store emits no routine logs and never accepts message bodies or raw validator output.

[schedule_reads.go](schedule_reads.go) establishes its SQLite snapshot before sampling the supplied clock. `GetSchedule` derives counts and references from that snapshot without applying current settings. `DueCandidates` excludes active items and unresolved attempts, orders by due time and candidate ID, and accepts a selection limit from one through eight.

## Cancellation

[context.go](context.go) lets callers observe cancellation while native SQLite work finishes cleanup. The five second busy timeout is shortened for earlier deadlines. Entered work retains resources and writer ownership until cleanup completes. Inputs captured by that work are copied before dispatch. `Close` waits for entered operations; callers cancel their contexts first when they need bounded shutdown.

## Verification

You can run `rtk proxy go test ./internal/store` from the repository root for the store suite, or `rtk proxy pre-commit run --all-files` for formatting, vet, race tests, and lint. External package tests exercise the public interface; internal fixtures inject SQL failures, migrations, contention, and synchronized process crashes. Test comments trace assertions to acceptance criteria.

[verify.md](../../docs/specs/0002-persistent-key-state/verify.md) records runtime checks and the Linux arm64 container command. Real entitlement and remote registration cleanup remain outside these tests.

_Drafted by /sync from the introducing change, worth a quick human pass._
