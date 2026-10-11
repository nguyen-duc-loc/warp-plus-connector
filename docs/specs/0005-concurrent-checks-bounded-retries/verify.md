# Concurrent checks and bounded retries verification

**State**: Verification passed on 2026-10-11. All twelve acceptance criteria have runtime evidence and retained fault coverage. The separate `/test` workflow remains pending.

Use private scratch profiles and synthetic keys for controlled scenarios. Real checks require the keys you explicitly authorize when verification begins. Preserve the host registration and record only redacted identities, fixed reasons, timings, and counts in this file. Suggested commands below follow the repository RTK convention.

## Selection, history, and configuration

1. Exercise one ID, several IDs, duplicate IDs, malformed and unknown IDs, and no IDs through the actual CLI. Verify no run or external work for an unknown ID, and first supplied order for distinct items. Covers AC-1.
2. Exercise defaults, each bound, invalid values, private JSON, explicit flag precedence, and invalid JSON hidden by a would be valid flag. Verify help and version with inaccessible config, state, and Docker. Covers AC-10.
3. Create a controlled batch of 338 candidates. Inspect all items and attempts through `check show` in another process without a total cap or duplicate rows. Compare saved settings after changing the current config. Read active and legacy runs honestly. Covers AC-1, AC-9.
4. Confirm final item records appear only after their commits and can arrive out of selection order. Break the output callback while other workers are active. Verify no further output, no new work, retained commits, cleanup, and a failure exit. Covers AC-7, AC-9.

## Concurrency and pacing

1. Hold controlled workers at barriers so two and four attempts overlap. Measure active slots and live owned containers, and prove the selected ceiling is never exceeded. Exercise duplicate keys and competing profiles and recovery processes. Covers AC-2.
2. Record mutation admission and completion times with a controlled monotonic clock and with the real CLI. Assert that no second registration or attachment starts until the prior command completion plus interval. Delay attachment observation after its completion marker and demonstrate useful overlap. Covers AC-3.
3. Lose an operation acknowledgement and delay a worker command. Verify no additional mutation is admitted on uncertain completion, and no delayed operation can act after recovery closed the session. Covers AC-3, AC-7.
4. Verify bounded polling and cleanup while new mutations are paused. Ensure retry waits release cleaned slots and both ready retries and untouched selections make progress. Covers AC-3, AC-4.

## Retry policy and evidence

1. Use a controlled backend for temporary failures followed by confirmation, permanent failure, device capacity, Free account, unsupported validation, generic provider error, and definitive rejection. Verify exactly the permitted attempts and no refresh of old evidence by inconclusive results. Covers AC-4, AC-6.
2. Control time and randomness to exercise every delay range, attempt exhaustion, candidate expiration during a retry wait or mutation queue wait, and cancellation. Check saved attempt numbers, chosen waits, eligible times, and stop reasons. Covers AC-4, AC-6, AC-7.
3. Supply a trustworthy delay through a controlled capable backend, including a delay beyond the candidate budget. Verify the shared pause never shortens the delay and cleanup stays available. Then use a Docker throttling fixture with no delay and prove that the batch stops without a replacement registration. Covers AC-5.
4. Time out a license operation, prove cleanup, and permit a fresh isolated retry. Repeat with ambiguous cleanup and prohibit it. Separate attribution polling timeout from malformed responses and identity mismatch. Covers AC-4, AC-6, AC-7.
5. Deliver a validated observation before the active deadline, then finish cleanup after that deadline within its allowance. Preserve confirmation. Repeat with first observation receipt after the deadline, before and after a global stop, and with uncertain store completion. Verify the exact eligibility boundary without comparing worker and host wall clocks. Covers AC-6, AC-7, AC-8.

## Failure, migration, and recovery

1. Cancel while preparing, awaiting mutation admission, registering, attaching, observing, cleaning, and waiting to retry. Hold the worker operation lock and prove status reads and cancellation markers remain accessible. Verify ordinary cleanup gets 60 seconds, shutdown clips host collection to one shared 60 second allowance, and neither repeated status nor cancellation resets either budget. Covers AC-7.
2. Inject lost store acknowledgements at batch creation, attempt creation, completion with a retry plan, and finalization. Verify no later mutation, preserved commits, separate remote cleanup, and correct next writer recovery. Covers AC-8.
3. Kill the host with several tracked sessions in different phases. Reconcile every safely identified session before a later batch. Include missing containers, malformed journals, daemon mismatch, untracked labeled containers, removal failure, and one unresolved cleanup among successful cleanups. Covers AC-7, AC-8, AC-11.
4. Upgrade a version 3 database with old history, including interrupted work. Verify migration rollback under an injected failure and unchanged legacy evidence. Upgrade both a clean and an unresolved version 1 Docker journal. Fail journal replacement after rename and before successful directory synchronization. Verify the next recovery inspects the actual version, never restores a presumed old payload, and preserves cleanup ownership. Prove conversion waits for real cleanup and old binaries reject the version 2 registry even when empty. Covers AC-11.
5. Exercise wrong worker protocol, old image for new work, wrong session receipt, broad permissions, symlinks, and endpoint mismatch. Inspect arguments, environment, logs, stdout, and stderr for synthetic secrets. Covers AC-10, AC-11.
6. Interrupt creation before acknowledgement, after acknowledged creation but before identity capture, after captured identity, and during disconnect. Exercise each persisted phase with matching, missing, and mismatched registration identity. Recovery may delete the proven existing registration but never create or attach again. Covers AC-7, AC-11.
7. Hold entered SQLite work beyond the finalization context deadline. Verify cancellation is returned to the caller, further mutations stop, and close retains writer ownership until that work ends. Report the measured close delay separately from the five second finalization budget. Covers AC-7, AC-8.

## Live acceptance and retained checks

1. Record the new immutable protocol 2 image ID and local Linux arm64 Docker identity. Obtain explicit authorization for the selected real keys at this stage. Establish host registration identity through the existing permitted read only comparison. Covers AC-10, AC-12.
2. Run at least two real isolated checks in one batch. Record observable overlap, active ceiling, command spacing, isolated session identities, attributable results, and cleanup receipts. A rate limited or capacity blocked run is evidence of that case, not proof of successful overlap. Covers AC-2, AC-3, AC-12.
3. Read the saved report after restarting. Confirm no retained remote registrations from the completed checks and unchanged host registration. Record any unavailable live condition as blocked instead of substituting a fixture result. Covers AC-7, AC-9, AC-12.
4. After behavior verification, use `/test` for retained regression tests against public boundaries. Run `rtk proxy pre-commit run --all-files`, then the supported macOS and Linux arm64 builds and relevant Linux arm64 fixture suites. Avoid real provider calls in routine tests. Covers AC-1 through AC-12.

## Results

### Build milestone proof, 2026-10-11

You authorized one real key for an isolated Docker check. The key and account identities are omitted from this record. The run used a private scratch profile, the shared validator owner and registry, and the actual CLI. No host registration mutation was performed.

The immutable Linux arm64 image was `sha256:4133e87d97078e9d4500f79a42c61c1d40116b8e4a5eb0bbb45e059dea062213`, advertising worker protocol 2 and WARP `2026.8.2100.0`. The invocation selected one candidate, one worker, the default three attempt limit, a one second mutation interval, and the default 15 minute candidate budget.

| Observation | Result | Coverage |
| --- | --- | --- |
| Separate registration and attachment admission | Registration completed at `06:21:33.785Z`; attachment was admitted at `06:21:34.790Z`, a 1.005 second gap | AC-3, one session only |
| Entitlement | Run 2, candidate 1, attempt 1 confirmed Unlimited; command exited 0 and no retry was needed | AC-6, successful result path |
| Private completion and cleanup | Captured receipt had protocol 2, `attachment_finished=true`, phase `cleaned`, and a confirmed result | AC-7, AC-11, normal cleanup path |
| Resource retirement | Version 2 registry retained zero sessions; Docker retained zero labeled validator containers | AC-7, AC-11 |
| Saved report after restart | A separate `check show 2` process exited 0 and returned the same policy, attempt, evidence, and totals | AC-9 |
| Host comparison | Registration, device, account, and license identities matched before and after the run | AC-10, partial AC-12 |

The network disabled container smoke check used synthetic input. Initialization and status reported `prepared`; cancellation and recovery produced `cleaned`; the obsolete `check` command and a later registration attempt were rejected. Session and cancellation files had mode `0600`.

`TestLegacyRecoveryConvertsOnlyAfterCleanup` passed for clean and unresolved legacy journals. The controlled Docker fixture proved that failed cleanup preserves the original journal and container, successful recovery converts to an empty version 2 registry without creation or attachment, the legacy manifest reader rejects the converted journal, and repeated recovery does not remove the container twice. This is controlled conversion evidence, not a live legacy registration experiment or an independently executed old binary.

The full Go suite passed before the added regression. After the regression and scope update, `rtk proxy pre-commit run --all-files` passed formatting, vet, race tests, and lint with toolchain cache access. macOS arm64 and Linux arm64 CLI builds passed. Linux fixture execution and the separate `/test` pass remain pending.

Private evidence remains in `/private/tmp/wpc-live-0005-x_vahyfc`: `check.stdout`, `check.stderr`, `saved-report.txt`, `events.json`, `journal-snapshots.jsonl`, and `result.json`. The scratch database and host snapshot also contain private data and are not repository artifacts. The standalone key input file was removed.

### Acceptance closure, 2026-10-11

The initial verification found that a closed output pipe terminated the host through SIGPIPE before finalization. The host now lets failed writes reach the coordinator. A later fault probe found that cancellation during an intent write could still start registration or attachment. The worker now checks cancellation again immediately after saving that intent. Both failures have retained regressions that failed before their fixes.

The final immutable worker image is `sha256:d44d3fba025706c17a9171a40dc6ec194171490c088ddfa3691892143ff3a779`. It still uses protocol 2 and WARP `2026.8.2100.0`. A network disabled image check reached `prepared`, then cancellation reached `cleaned` without registration. Existing profiles that pin an older image need this new ID to use the worker fix.

You explicitly authorized two real keys. The final CLI selected candidates 339 and 1 with two workers. Two sessions overlapped for 9.628 seconds. Registration and attachment gaps after previous completion measured 1.001, 1.005, and 1.009 seconds. Your supplied key confirmed Unlimited; the crawled key returned `not_unlimited` without retry. The command correctly exited 1 for that inconclusive result. Both cleanup receipts were captured, the registry and container list were empty afterward, and host registration was unchanged. A separate `check show 5` process exited 0. Container arguments, environment, labels, reports, logs, and the host journal contained no candidate keys.

The final live command was `wpc check 339 1 --state-dir <private-profile> --validator docker --validator-image sha256:d44d3fba025706c17a9171a40dc6ec194171490c088ddfa3691892143ff3a779 --workers 2`. Its private evidence is in `/private/tmp/wpc-debug-boundaries-s2vk_o4n`: `live-result.json`, `observations.json`, `live.stdout`, `live.stderr`, `saved.txt`, `journal.json`, and `image-smoke.json`.

Earlier actual CLI and public API probes remain in `/private/tmp/wpc-reverify-0005-jm1tjkme`. They include 36 CLI invocations, complete reports for 338 candidates, overlap at two and four workers, bounded retry sequences, migration and recovery cases, real cleanup after output failure, ordinary cleanup lasting 60 seconds, shared shutdown lasting 60.008 seconds, and store cancellation after five seconds while close retained ownership for another two seconds. These observations supplement the retained tests below.

| Criterion | Verdict and evidence |
| --- | --- |
| AC-1 | Met. `cli-ledger.jsonl` proves malformed and unknown selection created no batch, deduplication preserved order, and all 338 selected items were saved. `TestBatchSelectionAndCompleteSavedHistory` retains the boundary. |
| AC-2 | Met. `probe.jsonl` proves ceilings of two and four. Final live evidence proves two isolated sessions. The earlier authorized live `ownership.json` proves competing profiles and recovery return `validator_busy`. |
| AC-3 | Met. Final live timing and controlled mutation timing prove serialization after completion. Worker probes measure polling and cleanup bounds. `TestStagedDockerCommandFailuresRecoverWithoutNewMutations` and `TestLateCommandsCannotReplayAfterRecovery` cover delayed and uncertain operations. |
| AC-4 | Met. `remaining.jsonl` proves one, default three, and five attempt limits, temporary success, fair capacity reuse, and saved expiration. `TestBatchCandidateDeadlineWhileAwaitingMutation` uses an actual context deadline after controlled preparation time. `TestBatchCancellationDuringSavedRetryWait` preserves the committed plan without another attempt. |
| AC-5 | Met. Controlled capable backend probes respect a shared server delay and reject a delay beyond the budget without shortening it. `TestStagedDockerThrottleReachesGateBeforeCleanup` proves the Docker marker reaches admission before cleanup. `TestBatchThrottleCompletionStopsCompetingAdmission` blocks the other worker while allowing cleanup. |
| AC-6 | Met. Worker and coordinator probes cover every defined classification, definitive rejection, exhausted retries, and early versus late evidence. Previous live confirmation retained its evidence time after an unsupported check. `TestBatchLostCommitAcknowledgements/confirmed_completion` proves recovery preserves committed confirmation after acknowledgement loss. |
| AC-7 | Met. Process pipe and signal probes, every saved worker phase, active command cancellation, observation cancellation, delayed command replay, retry wait cancellation, and shared shutdown were exercised. Entered SQLite finalization returned cancellation at five seconds but retained ownership until native work ended. |
| AC-8 | Met. `TestBatchLostCommitAcknowledgements` commits real SQLite transactions and then returns a driver error at batch creation, attempt creation, retry completion, definitive completion, and finalization. It proves no subsequent store write, independent remote cleanup, preserved commits, and recovery without resuming retries. |
| AC-9 | Met. Actual reports include all 338 items and attempts, active and legacy runs, and the saved policy after config changes. Completion callbacks read committed rows. `TestCLI_ClosedPipePreservesBatchFinalization` and the real Docker pipe experiment prove failure propagation and cleanup. |
| AC-10 | Met. CLI bounds, precedence, private config, help, version, and key redaction passed. `TestDockerCommandPrivateInputAndExplicitEndpoint` drives the actual subprocess boundary and checks arguments and environment. Final live container metadata and output were scanned for both selected keys. |
| AC-11 | Met. Actual migration and rollback probes preserve old history. Multiple retained session recovery, legacy conversion, incompatible images, and an independently compiled old validator were exercised. `TestDirectorySyncFailureLeavesRenamedPayload`, `TestJournalLostAcknowledgementPoisonsOwnerAndRecoversVisiblePayload`, and `TestLegacyJournalLostAcknowledgementRecoversActualVersion` prove recovery after rename acknowledgement loss. |
| AC-12 | Met. The authorized live runs prove overlap, spacing, cleanup, restart reads, and unchanged host registration. All packages pass race tests on macOS and Linux arm64, including the retained persistence, cancellation, privacy, and eighteen staged subprocess failure cases. |

The macOS gate is `rtk proxy pre-commit run --all-files`, covering formatting, vet, race tests, and lint. Linux runs the same source with `go test -p 1 -race ./...`, Go `1.27.2`, GCC `14.2.0`, `CGO_ENABLED=1`, and editorconfig checker `4.0.2`. The isolated Linux container runs without network access; reduced compiler parallelism fits the local VM memory. Its official Go image digest is `sha256:5bc7f572bbaa98885a3a1fd9c0aa76b59e3e14e8628bfc316bbfd0c701e4818c`.

Linux evidence and the repeat command are in `/private/tmp/wpc-linux-race-20261011`: `final.log`, `final-exit-code.txt`, `final-source-sha256.json`, `environment.json`, and `run.py`. The final source comparison found no drift. You can repeat that environment with `rtk proxy python3 /private/tmp/wpc-linux-race-20261011/run.py` while the scratch container and files remain available.

No acceptance blocker remains. The scope verification box is complete. The feature status and separate `/test` workflow are unchanged.
