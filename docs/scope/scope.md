# Scope: Warp Plus Connector

A personal CLI that collects publicly shared WARP Plus key candidates, checks their entitlement, and keeps results fresh while running in the background. You can inspect and export the complete collection without a fixed 100 key cap.

**Build approach:** Tracer Bullet (prove one real crawl, check, save, and list path, then expand it).
**Workflow:** Beta (after development, verify behavior with `/check verify`, then add meaningful coverage with `/test`).

You selected this approach and workflow. Each feature starts with its first suggested command; you can adjust the sequence as the project develops.

## Boundaries and decisions

Your fixed requirements are Go, Cobra, concurrent key checks, retries, scheduled crawling, scheduled revalidation, background operation, and access to every collected result. The installed `warp-cli` is available for testing. Its presence alone does not establish a validation backend.

Your selected boundary is personal use on Linux amd64 and Linux arm64 only, with configurable public Telegram channels and no Telegram login. Your decision on 2026-10-12 replaces the earlier macOS and Linux arm64 boundary and brings Linux amd64 into the first version. macOS support, release artifacts, native service operation, and verification are outside this scope. macOS verification does not block completion or release. The CLI lists and exports checked keys; you apply a key manually outside the tool. Crawling defaults to hourly and rechecks to every six hours, both configurable. Validation uses isolated state and preserves your current WARP connection. No delivery deadline or numerical throughput target was provided.

**Platform transition:** Release builds, installation, the validator, background operation, and runtime verification must cover both Linux architectures. Existing macOS checks remain historical evidence only. Completed feature records retain their original verification results; they do not establish Linux amd64 support. Building a binary alone does not establish runtime support.

**Next recommended step:** `/architect terminal foundation and overview`. On 2026-10-14, you selected development experience and the user interface as the next priority and confirmed spec 0009 after independent review. Feature 9 retains its recorded verification limits and status; this change in priority does not claim new backend acceptance.

**Terminal experience, 2026-10-14:** You requested readable development logs using the supplied zerolog console example as a visual reference, plus Ink (React in the terminal) for the user interface. Spec 0009 confirms zerolog throughout application logging, readable terminal presentation, and preserved service archives. Go and Cobra retain the backend and explicit command paths. Architecture will define how your requested UI works with that backend and how it is installed on both supported Linux architectures.

You selected an interactive UI that opens when you run `wpc` in a supported terminal, with result browsing, export, manual crawls, selected checks, and progress. Explicit commands remain available for scripts. The proposed sequence is readable logs, a real terminal overview, complete result browsing, then interactive work. Each slice inherits Tracer Bullet and Beta. The scope contains no reference notes.

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
| 10 | Release automation | Release | in-progress |
| 11 | Readable development logs | Slice 7 | done |
| 12 | Terminal foundation and overview | Slice 8 | done |
| 13 | Terminal results and export | Slice 9 | done |
| 14 | Terminal crawl and check controls | Slice 10 | in-progress |

## Foundations

### 1. Stack and architecture · done

Record the remaining architecture around your Go and Cobra choices, then create the smallest runnable CLI. Resolve validation feasibility before committing to a concurrency model.

**Done when:** the architecture records supported platforms, validator boundaries and evidence, state isolation, configuration boundaries, and persistence responsibilities; the scaffold builds and exposes working help and version output.

**Spec:** [0001. Stack and architecture](../specs/0001-stack-architecture/index.md), originally confirmed on 2026-10-10. You confirmed its Linux amd64 and arm64 amendment on 2026-10-12 after independent review. The completed scaffold retains its historical evidence; implementation and verification of the platform transition are tracked in feature 9.

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

Deliver matching Linux amd64 and arm64 releases and validator images. Run unattended and let you inspect progress, stop cleanly, and recover after failure.

**Done when:** on both Linux architectures, you can start background operation, inspect status, and stop it; duplicate instances sharing state are prevented; shutdown saves completed work within a bounded time; restart recovers interrupted work; status reports last and next runs, queue depth, outcome counts, and source or validator failures; routine logs mask key values; both Linux release targets, matching validators, and existing command contracts pass the platform transition checks.

**Spec:** [0008. Background operation and diagnostics](../specs/0008-background-operation-diagnostics/index.md), confirmed on 2026-10-11, including the approved Linux removal correction. Code in `internal/service`, `internal/cli/service.go`, `internal/app/run.go`, and `internal/store/operational.go` adds controls, observations, logs, status, and doctor without a database migration. Local checks cover complete snapshots, ownership, log rotation and following, and observation failure. On 2026-10-11, the provisioned Lima VM `wpc-linux` (Ubuntu 26.04.1 arm64, systemd 259, Docker 29.9.0) proved install without state creation, explicit start, collection of 100 candidates, complete status, repeated start and stop, disable without stopping, enable, restart, and repeated uninstall with retained state. Native checks exposed and corrected systemd empty property parsing and working directory rendering in `internal/service/native.go`. The VM remains available with its test service uninstalled; Lima supplied user lingering. Further Linux checks on 2026-10-11 proved login activation in a separate account with lingering disabled, paced recovery after SIGKILL and nonzero exit, duplicate writer rejection, permanent configuration and ownership failure without restart loops, configuration path binding, diagnostic failure shutdown, native forced stop at 75.7 seconds, and the internal watchdog at 80 seconds from a blocked write (five second observation timeout plus the 75 second watchdog). Interrupted install, enable, disable, and four uninstall checkpoints recovered safely; foreign definitions and unsafe files were refused. Native evidence also led to precise Linux definition drift diagnostics and faster strict log decoding; formatting, vet, race tests, lint, both target builds, and native Linux service tests passed. Verification scripts and command evidence are retained in `/tmp/wpc-native-verification-20261011` and the VM account's `wpc-verification/native-evidence` directory. On 2026-10-12, native checks reproduced and corrected doctor reporting a pending first start as stopped. Doctor now preserves unknown ownership and pending job evidence instead of inventing interruption. The real Linux CLI passed controlled Docker readiness, socket binding, disabled validation, historical failure, clock anomaly, and missing owner checks in text and JSON. Tracing proved only Docker info and image inspection, with no candidate state creation. A runner paused before lifetime acquisition was refused after concurrent uninstall without changing the retained observation. Fifteen doctor cases and a combined crawl and validation cancellation test cover these findings and the shared 60 second cleanup plus five second finalization budget. The Linux lifecycle milestone is complete. Evidence is retained in `/tmp/wpc-diagnostics-20261012` and the VM account's `wpc-verification/diagnostics-20261012` directory. The VM test service is uninstalled. MacOS GUI lifecycle proof, combined managed remote recovery, and the remaining fault matrix are pending. This macOS process still reports the Background manager context. On 2026-10-12, you chose to continue local checks and leave macOS native proof pending. A real Linux file size limit exposed a partial native definition that blocked installation retries. The publication fix in `internal/service/control.go` stages and synchronizes complete bytes before atomic creation without replacement. The real CLI recovered after a short write, crashes immediately before and after publication, and an injected directory synchronization error. Regression cases in `internal/service/control_test.go` passed on macOS and Linux arm64 for both definition formats, exact adoption, and refusal of foreign, partial, permissive, and symlink files. The full formatting, vet, race, and lint gate passed after initial timing failures in the existing archive read and validator deadline tests passed separately. Both target builds passed. Evidence is retained in `/tmp/wpc-publication-20261012` and the VM account's `wpc-verification/publication-20261012` directory. The test service is uninstalled and its candidate state was never created. No feature completion or verification box is advanced. Further log recovery checks on 2026-10-12 exposed a first log write failure that left the managed observation at starting. The fix in `internal/service/run.go` saves a terminal `service_log_failed` observation through the independent working sink. Regression cases in `internal/service/run_test.go` use child process file limits and isolated account resolution to prove archive open and first append failure without application work. They passed on macOS and Linux arm64. The real Linux CLI passed six rotation crash checkpoints, six directory synchronization faults, and a follower paused across seven rotations and invocations. It resumed with explicit gaps and 67,490 retained records without duplicates; cancellation and a broken consumer ended only the log command. Native systemd startup reported the saved log failure with native exit 0 and no restart during 35 seconds. Formatting, vet, race tests, lint, both target builds, and the Linux service suite passed. Evidence is retained in `/tmp/wpc-log-recovery-20261012` and the VM account's `wpc-verification/log-recovery-20261012` directory. The fixture service is uninstalled, earlier logs are preserved, and no candidate state was created. MacOS GUI proof, combined managed remote recovery, the remaining identity and control faults, and complete privacy fixtures remain pending. No build, verification, or test box is advanced. On 2026-10-12, implementation added Linux amd64 and arm64 release targets, matching executable, daemon, image, and worker admission, Linux service admission, and validated image preparation in `scripts/validator-image`. The existing Debian pin covers both targets. Native arm64 service checks retained 100 candidates across restart; real doctor checks verified image readiness and refused missing labels, missing images, foreign manager metadata, and a changed systemd policy without candidate state or container creation. Linux testing reproduced a deadline propagation race before worker mutation. `internal/validator/worker` now checks deadline expiry before admission and vendor process execution; deterministic regressions and 100 repetitions of both deadline cases passed. The local gate, full Linux arm64 Docker gate, release configuration, both Linux archives, and final arm64 image inspection passed. Evidence is in `/tmp/wpc-platform-20261012` and the VM account's `wpc-verification/platform-20261012` directory. The service fixture is uninstalled. Docker amd64 version execution was emulated and does not establish native support. Native amd64 service and provider proof, combined managed remote recovery, and remaining faults and privacy coverage remain pending. No milestone, build, verification, or test box is advanced. On 2026-10-12, startup confirmation in `internal/service/control.go` was unified so new and repeated starts require agreeing invocation, native identity, and kernel ownership samples. Twenty eight startup cases passed locally and on Linux arm64 and native Linux amd64. A temporary EC2 `t3.micro` with Ubuntu 24.04, systemd 255.4, Docker 29.1.3, and Go 1.27.2 proved amd64 install without work, collection of 100 candidates, repeatable controls, login activation with lingering disabled, paced crash restart, writer exclusion, configuration failure recovery, native policy drift refusal, and forced stop at 75.7 seconds. The matching validator image passed preparation with official Buildx v0.38.0; Ubuntu Buildx 0.30.1 returned an unusable config digest and was refused. The authorized provider check saved `device_limit` without retry, then proved cleanup and an independent saved read. A managed crash after registration recovered the retained session without resuming attachment. Independent host inspection found both temporary devices absent and the host registration unchanged. Routine service artifacts and outputs did not disclose any of the 100 collected keys. The local gate passed. Native formatting, vet, lint, and every other race package passed; `TestLogRotationAndTailRecovery` repeatedly exceeded its five second read budget under race instrumentation on the small VM, while the uninstrumented case passed in 2.75 seconds. The profile attributed most time to race instrumentation. Evidence is retained in `/tmp/wpc-amd64-aws-20261012`. A fresh Unlimited confirmation remains blocked by subscription device capacity; the native race gate and remaining fault matrix remain open. No milestone, build, verification, or test box is advanced. On 2026-10-13, the log investigation separated full archive race coverage from wall clock performance and added virtual clock checks for the shared five second read budget and three attempt limit. Those checks reproduced a late snapshot being accepted after cancellation. `internal/service/logs.go` now rejects late or partial results and checks cancellation while scanning records. All three late result regressions fail without the new guard. The local gate and full Linux arm64 formatting, vet, race, and lint gate passed. The original full archive public read and recovery workload passed on native amd64 in 2.70 seconds with the unchanged production timeout. A session time gap outlasted the followup VM; its final native gate output was not captured, so no complete amd64 gate pass is claimed. Evidence and the remaining cleanup procedure are retained in `/tmp/wpc-log-budget-20261012`. Positive entitlement, final native amd64 gate evidence, and the remaining fault matrix stay pending. No completion box is advanced. Later on 2026-10-13, refreshed credentials completed the pending cloud cleanup and a new bounded native amd64 run captured a complete passing formatting, vet, race, and lint gate. The exact source matched the successful local and Linux arm64 runs. The native service package passed under race instrumentation, including the full archive integrity test and virtual clock deadline regressions. Evidence was uploaded privately during execution and retained locally in `/tmp/wpc-final-amd64-20261013`. This closes the missing native amd64 gate evidence for these fixes. The authorized subscription still had five devices, so no new attachment was attempted. Positive Unlimited confirmation and the remaining runtime fault matrix remain pending; no completion box is advanced. On 2026-10-13, you requested closure of the remaining build milestones. Implementation and the exercised native command paths are complete, so both milestones and Build it are checked. Under the Beta workflow, fresh Unlimited confirmation and the remaining runtime fault scenarios stay pending for Verify it, with separate test workflow closure still pending. The feature and governing spec remain In Progress; these build checkboxes do not claim that every acceptance criterion has passed.

**Scope update, 2026-10-12:** You confirmed the Linux architecture, validator, and service amendment after independent review. The macOS lifecycle milestone is dropped. Earlier macOS pending notes above describe the previous scope and no longer block this feature. Existing Linux arm64 evidence remains valid. Linux amd64 lifecycle proof, combined managed remote recovery, remaining identity and control faults, and complete privacy fixtures remain open. Native service verification needs a real Linux user manager on each architecture.

- [x] Design it (spec): `/architect background operation and diagnostics` (Linux platform amendment confirmed through `/architect stack and architecture` on 2026-10-12).
- [x] Build it: `/develop background operation and diagnostics`
  - [x] Prove Linux amd64 end to end: release build, matching validator image, attributable saved check, and collection service through install, explicit start, status, stop, and restart, including login activation and native supervision (AC-1 through AC-6, AC-8, AC-15 through AC-19).
  - [x] Prove the collection service path under a real Linux arm64 user manager, then complete login activation, repeatable controls, bound paths, stopped uninstall, and native supervision (AC-1, AC-2, AC-3, AC-4, AC-5, AC-16).
  - [x] Complete snapshot status across all 338 controlled candidates, preserving complete terminal summaries during active work and explicit unavailable components (AC-8, AC-9, AC-10, AC-11, AC-15).
  - [x] Add bounded logs, recent and following inspection, local doctor checks, and diagnostic failure handling (AC-11, AC-12, AC-13, AC-14, AC-15).
  - [x] Complete crash recovery, service identity checks, ownership races, interrupted controls, and bounded shutdown on Linux amd64 and Linux arm64 (AC-3, AC-4, AC-5, AC-6, AC-7, AC-14, AC-16, AC-18, AC-19).
- [x] Verify it: `/check verify background operation and diagnostics` (AC-1 through AC-19 and the [platform transition checklist](../specs/0001-stack-architecture/verify.md), within the verification boundary recorded below).
- [x] Test it: `/test background operation and diagnostics` (both Linux architectures and the complete repository gate).

**Verification, 2026-10-13:** You requested checking only Verify it. Runtime checks passed, including fresh CLI checks against the final Linux arm64 binary; the unchanged source also passed the full local and native Linux amd64 and arm64 test gates. New provider attachment and fresh Unlimited entitlement proof remain excluded at your request and unverified. This checkbox records the completed verification within that boundary, not full feature acceptance. The feature remains in progress. Evidence is retained in `/tmp/wpc-final-0008-20261013/REPORT.md` and `/tmp/wpc-final-0008-20261013/REVERIFY.md`.

**Test pass, 2026-10-13:** Added 28 regression cases by extending the log event catalogue checks and adding gap callback cancellation, output failure, and public status isolation tests. Existing doctor regression coverage is retained and tagged to the acceptance criteria. The full race suite passed with 945 tests and subtests locally and 964 on native Linux arm64, across thirteen tested packages on each host. Formatting, vet, and lint passed. The updated service tests compile for Linux amd64; they were not newly executed on that architecture. The earlier native amd64 full gate is retained for the unchanged application code. Provider attachment remains excluded and unverified. Evidence is in `/tmp/wpc-test-0008-20261013/REPORT.md`. Only Test it is newly checked; the feature and spec remain in progress.

## Release: Existing automation

### 10. Release automation · in-progress

Build and publish matching Linux binaries and validator images through the release workflow you selected on 2026-10-13. This entry reconciles automation already present in the repository with the earlier deferred scope.

**Done when:** actual hosted execution proves both native gates, packaged releases, and anonymous access to matching validator images; configuration files alone do not establish publication.

**Code:** `.github/workflows/ci.yml`, `.github/workflows/release.yml`, and `.goreleaser.yaml`. The operational contract is `docs/releasing.md`. Hosted completion is not established by the local evidence inspected in this scope pass.

- [ ] Verify it: `/check verify release automation`

## Slice 7: Read development activity at a glance

### 11. Readable development logs · done

Help you follow development runs with compact local times, distinct level labels and colors, clear messages, and quieter supporting fields. Keep run and attempt identity available when concurrent work needs diagnosis.

**Done when:** foreground diagnostics and displayed service logs are easy to scan; verbosity is selectable; date and timezone context remain discoverable; warnings and failures are understandable without color; redirected output and saved service records contain no terminal styling; keys remain private and command results stay separate from diagnostics.

**Spec:** [0009. Readable development logs](../specs/0009-readable-development-logs/index.md), confirmed on 2026-10-14 after independent review and application of its recommended fixes. The build is complete. Native logging verification passed on Linux amd64 and arm64 on 2026-10-14.

**Code:** `internal/diagnostic`, `internal/app`, `internal/cli/diagnostics.go`, `internal/cli/service_logs.go`, `internal/service/run.go`, and `cmd/wpc/main.go`. Local build evidence and tested dependencies are recorded in `internal/diagnostic/README.md`.

- [x] Design it (spec): `/architect readable development logs`
- [x] Build it: `/develop readable development logs`
  - [x] Prove one crawl through zerolog, presentation flags, readable and redirected output, safe final errors, and diagnostic failure handling (AC-1 through AC-6, AC-9 through AC-11).
  - [x] Complete concurrent check and foreground runner logging, exact event fields and severity, and cancellation integration (AC-1, AC-4, AC-6, AC-9 through AC-11).
  - [x] Preserve managed service capture and add readable finite and following service logs with filtering and complete retention context (AC-3, AC-7 through AC-11).
- [x] Verify it: `/check verify readable development logs` (AC-1 through AC-12, including both Linux architectures).
- [x] Test it: `/test readable development logs`

**Test pass, 2026-10-14:** Added 19 regression cases to `internal/cli/results_diagnostics_test.go` for spec 0009 AC-6, AC-9, and AC-10. They cover errors reported after accepting all metadata bytes, wrapped failure suppression, private writer error text, and healthy signal diagnostics at error verbosity when cancellation occurs before or after metadata delivery. Formatting, vet, the full race suite, and lint passed on macOS arm64 through `rtk proxy pre-commit run --all-files`. Native Linux execution was not repeated in this test pass; the earlier verification remains recorded in spec 0009. Only Test it is newly checked. The feature and spec remain in progress.

## Slice 8: Open a useful terminal overview

### 12. Terminal foundation and overview · done

Give you one keyboard driven home screen showing real collection totals, freshness, recent work, schedules, and service health. Establish the visual language and backend connection through this usable first screen.

**Done when:** bare `wpc` opens the UI in a supported interactive terminal on Linux amd64 and arm64; installation and startup work on both; scripts retain explicit commands and a deterministic plain fallback; help and version retain their existing isolation; navigation, focus, resizing, no color use, loading, empty state, stale data, and unavailable components are clear; opening or leaving the overview does not start or stop background work.

**Spec:** [0010. Terminal foundation and overview](../specs/0010-terminal-foundation-overview/index.md), confirmed on 2026-10-14 after independent review. Code in `internal/terminal`, `internal/app/overview.go`, `internal/store/overview.go`, `internal/service/overview.go`, `internal/cli/terminal.go`, and `ui/` implements the private Go and Ink connection, complete collection snapshot, independent service evidence, and compact screen. Both Linux archives build and pass payload integrity checks. Both final archives pass native amd64 and arm64 PTY checks on stock Debian 10 with glibc 2.28 and Ubuntu 26.04.1, covering 105 saved candidates, absent state, navigation, refresh, resize, no color, quit, signals, and child crash restoration. Local formatting, vet, race tests, lint, UI checks, GoReleaser validation, and workflow validation pass. [Hosted CI run 38577012410](https://github.com/nguyen-duc-loc/warp-plus-connector/actions/runs/38577012410) passed both native quality jobs, snapshot packaging, and both final extracted archive jobs. All build milestones are complete. Acceptance verification passed on 2026-10-15 for code commit `3c76a4310a704f31f01f542968a3fd3e2128e106`, including [CI run 38677400258](https://github.com/nguyen-duc-loc/warp-plus-connector/actions/runs/38677400258) and the full controlled runtime matrix. The Beta test workflow passed on 2026-10-15. You approved marking the feature done and the spec Accepted on 2026-10-15.

- [x] Design it (spec): `/architect terminal foundation and overview`
- [x] Build it: `/develop terminal foundation and overview`
  - [x] Prove the packaged Go and Ink path with one real collection total, private protocol, terminal startup and exit, and both Linux runtime targets (AC-1 through AC-3, AC-12 through AC-15).
  - [x] Complete collection freshness, recent runs, saved schedules, independent service observations, and bounded refresh with clear missing or stale data (AC-4 through AC-9, AC-12, AC-14).
  - [x] Complete keyboard navigation, resizing, no color presentation, failure and signal cleanup, and release verification integration (AC-8 through AC-15).
- [x] Verify it: `/check verify terminal foundation and overview` (AC-1 through AC-15, including native amd64 and arm64 final archives).
- [x] Test it: `/test terminal foundation and overview` (2026-10-15, added 23 tests across 10 files and extended snapshot refresh coverage. Local formatting, vet, full Go race tests, lint, UI compilation, and all 34 Node tests pass. Native Linux archive and visual checks retain the prior acceptance evidence and were not rerun in this test pass.)

## Slice 9: Browse and export every result

### 13. Terminal results and export · done

Let you browse saved candidates, filter by outcome and freshness, inspect details, and export the matching collection through the terminal interface. Build on the terminal foundation and existing complete result behavior.

**Done when:** every record in the 338 candidate fixture is reachable without silent truncation; selection survives navigation predictably; freshness and confirmation time are clear; keys are hidden until explicit disclosure; export selection, destination, completion, and failure are understandable and preserve existing privacy and publication guarantees.

**Spec:** [0011. Terminal results and export](../specs/0011-terminal-results-export/index.md), confirmed on 2026-10-15 after independent review and application of both approved fixes. The implementation uses private fixed results snapshots, complete paged details, session marks, explicit key disclosure, and reviewed file export through the Go parent. The build and native archive checks passed on 2026-10-15 at commit `19277c7`. Behavior verification passed on 2026-10-15 at commit `bfdc949`, including both final native archives. The separate Beta test stage passed on 2026-10-15.

**Code:** [results session](../../internal/app/results_session.go), [private store](../../internal/store/browse.go), [terminal bridge](../../internal/terminal/results_dispatch.go), [file export](../../internal/cli/terminal_export.go), and [Results screens](../../ui/src/results-screen.tsx).

- [x] Design it (spec): `/architect terminal results and export`
- [x] Build it: `/develop terminal results and export`
  - [x] Prove protocol 2, one real private results snapshot, paged browsing, and a reviewed JSON file export through the packaged terminal (AC-1, AC-7 through AC-10, AC-12 through AC-16).
  - [x] Complete filters, exact counts, marks, refresh reconciliation, and retained snapshots alongside concurrent writers (AC-1 through AC-3, AC-6 through AC-8, AC-11, AC-12, AC-14).
  - [x] Complete separate detail snapshots, all provenance and attempt history, saved retry facts, and explicit key reveal (AC-4 through AC-6, AC-11 through AC-14).
  - [x] Complete marked exports, disclosure formats, cancellation, publication receipts, cleanup warnings, and bounded shutdown (AC-3, AC-5, AC-7 through AC-13, AC-15).
  - [x] Complete keyboard and failure behavior, CLI compatibility, and matching native Linux archive integration (AC-1 through AC-16). [CI run 38696330010](https://github.com/nguyen-duc-loc/warp-plus-connector/actions/runs/38696330010) passed at commit `19277c7`. Both final archives passed checksum and payload validation and the complete packaged suite on native amd64 and arm64, with host glibc 2.43 and baseline glibc 2.28. The journeys cover every one of 338 rows at wide and 60 by 16 sizes, retained export failures, explicit command compatibility, and cancellation before job acknowledgement. Help and resize preserve scroll position, and export review actions stay visible.
- [x] Verify it: `/check verify terminal results and export` (AC-1 through AC-16, including real fault probes and final native amd64 and arm64 archives. [CI run 38703513452](https://github.com/nguyen-duc-loc/warp-plus-connector/actions/runs/38703513452) passed.)
- [x] Test it: `/test terminal results and export` (2026-10-15, added 40 regression cases across six test files. Snapshot identity, cancelled activation, fixed freshness, sparse marks, private bounded pages, review invalidation, exact exports, destination races, protocol validation, keyboard forms, and disclosure clearing pass. Local formatting, vet, the full Go race suite, lint, UI compilation, and all 49 Node tests pass. Native archive, visual, memory scaling, and operating system fault probes retain the prior verification evidence and were not repeated in this test pass. Feature and spec status remain unchanged.)

## Slice 10: Run work and understand its progress

### 14. Terminal crawl and check controls · in-progress

Let you start a crawl or check selected candidates, follow concurrent progress, and inspect the saved outcome. Present meaningful activity and recovery guidance in the main view, with technical details available when needed.

**Done when:** you can choose configured sources or saved candidates, start work, follow counts and attempts, and request cancellation; rate limits, retries, partial success, and unstarted work are distinct from invalid keys; saved results remain inspectable after exit; background ownership conflicts give clear guidance; exit and cancellation preserve existing cleanup and recovery rules without launching a second writer or duplicating scheduling logic.

**Spec:** [0012. Terminal crawl and check controls](../specs/0012-terminal-crawl-check-controls/index.md), confirmed on 2026-10-15 after independent review, application of both approved fixes, and a focused recheck. The design adds one reviewed manual operation, typed progress, independent browsing and export, complete fixed report snapshots, and bounded cleanup through the existing Go engines. Protocol 3 extends the terminal connection; the live database remains schema 5. Implementation is present in the working tree. Native amd64 and arm64 archive verification and current pinned live validator cleanup passed on 2026-10-15; the observed sourced candidate was inconclusive, not confirmed Unlimited. Evidence is in the feature verification record.

- [x] Design it (spec): `/architect terminal crawl and check controls`
- [x] Build it: `/develop terminal crawl and check controls`
  - [x] Prove one reviewed crawl through protocol 3, real saved progress, cancellation, outcome, and process deadlines (AC-1, AC-2, AC-5, AC-6, AC-8, AC-9, AC-14, AC-15).
  - [x] Prove a focused candidate check with frozen identity admission, configured policy, shared ownership, cleanup, and truthful receipts (AC-3 through AC-6, AC-8 through AC-10, AC-14, AC-15).
  - [x] Complete source and batch selection, concurrent progress, retry guidance, independent cancellation, browsing, and export (AC-2 through AC-8, AC-10, AC-13 through AC-15).
  - [x] Complete recent and older saved reports, fixed snapshots, every bounded detail page, legacy data, retained failures, and outcome acknowledgement (AC-6, AC-7, AC-11 through AC-15).
  - [x] Complete keyboard, privacy, uncertainty, shutdown, explicit command compatibility, and final native Linux archive integration (AC-1 through AC-16).

**Code:** `internal/app/work.go`, `internal/app/reports.go`, `internal/terminal/work_dispatch.go`, `internal/terminal/report_dispatch.go`, and `ui/src/controls.tsx`. Protocol 3 bundles build for both architectures. Controlled protocol and native arm64 terminal journeys cover saved work, cancellation, ownership conflicts, and complete reports.

- [x] Verify it: `/check verify terminal crawl and check controls`
- [ ] Test it: `/test terminal crawl and check controls`

## Deferred

macOS support, release artifacts, and native service verification are outside the first version as of 2026-10-12. Existing macOS implementation and evidence do not create a completion requirement. Linux amd64 is now in scope; no 32 bit architecture is included.

Billing, a hosted service, and product analytics are unnecessary for the selected personal CLI. Automatic key application and rotation, private Telegram access, unlimited historical backfill, a public API or web dashboard, and notifications are outside this initial slice. They can be enrolled later if you need them. Public release automation was brought into scope on 2026-10-13 and is tracked in feature 10.

For the first terminal UI, configuration editing, service installation and lifecycle controls, and additional interface languages remain deferred. Existing commands and configuration files continue to cover setup and service management. The first UI uses English, matching the current commands.

From spec 0006: provenance exports, complete history archives, CSV, saved export jobs, source and date filters, overwrite flags, and resumable public cursors remain deferred. None blocks complete candidate summaries and export.

From spec 0007: history retention controls, calendar schedules, live configuration reload, user queue controls, and adaptive intervals remain deferred. None blocks the confirmed interval scheduler.

From spec 0008: boot services, Linux lingering management, multiple named services, remote control, alerts, configurable log retention, and log export bundles remain deferred. None blocks the confirmed user service and diagnostics design.

## Legend

`planned` means work has been scoped. `in-progress` means design or implementation has started. `done` means the selected verification stages have completed. `existing` identifies work that predates this workflow, and `dropped` preserves a removed feature's history.

A `needs a decision` tag recommends architecture before implementation. The first unchecked box is the suggested next step. Architecture adds a spec link and a short build plan when the decision is recorded. Detailed implementation tasks stay in that spec.

All features inherit the project approach and Beta workflow. Future changes update this file in place.
