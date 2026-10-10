# Scope: Warp Plus Connector

A personal CLI that collects publicly shared WARP Plus key candidates, checks their entitlement, and keeps results fresh while running in the background. You can inspect and export the complete collection without a fixed 100 key cap.

**Build approach:** Tracer Bullet (prove one real crawl, check, save, and list path, then expand it).
**Workflow:** Beta (after development, verify behavior with `/check verify`, then add meaningful coverage with `/test`).

You selected this approach and workflow. Each feature starts with its first suggested command; you can adjust the sequence as the project develops.

## Boundaries and decisions

Your fixed requirements are Go, Cobra, concurrent key checks, retries, scheduled crawling, scheduled revalidation, background operation, and access to every collected result. The installed `warp-cli` is available for testing. Its presence alone does not establish a validation backend.

Your selected boundary is personal use on macOS and Linux, with configurable public Telegram channels and no Telegram login. The first version targets arm64 only on both platforms; amd64 is deferred by your decision on 2026-10-10. The CLI lists and exports checked keys; you apply a key manually outside the tool. Crawling defaults to hourly and rechecks to every six hours, both configurable. Validation uses isolated state and preserves your current WARP connection. No delivery deadline or numerical throughput target was provided.

The first architecture decision is how to confirm actual WARP Plus entitlement with isolated validation state. A format match, source label, or ordinary WARP connection does not establish Plus entitlement. A successful check describes evidence at a particular time, not a promise that another device can activate the key later.

A shared WARP registration cannot serve as independent state for concurrent license changes. Parallel validation needs isolated contexts or another verified method. Feasibility, resource limits, registration effects, and cleanup remain decisions for architecture. If a usable method cannot be established, validation stays explicitly unsupported and the core loop is not complete.

Success means every distinct candidate within the configured crawl coverage is retained and reachable, confirmed keys carry fresh evidence, temporary failures do not become false invalid results, and scheduled work survives ordinary shutdown and restart. A collection containing 338 distinct records must expose all 338 through listing or export; this is a completeness check, not a promised live yield.

## At a glance

| # | Feature | Phase | Status |
|---|---------|-------|--------|
| 1 | Stack and architecture | Foundation | done |
| 2 | Coding standards and tooling | Foundation | done |
| 3 | Persistent key state | Foundation | done |
| 4 | One source crawl and validation loop | Slice 1 | done |
| 5 | Configurable source coverage | Slice 2 | in-progress |
| 6 | Concurrent checks and bounded retries | Slice 3 | planned |
| 7 | Complete results and export | Slice 4 | planned |
| 8 | Scheduled crawl and revalidation | Slice 5 | planned |
| 9 | Background operation and diagnostics | Slice 6 | planned |

## Foundations

### 1. Stack and architecture · done

Record the remaining architecture around your Go and Cobra choices, then create the smallest runnable CLI. Resolve validation feasibility before committing to a concurrency model.

**Done when:** the architecture records supported platforms, validator boundaries and evidence, state isolation, configuration boundaries, and persistence responsibilities; the scaffold builds and exposes working help and version output.

**Spec:** [0001. Stack and architecture](../specs/0001-stack-architecture/index.md), confirmed on 2026-10-10. The scaffold can proceed; real validation remains unsupported until the recorded feasibility gate passes.

**Code:** [executable entry point](../../cmd/wpc/main.go) and [CLI commands](../../internal/cli/root.go).

- [x] Decide the architecture (spec): `/architect stack and architecture`
- [x] Scaffold from the decision: `/develop stack and architecture`
- [x] Verify it: `/check verify stack and architecture`
- [x] Test it: `/test stack and architecture`

Verified help, version metadata, errors, and filesystem behavior on macOS and Linux arm64. Linux syscall tracing confirmed no network calls or configuration and state access for help and version; macOS checks also passed with network access denied. All 16 automated test cases passed with Go testing and Testify. The remaining macOS amd64 runtime check is outside the first version scope after your platform decision.

### 2. Coding standards and tooling · done

Capture conventions from the real scaffold so later work follows consistent build, error handling, cancellation, and testing practices.

**Done when:** project instructions describe the actual scaffold and the agreed formatting, static checks, test commands, and concurrency checks run successfully.

**Code:** [commit hooks](../../.pre-commit-config.yaml), [lint policy](../../.golangci.yml), and [formatting check](../../scripts/check-format.sh).

- [x] Capture conventions and tooling: `/audit`
- [x] Verify it: `/check verify tooling`
- [x] Test it: `/test tooling`

Six automated formatting cases cover clean and empty source trees, formatting failures in both source directories, nested paths with spaces, multiple affected files, syntax errors, and preservation of source bytes. All 22 cases across the module passed, including race tests. Formatting, vet, lint, and both configuration validators passed on macOS arm64. Installed hook behavior remains covered by the tooling verification checklist rather than the Go suite.

### 3. Persistent key state · done

Keep candidate identity, source provenance, validation evidence, and run history across restarts. Keep source visibility separate from entitlement.

**Done when:** repeated discoveries merge without losing provenance; discovery and check timestamps are distinct; unchecked, confirmed, invalid, and inconclusive outcomes are distinguishable; stale confirmations are visible; interrupted writes preserve usable state.

**Spec:** [0002. Persistent key state](../specs/0002-persistent-key-state/index.md), confirmed on 2026-10-10 after independent review. This slice builds the store interface; CLI commands and real validation remain in later features.

- [x] Design it (spec): `/architect persistent key state`
- [x] Build it: `/develop persistent key state`
  - [x] Prove private initialization, atomic discovery, and summary and provenance reads across a process restart (AC-1, AC-2, AC-3, AC-6, AC-7, AC-10, AC-11).
  - [x] Complete repeatable provenance writes, request identities, and bounded traversal of all 338 controlled candidates (AC-2, AC-9, AC-12).
  - [x] Retain validation attempts and derive definitive evidence and freshness using controlled fixtures (AC-3, AC-4, AC-5, AC-12).
  - [x] Verify recovery, migration rollback, permissions, and concurrent access on both arm64 platforms (AC-6, AC-7, AC-8, AC-10, AC-11, AC-12).
- [x] Verify it: `/check verify persistent key state`
- [x] Test it: `/test persistent key state`

**Code:** Domain records in `internal/state/`; persistence and build checks in `internal/store/`. The migration and runtime checks passed on macOS and Linux arm64 with SQLite driver `v1.60.1`. The repository gate passed. Suggested verification is in [verify.md](../specs/0002-persistent-key-state/verify.md).

The test pass added eight test functions covering 42 cases for evidence rejection, normalized retries, terminal results, immutable provenance, cursor validation, and writer ownership. It also extended handle lifecycle assertions and linked the retained store tests to AC-1 through AC-12. The full Go suite, formatting, vet, race tests, and lint passed on macOS arm64. Vet needed access to the Go build cache outside the sandbox. You confirmed feature completion on 2026-10-10.

## Slice 1: Prove the whole loop

### 4. One source crawl and validation loop · done

Run a manual crawl of one public source, check a candidate through the verified validator, save the outcome, and read it back through the CLI.

**Done when:** a real source produces a candidate that is checked, persisted, and listed after restart; Plus confirmation has attributable evidence; source errors, validator errors, empty results, and unsupported validation are explicit; the selected isolation boundary is preserved.

**Spec:** [0003. One source crawl and validation loop](../specs/0003-source-crawl-validation/index.md), confirmed on 2026-10-10 after independent review. The [Docker backend child](../specs/0003-source-crawl-validation/0003-docker-validator.md) records the completed runtime and recovery design. You selected automatic cleanup reconciliation before a new check.

**Verification:** Passed on 2026-10-11. A real sourced candidate returned attributable Unlimited evidence through the final CLI and remained confirmed after restart. Live interruption and lost acknowledgement checks preserved recovery ownership and private evidence. Controlled provider responses, both arm64 persistence paths, and host isolation passed. Your working registration remained unchanged. The evidence record is in the feature verification checklist.

- [x] Design it (spec): `/architect one source crawl and validation loop`
- [x] Build it: `/develop one source crawl and validation loop`
  - [x] Assess and provision the controlled Docker experiment, recording unavailable prerequisites without enabling real validation (AC-7).
  - [x] Prove the one page crawl, coverage migration, persistence, complete text listing, and read after restart, including failure and privacy behavior (AC-1, AC-2, AC-3, AC-5, AC-6, AC-9).
  - [x] Add selected candidate checks through the unsupported adapter and retain truthful attempt history (AC-3, AC-4, AC-5, AC-6).
  - [x] Pass the experiment, complete the backend design through `/architect`, and prove the real source, check, save, and restart path (AC-7, AC-8).
- [x] Verify it: `/check verify one source crawl and validation loop`
- [x] Test it: `/test one source crawl and validation loop`

The test pass on 2026-10-11 added 28 test functions across 15 files. Coverage includes source parsing and request limits, private configuration, durable crawl intent, partial failure, schema migration, complete CLI listing, unsupported checks, validator identity and cleanup, provider process responses, and image build failures. All 273 tests and subtests passed with the race detector on macOS arm64. Formatting, vet, and lint passed. All eight internal package suites also passed on Linux arm64, with 246 tests and subtests, in a container with networking disabled. Live entitlement and host isolation remain covered by the separate verification record. You confirmed feature completion on 2026-10-11.

**Code:** `cmd/wpc`, `cmd/wpc-worker`, `internal/cli`, `internal/config`, `internal/app`, `internal/source/telegram`, `internal/validator`, `internal/state`, and `internal/store`. The [validator notes](../../internal/validator/README.md) describe setup and recovery. The [verification checklist](../specs/0003-source-crawl-validation/verify.md) retains the separate acceptance pass. Real Free, capacity, provider error, and creation throttling paths were exercised; controlled responses cover confirmation persistence.

## Slice 2: Expand source coverage

### 5. Configurable source coverage · in-progress

Let you configure multiple public channels, collect directly from them, and see which sources succeeded. Initial seed candidates are `warpplus`, `warppluscn`, `warpPlusHome`, and `warp_veyke`.

**Done when:** enabled sources contribute deduplicated candidates with provenance; the configured message or page coverage and any truncation are visible; repeated crawls are idempotent; one source failure does not discard other results or change a key's entitlement status.

**Spec:** [0004. Configurable source coverage](../specs/0004-configurable-source-coverage/index.md), confirmed on 2026-10-11 after independent review. All four seeds are enabled by default. Coverage uses a shared page limit with per channel overrides, sequential requests, and one saved run with source and page outcomes. The spec remains Proposed until implementation begins.

- [x] Design it (spec): `/architect configurable source coverage`
- [ ] Build it: `/develop configurable source coverage`
  - [ ] Prove one configured channel through the migration, saved plan, one page crawl, and report after restart while preserving old history (AC-2, AC-4, AC-6, AC-10).
  - [ ] Add the four defaults, custom source selection, source inspection, limit precedence, and independent source outcomes (AC-1, AC-2, AC-3, AC-4, AC-7).
  - [ ] Prove bounded older page traversal, page records, overlap handling, request pacing, and visible coverage limits (AC-4, AC-5, AC-6, AC-9).
  - [ ] Complete rate limit abort, cancellation, mutation error recovery, output errors, and privacy behavior (AC-2, AC-3, AC-7, AC-8, AC-10).
- [ ] Verify it: `/check verify configurable source coverage`
- [ ] Test it: `/test configurable source coverage`

## Slice 3: Check reliably at scale

### 6. Concurrent checks and bounded retries · planned · needs a decision

Use Go concurrency to validate independent candidates with configurable worker and request limits. Retry transient failures with increasing delay and jitter, a small random variation that avoids synchronized retries.

**Done when:** multiple checks can run with isolated state and a measurable concurrency ceiling; each key has at most one active check; timeouts and cancellation bound work; transient failures retry within a finite budget and respect server delays; exhausted retries remain inconclusive; definitive rejection does not enter an immediate retry loop.

- [ ] Design it (spec): `/architect concurrent checks and bounded retries`

## Slice 4: Reach every result

### 7. Complete results and export · planned · needs a decision

Inspect outcomes and freshness, and export the full matching collection for your own use. Terminal presentation and export must make any user selected limits explicit.

**Done when:** all 338 records in a controlled dataset are reachable and exportable without duplicates or silent truncation; status and freshness filters behave consistently; text and structured output remain usable in scripts; diagnostics stay separate from exported data; empty results have clear semantics.

- [ ] Design it (spec): `/architect complete results and export`

## Slice 5: Keep results fresh

### 8. Scheduled crawl and revalidation · planned · needs a decision

Give crawling and validation separate configurable intervals. New candidates and previously checked keys both receive appropriate future checks.

**Done when:** both schedules run independently; slow jobs do not create overlapping checks or an unbounded backlog; startup and missed interval behavior are defined; failed work gets a future attempt; restarts preserve freshness information and pending eligibility; manual runs respect the same coordination rules.

- [ ] Design it (spec): `/architect scheduled crawl and revalidation`

## Slice 6: Operate in the background

### 9. Background operation and diagnostics · planned · needs a decision

Run unattended on the selected platforms and let you inspect progress, stop cleanly, and recover after failure.

**Done when:** you can start background operation, inspect status, and stop it; duplicate instances sharing state are prevented; shutdown saves completed work within a bounded time; restart recovers interrupted work; status reports last and next runs, queue depth, outcome counts, and source or validator failures; routine logs mask key values.

- [ ] Design it (spec): `/architect background operation and diagnostics`

## Deferred

Billing, a hosted service, and product analytics are unnecessary for the selected personal CLI. Automatic key application and rotation, private Telegram access, unlimited historical backfill, a public API or dashboard, notifications, and public release automation are outside this initial slice. They can be enrolled later if you need them.

## Legend

`planned` means work has been scoped. `in-progress` means design or implementation has started. `done` means the selected verification stages have completed. `existing` identifies work that predates this workflow, and `dropped` preserves a removed feature's history.

A `needs a decision` tag recommends architecture before implementation. The first unchecked box is the suggested next step. Architecture adds a spec link and a short build plan when the decision is recorded. Detailed implementation tasks stay in that spec.

All features inherit the project approach and Beta workflow. Future changes update this file in place.
