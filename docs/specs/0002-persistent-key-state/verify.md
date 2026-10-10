# Verify: persistent key state · spec 0002 · updated 2026-10-10

Steps derived from spec 0002 acceptance criteria and value sources. You can run `/check verify persistent key state` against this checklist, then use `/test persistent key state` to assess and extend the retained regression coverage.

## Build evidence

The developer checks passed with Go 1.27.2 and SQLite driver v1.60.1 on macOS arm64 and in an Alpine Linux arm64 container. The macOS repository gate passed formatting, vet, race tests, and lint. Both CLI target builds passed. These observations do not mark the separate Beta verification or test workflow complete.

## Public interface and failure scenarios

1. [x] Open a writer using an absolute private directory. Inspect the live schema version and five application tables. Open a competing writer and a simultaneous reader. Expect immediate `ErrBusy` only for writer contention, and committed reads through the reader. Confirm new database, lock, WAL, and shared memory files have no group or other access. Covers AC-7, AC-10, AC-11.
2. [x] Replay run and attempt creation with the original token and normalized inputs. Expect the same ID and current status, including after completion. Change an input with that token and expect `ErrConflict`. Replay matching completions successfully and reject conflicting terminal results. Covers AC-12.
3. [x] Discover keys with surrounding whitespace, case variants, and internal spaces. Repeat a message, observe it in a second run, and discover it in another source. Expect exact normalized identity, distinct provenance, and one observation per run and sighting. Covers AC-2, AC-12.
4. [x] Supply different observation times, including backward clocks. Expect minimum and maximum discovery times from saved observations. Omit publication time and expect null. Fill absent metadata, then attempt to replace known metadata. Expect the first known value to remain. Covers AC-3.
5. [x] Supply typed confirmation, inconclusive retry, and definitive rejection fixtures. Inspect the stored backend, version, reason, and evidence time. Reject unsupported account types, inconsistent evidence shapes, and free text reason codes. Covers AC-4, AC-10.
6. [x] Complete attempts whose wall clocks run backward. Expect latest and definitive summaries selected by attempt ID. Inspect an unresolved attempt separately. Expect a later inconclusive outcome to preserve the prior definitive result. Covers AC-3, AC-4.
7. [x] Hold an explicit evaluation time and interval. Evaluate just before expiry, exactly at expiry, and before confirmation evidence. Expect fresh, stale, and unknown with a clock anomaly respectively. Move the evaluation clock forward and expect ordinary freshness again. Covers AC-5.
8. [x] Kill a writer with unresolved work, then open another with a supplied recovery clock. Expect interrupted runs, inconclusive unresolved attempts, explicit recovery time, and absent actual finish time. Reopen with a later clock and expect no further recovery changes. Completed evidence stays intact. Covers AC-8.
9. [x] Traverse 338 candidates with bounded pages. Insert another candidate and update evidence for an existing candidate between pages. Expect each original candidate once, no later ID, and current details on later pages. Serialize a cursor between calls. Reject wrong collections, parents, evaluation times, and invalid bounds. Covers AC-9.
10. [x] Page run history, candidate sightings, observations, and attempts by both candidate and validation run. Expect retained records beyond the default page size and no total cap. Reject observation traversal for an active run. Covers AC-9, AC-12.
11. [x] Save a discovery, close the process, and read its summary and provenance in another process. Run CLI help and version in an isolated environment and expect no state or configuration creation. Covers AC-1.
12. [x] Interrupt a message before commit and expect no partial candidates or links, with earlier committed data still readable. Interrupt after commit and replay successfully with zero insert counts. Covers AC-6, AC-12.
13. [x] Fail a migration after its first schema change and expect the prior schema and data. Interrupt initial migration after WAL setup and expect an empty version zero database to initialize on retry. Reject version zero with application objects, newer versions, and damaged state without replacement. Covers AC-11.
14. [x] Try permissive managed files and symlinked database, lock, and sidecar paths. Expect clear refusal without changing permissions. Open absent state as a reader and expect no created directory. Trigger a driver error containing synthetic secret text and confirm the public error omits it. Covers AC-10, AC-11.
15. [x] Hold a real SQLite write transaction from another connection. Expect a five second bounded native wait, prompt earlier context cancellation, no later commit of cancelled work, and writer ownership retained until cleanup. Covers AC-7, AC-12.

## Commands

You can run all retained build checks with:

```sh
rtk proxy go test ./internal/store -v
rtk proxy go test ./cmd/wpc ./internal/cli
rtk proxy pre-commit run --all-files
rtk proxy go build -o bin/wpc ./cmd/wpc
rtk proxy env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bin/wpc-linux-arm64 ./cmd/wpc
```

For Linux runtime checks, you can compile the same store suite, then mount the resulting binary into a disposable Linux arm64 container with no network. The host path needs to be shared with your container engine.

```sh
rtk proxy env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o bin/store-linux-arm64.test ./internal/store
rtk proxy docker run --rm --network none --platform linux/arm64 --mount type=bind,source=/Users/nguyenducloc/warp-plus-connector/bin/store-linux-arm64.test,target=/checks/store.test,readonly alpine:3 /checks/store.test -test.v
```

## Value source coverage

| Spec value source row | Steps |
| --- | --- |
| Open: explicit path, operating system lock, schema version | 1, 13, 14 |
| Create: generated IDs and caller retry tokens | 2 |
| Discover: normalized keys and supplied provenance | 3 |
| Discover: minimum and maximum observation times | 4 |
| Discover: actual transaction insert counts | 3, 12 |
| Complete attempt: typed result and stored backend | 5 |
| Summarize: persisted attempts in ID order | 6 |
| Summarize: explicit evaluation, interval, and evidence time | 7 |
| Recover: unfinished records and supplied clock | 8 |
| Page: initial bound, last ID, filter, and cursor version | 9 |
| Inspect runs: retained records and foreign key links | 10 |

## Acceptance criteria coverage

| Criterion | Steps |
| --- | --- |
| AC-1 | 11 |
| AC-2 | 3 |
| AC-3 | 4, 6 |
| AC-4 | 5, 6 |
| AC-5 | 7 |
| AC-6 | 12 |
| AC-7 | 1, 15 |
| AC-8 | 8 |
| AC-9 | 9, 10 |
| AC-10 | 1, 5, 14 |
| AC-11 | 1, 13, 14 |
| AC-12 | 2, 3, 10, 12, 15 |
