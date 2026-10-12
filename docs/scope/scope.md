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
| 5 | Configurable source coverage | Slice 2 | done |
| 6 | Concurrent checks and bounded retries | Slice 3 | done |
| 7 | Complete results and export | Slice 4 | done |
| 8 | Scheduled crawl and revalidation | Slice 5 | done |
| 9 | Background operation and diagnostics | Slice 6 | in-progress |

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

### 5. Configurable source coverage · done

Let you configure multiple public channels, collect directly from them, and see which sources succeeded. Initial seed candidates are `warpplus`, `warppluscn`, `warpPlusHome`, and `warp_veyke`.

**Done when:** enabled sources contribute deduplicated candidates with provenance; the configured message or page coverage and any truncation are visible; repeated crawls are idempotent; one source failure does not discard other results or change a key's entitlement status.

**Spec:** [0004. Configurable source coverage](../specs/0004-configurable-source-coverage/index.md), confirmed on 2026-10-11 after independent review. All four seeds are enabled by default. Coverage uses a shared page limit with per channel overrides, sequential requests, and one saved run with source and page outcomes. Implementation, runtime verification, and regression tests are complete. You confirmed feature completion on 2026-10-11. Code in `internal/config`, `internal/source/telegram`, `internal/state`, `internal/store`, `internal/app`, and `internal/cli`.

- [x] Design it (spec): `/architect configurable source coverage`
- [x] Build it: `/develop configurable source coverage`
  - [x] Prove one configured channel through the migration, saved plan, one page crawl, and report after restart while preserving old history (AC-2, AC-4, AC-6, AC-10).
  - [x] Add the four defaults, custom source selection, source inspection, limit precedence, and independent source outcomes (AC-1, AC-2, AC-3, AC-4, AC-7).
  - [x] Prove bounded older page traversal, page records, overlap handling, request pacing, and visible coverage limits (AC-4, AC-5, AC-6, AC-9).
  - [x] Complete rate limit abort, cancellation, mutation error recovery, output errors, and privacy behavior (AC-2, AC-3, AC-7, AC-8, AC-10).
- [x] Verify it: `/check verify configurable source coverage`
- [x] Test it: `/test configurable source coverage`

The test pass on 2026-10-11 added 16 test functions across five existing test files, with acceptance references for AC-1 through AC-10. Coverage includes configuration boundaries, safe pagination, page budgets, later page failures, cancellation, saved reports, store lifecycle rejection, replay, atomic page links, migration rollback, and unchanged entitlement on rediscovery. The full repository formatting, vet, race test, and lint gate passed on macOS arm64. All five affected package suites also passed on Linux arm64 with networking disabled. Live Telegram behavior remains covered by the completed verification record. You approved feature completion and the milestone commit on 2026-10-11.

## Slice 3: Check reliably at scale

### 6. Concurrent checks and bounded retries · done

Use Go concurrency to validate independent candidates with configurable worker and request limits. Retry transient failures with increasing delay and jitter, a small random variation that avoids synchronized retries.

**Done when:** multiple checks can run with isolated state and a measurable concurrency ceiling; each key has at most one active check; timeouts and cancellation bound work; transient failures retry within a finite budget and respect server delays; exhausted retries remain inconclusive; definitive rejection does not enter an immediate retry loop.

**Spec:** [0005. Concurrent checks and bounded retries](../specs/0005-concurrent-checks-bounded-retries/index.md), confirmed on 2026-10-11 after independent review. Explicit candidate batches use two workers and three attempts by default, with shared command pacing and recovery ownership. Saved reports retain selection, effective limits, and retry history. Code in `internal/app/batch.go`, `internal/store/batch.go`, `internal/validator/batch.go`, and `internal/validator/worker/worker.go`. One authorized live protocol 2 session confirmed Unlimited on 2026-10-11, with a 1.005 second mutation gap, a durable cleanup receipt, an empty registry, no retained containers, a matching report after restart, and unchanged host registration. Controlled legacy conversion checks pass in `internal/validator/docker_test.go`. Private run evidence is in `/private/tmp/wpc-live-0005-x_vahyfc`. Verification passed on 2026-10-11 after closing the output pipe and cancellation races. Two authorized live sessions overlapped for 9.628 seconds with mutation gaps above one second, complete cleanup, saved reports after restart, and unchanged host registration. Retained acknowledgement, journal, cancellation, and staged command fault cases pass under race tests on macOS and Linux arm64. Final evidence is recorded in the feature verification file. The separate test workflow is complete; `/sync` reconciled the feature status to done.

- [x] Design it (spec): `/architect concurrent checks and bounded retries`
- [x] Build it: `/develop concurrent checks and bounded retries`
  - [x] Prove one saved batch item through configuration, migration, an unsupported check, and a report after restart, preserving old history (AC-1, AC-6, AC-9, AC-10, AC-11).
  - [x] Prove staged Docker commands, the new recovery registry, and safe legacy conversion through one isolated session (AC-3, AC-7, AC-10, AC-11).
  - [x] Add bounded concurrent workers, shared mutation pacing, fair dispatch, and complete result reporting (AC-1, AC-2, AC-3, AC-9).
  - [x] Add classified retries, saved wait plans, finite budgets, and throttling behavior without changing definitive evidence (AC-4, AC-5, AC-6).
  - [x] Complete cancellation, uncertain acknowledgements, recovery across sessions, privacy, and output failures (AC-2, AC-7, AC-8, AC-9, AC-10, AC-11).
- [x] Verify it: `/check verify concurrent checks and bounded retries` (AC-1 through AC-12, including separately authorized live Docker checks).
- [x] Test it: `/test concurrent checks and bounded retries` (13 added tests cover worker ceilings, retry bounds, terminal classifications, cleanup requirements, server delays, budget exhaustion, evidence preservation, committed output, saved reports, CLI validation, and configuration precedence. The full Go suite and macOS formatting, vet, race, and lint gate pass.)

## Slice 4: Reach every result

### 7. Complete results and export · done

Inspect outcomes and freshness, and export the full matching collection for your own use. Terminal presentation and export must make any user selected limits explicit.

**Done when:** all 338 records in a controlled dataset are reachable and exportable without duplicates or silent truncation; status and freshness filters behave consistently; text and structured output remain usable in scripts; diagnostics stay separate from exported data; empty results have clear semantics.

**Spec:** [0006. Complete results and export](../specs/0006-complete-results-export/index.md), confirmed on 2026-10-11 after independent review. Listing and export share fixed snapshot filters and explicit limits. Export provides JSON or complete key lines to stdout or a private named file that never replaces an existing path. Output is prepared before delivery; no database migration is needed.

- [x] Design it (spec): `/architect complete results and export`
- [x] Build it: `/develop complete results and export`
  - [x] Prove one saved candidate through snapshot traversal, JSON preparation, and stdout delivery after restart (AC-3, AC-5, AC-8, AC-11).
  - [x] Add complete summaries, shared filters and limits, empty results, and snapshot text listing across all 338 controlled candidates (AC-1, AC-2, AC-3, AC-4, AC-5, AC-7).
  - [x] Add key disclosure and exact key lines, private named output, and atomic refusal to replace existing destinations (AC-5, AC-6, AC-9).
  - [x] Complete cancellation, failure diagnostics, publication uncertainty, cleanup, and concurrent writer and destination checks (AC-3, AC-6, AC-8, AC-9, AC-10, AC-11).
**Code:** `internal/store/snapshot.go`, `internal/app/query.go`, `internal/cli/results.go`, `internal/cli/result_encoding.go`, and `internal/cli/output_stage.go`.

- [x] Verify it: `/check verify complete results and export` (AC-1 through AC-12 passed on macOS and Linux arm64, 2026-10-11; evidence in the feature verification checklist).
- [x] Test it: `/test complete results and export`

The test pass on 2026-10-11 added 13 test functions and extended subprocess coverage across five files, with references to spec 0006 acceptance criteria. Coverage includes complete collections, filter normalization, freshness boundaries, limit metadata, history references, disclosure, unsafe key preparation, empty and damaged state, publication races, state aliases, and callback ownership during cancellation. All 641 tests and subtests passed with the race detector on macOS arm64; one subprocess helper was intentionally skipped outside its parent test. Formatting, vet, and lint passed. The three affected internal package suites also passed on Linux arm64, with 341 tests and subtests and the same helper skip, using compiled test binaries in a container with networking disabled. Linux container compilation with race instrumentation was killed while building SQLite; the successful Linux run used ordinary test binaries. Platform command and memory verification remains recorded in the feature verification checklist. The separate test workflow is complete; `/sync` reconciled the feature status to done from implementation, verification, and test evidence.

## Slice 5: Keep results fresh

### 8. Scheduled crawl and revalidation · done

Give crawling and validation separate configurable intervals. New candidates and previously checked keys both receive appropriate future checks.

**Done when:** both schedules run independently; slow jobs do not create overlapping checks or an unbounded backlog; startup and missed interval behavior are defined; failed work gets a future attempt; restarts preserve freshness information and pending eligibility; manual runs respect the same coordination rules.

**Spec:** [0007. Scheduled crawl and revalidation](../specs/0007-scheduled-crawl-revalidation/index.md), confirmed on 2026-10-11 after independent review. Foreground `run` uses separate crawl and validation schedules, saved eligibility, small bounded batches, and shared validator cooldown recovery. Build complete, code in `internal/app/run.go`, `internal/store/schedule.go`, `internal/validator/gate.go`, and `internal/cli/schedule.go`. Service management remains in feature 9.

- [x] Design it (spec): `/architect scheduled crawl and revalidation`
- [x] Build it: `/develop scheduled crawl and revalidation`
  - [x] Prove a scheduled crawl through configuration, schema version 5, the shared writer, saved inspection, and output after restart (AC-1, AC-6, AC-10, AC-11, AC-12, AC-13).
  - [x] Add automatic candidate selection, routine and failure eligibility, history initialization, and matching manual work (AC-3, AC-4, AC-5, AC-6, AC-13).
  - [x] Prove registry version 3 conversion, shared cooldowns, single candidate probes, and recovery across profiles (AC-8, AC-9, AC-10, AC-13, AC-14).
  - [x] Allow crawl and validation overlap with bounded selection, independent controls, and persisted recovery waits (AC-1, AC-2, AC-3, AC-7, AC-8, AC-10).
  - [x] Complete clock handling, inspection consistency, fault recovery, signals, and shared shutdown deadlines on both arm64 platforms (AC-5, AC-7, AC-9, AC-11, AC-12, AC-13, AC-14, AC-15).
- [x] Verify it: `/check verify scheduled crawl and revalidation` (AC-1 through AC-15 passed on macOS and Linux arm64, 2026-10-11; evidence in the feature verification checklist).
- [x] Test it: `/test scheduled crawl and revalidation`

The test pass on 2026-10-11 added 21 test functions across eight files and extended the existing history migration test. Coverage includes configuration bounds and explicit false overrides, due time rounding and ordering, recovery of started and pending work, restart waits, manual coverage matching, terminal outcome intervals, clock repair, probe selection, private inspection, output failure, shared cleanup allowances, provider cooldowns, and the shared acquisition budget. Acceptance references live beside the tests. The full Go suite and the macOS arm64 formatting, vet, race test, and lint gate passed. All six affected internal package suites also passed as ordinary Linux arm64 test binaries in a disposable container with networking disabled. Provider responses remained controlled; real provider behavior and process crash experiments remain in the separate verification record. The separate test workflow is complete; `/sync` reconciled the feature status to done from implementation, verification, and test evidence.

## Slice 6: Operate in the background

### 9. Background operation and diagnostics · in-progress

Run unattended on the selected platforms and let you inspect progress, stop cleanly, and recover after failure.

**Done when:** you can start background operation, inspect status, and stop it; duplicate instances sharing state are prevented; shutdown saves completed work within a bounded time; restart recovers interrupted work; status reports last and next runs, queue depth, outcome counts, and source or validator failures; routine logs mask key values.

**Spec:** [0008. Background operation and diagnostics](../specs/0008-background-operation-diagnostics/index.md), confirmed on 2026-10-11, including the approved Linux removal correction. Code in `internal/service`, `internal/cli/service.go`, `internal/app/run.go`, and `internal/store/operational.go` adds controls, observations, logs, status, and doctor without a database migration. Local checks cover complete snapshots, ownership, log rotation and following, and observation failure. On 2026-10-11, the provisioned Lima VM `wpc-linux` (Ubuntu 26.04.1 arm64, systemd 259, Docker 29.9.0) proved install without state creation, explicit start, collection of 100 candidates, complete status, repeated start and stop, disable without stopping, enable, restart, and repeated uninstall with retained state. Native checks exposed and corrected systemd empty property parsing and working directory rendering in `internal/service/native.go`. The VM remains available with its test service uninstalled; Lima supplied user lingering. Further Linux checks on 2026-10-11 proved login activation in a separate account with lingering disabled, paced recovery after SIGKILL and nonzero exit, duplicate writer rejection, permanent configuration and ownership failure without restart loops, configuration path binding, diagnostic failure shutdown, native forced stop at 75.7 seconds, and the internal watchdog at 80 seconds from a blocked write (five second observation timeout plus the 75 second watchdog). Interrupted install, enable, disable, and four uninstall checkpoints recovered safely; foreign definitions and unsafe files were refused. Native evidence also led to precise Linux definition drift diagnostics and faster strict log decoding; formatting, vet, race tests, lint, both target builds, and native Linux service tests passed. Verification scripts and command evidence are retained in `/tmp/wpc-native-verification-20261011` and the VM account's `wpc-verification/native-evidence` directory. On 2026-10-12, native checks reproduced and corrected doctor reporting a pending first start as stopped. Doctor now preserves unknown ownership and pending job evidence instead of inventing interruption. The real Linux CLI passed controlled Docker readiness, socket binding, disabled validation, historical failure, clock anomaly, and missing owner checks in text and JSON. Tracing proved only Docker info and image inspection, with no candidate state creation. A runner paused before lifetime acquisition was refused after concurrent uninstall without changing the retained observation. Fifteen doctor cases and a combined crawl and validation cancellation test cover these findings and the shared 60 second cleanup plus five second finalization budget. The Linux lifecycle milestone is complete. Evidence is retained in `/tmp/wpc-diagnostics-20261012` and the VM account's `wpc-verification/diagnostics-20261012` directory. The VM test service is uninstalled. MacOS GUI lifecycle proof, combined managed remote recovery, and the remaining fault matrix are pending. This macOS process still reports the Background manager context. On 2026-10-12, you chose to continue local checks and leave macOS native proof pending. A real Linux file size limit exposed a partial native definition that blocked installation retries. The publication fix in `internal/service/control.go` stages and synchronizes complete bytes before atomic creation without replacement. The real CLI recovered after a short write, crashes immediately before and after publication, and an injected directory synchronization error. Regression cases in `internal/service/control_test.go` passed on macOS and Linux arm64 for both definition formats, exact adoption, and refusal of foreign, partial, permissive, and symlink files. The full formatting, vet, race, and lint gate passed after initial timing failures in the existing archive read and validator deadline tests passed separately. Both target builds passed. Evidence is retained in `/tmp/wpc-publication-20261012` and the VM account's `wpc-verification/publication-20261012` directory. The test service is uninstalled and its candidate state was never created. No feature completion or verification box is advanced.

- [x] Design it (spec): `/architect background operation and diagnostics`
- [ ] Build it: `/develop background operation and diagnostics`
  - [ ] Prove a macOS collection service through installation, explicit start, saved observation, status, stop, and inspection after restart (AC-1, AC-2, AC-3, AC-6, AC-8, AC-15).
  - [x] Prove the same path under a real Linux user manager, then complete login activation, repeatable controls, bound paths, stopped uninstall, and native supervision (AC-1, AC-2, AC-3, AC-4, AC-5, AC-16).
  - [x] Complete snapshot status across all 338 controlled candidates, preserving complete terminal summaries during active work and explicit unavailable components (AC-8, AC-9, AC-10, AC-11, AC-15).
  - [x] Add bounded logs, recent and following inspection, local doctor checks, and diagnostic failure handling (AC-11, AC-12, AC-13, AC-14, AC-15).
  - [ ] Complete crash recovery, service identity checks, ownership races, interrupted controls, and bounded shutdown on both platforms (AC-3, AC-4, AC-5, AC-6, AC-7, AC-14, AC-16).
- [ ] Verify it: `/check verify background operation and diagnostics`
- [ ] Test it: `/test background operation and diagnostics`

## Deferred

Billing, a hosted service, and product analytics are unnecessary for the selected personal CLI. Automatic key application and rotation, private Telegram access, unlimited historical backfill, a public API or dashboard, notifications, and public release automation are outside this initial slice. They can be enrolled later if you need them.

From spec 0006: provenance exports, complete history archives, CSV, saved export jobs, source and date filters, overwrite flags, and resumable public cursors remain deferred. None blocks complete candidate summaries and export.

From spec 0007: history retention controls, calendar schedules, live configuration reload, user queue controls, and adaptive intervals remain deferred. None blocks the confirmed interval scheduler.

From spec 0008: boot services, Linux lingering management, multiple named services, remote control, alerts, configurable log retention, and log export bundles remain deferred. None blocks the confirmed user service and diagnostics design.

## Legend

`planned` means work has been scoped. `in-progress` means design or implementation has started. `done` means the selected verification stages have completed. `existing` identifies work that predates this workflow, and `dropped` preserves a removed feature's history.

A `needs a decision` tag recommends architecture before implementation. The first unchecked box is the suggested next step. Architecture adds a spec link and a short build plan when the decision is recorded. Detailed implementation tasks stay in that spec.

All features inherit the project approach and Beta workflow. Future changes update this file in place.
