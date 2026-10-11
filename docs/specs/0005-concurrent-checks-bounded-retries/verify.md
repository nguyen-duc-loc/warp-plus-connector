# Concurrent checks and bounded retries verification

**State**: Partial verification on 2026-10-11. The single session build proof passed. Full acceptance verification remains pending, including live overlap with two distinct authorized keys.

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

### Remaining acceptance work

The checklist above remains the verification plan. One candidate cannot demonstrate concurrent checks of independent keys. Live overlap requires a second distinct authorized key and must still establish the worker ceiling and global mutation spacing across sessions. Finite retries, cancellation races, uncertain acknowledgements, complete report paging, fault recovery, and both platform fixture runs still need the full acceptance pass. No full AC-12 result or feature completion is claimed here.
