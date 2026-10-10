# Persistent state store

The governing contract is [spec 0002](../../docs/specs/0002-persistent-key-state/index.md). [Usage notes](README.md) describe the public interface. Domain inputs and results live in [state.go](../state/state.go), without SQL or driver types. This package stores controlled evidence; it does not validate real WARP entitlement.

## Ownership and storage

[open.go](open.go) owns private paths, writer locking, connection setup, recovery, and close. One writer holds `writer.lock` until entered operations finish and database connections close. The lock file remains on disk. A writer has separate read and write connections; each handle serializes access to its own connection. Readers use `mode=ro` and do not initialize, migrate, or recover state.

New directories use `0700`; managed files use `0600`. Existing broad permissions, wrong ownership, and symlinked managed files cause errors without repair. Directory aliases resolve to the same writer lock. Connections enforce foreign keys and WAL supports concurrent readers; writes use `synchronous=FULL`.

[migrate.go](migrate.go) applies embedded SQL and the schema version in one transaction. [001_initial.sql](migrations/001_initial.sql) defines the five history tables; [002_crawl_coverage.sql](migrations/002_crawl_coverage.sql) adds requested and parsed crawl coverage. Version zero initializes only when no application schema objects exist. Readers require the current version; newer or damaged state is rejected without replacement.

## Saved records and retries

[discovery.go](discovery.go) saves one message atomically. Identity trims surrounding key whitespace and preserves the remaining bytes. Sightings retain their first known publication time and URL; each run and sighting pair retains its first observation time. Discovery times are derived from saved observations.

[crawl.go](crawl.go) creates crawl intent and coverage atomically, records parsed facts once, and reads active or terminal reports with counts from one transaction. Legacy runs have unavailable coverage. Local run recovery does not reconcile Docker registrations; the validator owns its separate remote session journal.

[runs.go](runs.go) and [attempts.go](attempts.go) use caller request tokens for repeatable creation. Matching normalized inputs return the current saved record; conflicting inputs or terminal results return `ErrConflict`. One unresolved attempt per candidate is permitted across all runs. A validation run finishes only after all its attempts resolve.

Times normalize to UTC microseconds. Recovery marks unfinished runs interrupted and unresolved attempts inconclusive, with explicit recovery time and no invented finish time. Earlier completed evidence remains intact.

## Reads and evidence

[reads.go](reads.go) separates the latest completed attempt, last definitive result, and unresolved attempt. Attempt ID determines recency. Freshness uses the last definitive confirmation, explicit `AsOf`, and positive `MaxAge`; inconclusive retries do not refresh evidence.

Pages default to 100 rows and allow up to 1000 per call, with no total cap. The first page fixes the highest included ID; later pages read current details. Cursors retain collection, parent, filter, and evaluation. Observation traversal requires a terminal crawl run.

[values.go](values.go) validates typed evidence and normalized reason codes. [errors.go](errors.go) preserves stable errors and cancellation for `errors.Is`, while excluding raw driver text from both messages and unwrap chains. The store emits no routine logs and never accepts message bodies or raw validator output.

## Cancellation

[context.go](context.go) lets callers observe cancellation while native SQLite work finishes cleanup. The five second busy timeout is shortened for earlier deadlines. Entered work retains resources and writer ownership until cleanup completes. Inputs captured by that work are copied before dispatch. `Close` waits for entered operations; callers cancel their contexts first when they need bounded shutdown.

## Verification

You can run `rtk proxy go test ./internal/store` from the repository root for the store suite, or `rtk proxy pre-commit run --all-files` for formatting, vet, race tests, and lint. External package tests exercise the public interface; internal fixtures inject SQL failures, migrations, contention, and synchronized process crashes. Test comments trace assertions to acceptance criteria.

[verify.md](../../docs/specs/0002-persistent-key-state/verify.md) records runtime checks and the Linux arm64 container command. Real entitlement and remote registration cleanup remain outside these tests.

_Drafted by /sync from the introducing change, worth a quick human pass._
