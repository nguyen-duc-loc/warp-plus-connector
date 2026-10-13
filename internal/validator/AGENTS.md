# Isolated validator

The governing contracts are [the Docker backend child](../../docs/specs/0003-source-crawl-validation/0003-docker-validator.md) and its batch extension in [spec 0005](../../docs/specs/0005-concurrent-checks-bounded-retries/index.md). The default adapter remains `unsupported`; selecting `docker` requires an immutable prepared image and a local Unix Docker endpoint. The tested official WARP version is `2026.8.2100.0` on Linux arm64, with worker protocol 2.

Platform admission requires Linux amd64 or arm64 and agreement between executable, local daemon, immutable image, and worker. Daemon aliases `x86_64` and `aarch64` normalize to their canonical targets; image architecture must be canonical. `InspectLocal` permits only local daemon and image inspection, with no ownership acquisition, container creation, registration, or attachment. Retained sessions keep their exact daemon, image, and container identities in the shared registry; architecture changes never prove cleanup.

Current build and runtime pin is `2026.8.2110.0`, following removal of the old version from the Bookworm indexes. Package and worker inspection and offline readiness and cancellation passed on Linux arm64; live entitlement and remote cleanup evidence above applies only to `2026.8.2100.0`. See [verification notes](README.md#package-availability-update-2026-10-13). The explicit old version pins in specs 0003 and 0005 need `/architect` reconciliation.

## Ownership

[docker.go](docker.go) owns host subprocesses and container identity checks; [batch.go](batch.go) owns shared recovery and staged sessions. The journal is under the platform default state directory, independent of profile overrides. One owner lock covers reconciliation and the whole batch, including retry waits. Its version 3 registry retains up to four sessions, the clock checkpoint, and the shared admission gate. Reconcile every retained session and reject untracked labeled containers before new work, even when a known session has a cleanup receipt.

[privatefs](privatefs/) owns private JSON replacement, synchronization, and advisory locking. Invalid or permissive files and symlinks fail closed. Record intent before external mutations. No SQL belongs here; candidate history remains in `internal/store`.

Registry changes pass through coordinator hooks. A failed journal replacement blocks every later journal write in that invocation, including when rename succeeded before directory synchronization failed. Conversion from registry versions 1 and 2 waits for proven cleanup and retains version 3 so old binaries reject it; incompatible images cannot start a new batch. Session updates preserve all gate metadata, including during `validator recover`.

[worker](worker/) runs inside the isolated container through [cmd/wpc-worker](../../cmd/wpc-worker/). Initialization persists input without external work. Registration, attachment, and recovery share a worker lock and durable phase checks; delayed commands cannot act after recovery. The host admits registration and attachment separately. The worker never connects a tunnel and never uses host registration state.

The durable attachment completion marker releases mutation serialization only after its normalized classification reaches the coordinator. Status polling can observe it before cleanup ends. The pinned Docker adapter supplies no trusted server delay, so throttling stops new batch mutations while cleanup remains allowed. Cancellation and remote cleanup use the coordinator's shared shutdown allowance.

[gate.go](gate.go) implements [spec 0007's cooldown contract](../../docs/specs/0007-scheduled-crawl-revalidation/0002-validator-cooldown.md). Persist admission intent before remote mutations. Throttling retains a deadline shared across profiles and manual checks; expiry permits only one candidate probe with one worker and attempt. Opening after a probe requires a healthy provider response, proven cleanup, container removal, and acknowledged terminal SQLite persistence. Abandoned active or probe phases become cooldowns after recovery; recovery never opens the gate or finalizes another profile's database.

Acquisition and recovery share one five minute budget. Typed results distinguish retryable backend or cleanup waits, contention, cooldown, clock regression, and fatal identity or journal errors. `CleanupInspected` records actual cleanup inspection independently of admission. Backward UTC blocks mutations but permits cleanup without lowering the saved checkpoint.

## Evidence and cleanup

Keys enter the container through standard input. Never put them in host arguments, environment variables, labels, images, repository fixtures, or diagnostics. The vendor license argument exists only inside the private container. Raw vendor output and credentials stay private.

Only a matching returned license and JSON account type `unlimited` can produce typed `Unlimited` evidence. Command success alone is insufficient. Free accounts, capacity errors, throttling, and unknown forms remain inconclusive. The observation must survive a failed cleanup or recovery.

Cleanup requires the explicit tested success response and a subsequent missing registration response, followed by a durable cleaned receipt. Missing local state alone does not prove remote deletion. Unknown outcomes retain the container and block new attachment. The measured creation HTTP 429 exception requires exactly one matching failure and terminal event in order, with no registration present; do not broaden it to generic API failures.

## Commands

Use `rtk proxy sh scripts/build-validator-image.sh` to build the pinned image and print its immutable ID. Preparation explicitly loads the image and resolves its tag to the local immutable ID, then verifies metadata, the installed package, and worker bytes; BuildKit `--iidfile` output is not a portable local lookup key. Release users can pull the matching GHCR image and obtain its ID with `docker image inspect`. `scripts/smoke-validator-image.sh` checks readiness and local cancellation with networking disabled and synthetic input only. Checks do not install packages or pull images. `wpc validator recover` performs cleanup only; new checks also reconcile first.

Focused checks are `rtk proxy go test -race ./internal/validator/... ./internal/app ./internal/config`. The repository gate also covers the worker command. Live fault experiments use private scratch profiles and explicitly authorized keys; retained tests use synthetic values.

_Drafted by /sync from the introducing change, worth a quick human pass._
