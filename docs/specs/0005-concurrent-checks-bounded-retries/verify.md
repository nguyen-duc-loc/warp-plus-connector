# Concurrent checks and bounded retries verification

**State**: Not run. This is the acceptance checklist for the proposed build.

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

No implementation, runtime verification, regression test pass, or live acceptance has been performed for this spec yet.
