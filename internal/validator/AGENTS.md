# Isolated validator

The governing contract is [the Docker backend child](../../docs/specs/0003-source-crawl-validation/0003-docker-validator.md). The default adapter remains `unsupported`; selecting `docker` requires an immutable prepared image and a local Unix Docker endpoint. The tested official WARP version is `2026.8.2100.0` on Linux arm64.

## Ownership

[docker.go](docker.go) owns host subprocesses, container identity checks, and shared recovery. Its journal is under the platform default state directory, independent of profile overrides. The owner lock stays held throughout reconciliation and a check. Reject untracked labeled containers, even when the known session already has a cleanup receipt.

[privatefs](privatefs/) owns private JSON replacement, synchronization, and advisory locking. Invalid or permissive files and symlinks fail closed. Record intent before external mutations. No SQL belongs here; candidate history remains in `internal/store`.

[worker](worker/) runs inside the isolated container through [cmd/wpc-worker](../../cmd/wpc-worker/). Initialization persists input without external work. Start and recovery share a worker lock; a delayed start cannot act after recovery. The worker never connects a tunnel and never uses host registration state.

## Evidence and cleanup

Keys enter the container through standard input. Never put them in host arguments, environment variables, labels, images, repository fixtures, or diagnostics. The vendor license argument exists only inside the private container. Raw vendor output and credentials stay private.

Only a matching returned license and JSON account type `unlimited` can produce typed `Unlimited` evidence. Command success alone is insufficient. Free accounts, capacity errors, throttling, and unknown forms remain inconclusive. The observation must survive a failed cleanup or recovery.

Cleanup requires the explicit tested success response and a subsequent missing registration response, followed by a durable cleaned receipt. Missing local state alone does not prove remote deletion. Unknown outcomes retain the container and block new attachment. The measured creation HTTP 429 exception requires exactly one matching failure and terminal event in order, with no registration present; do not broaden it to generic API failures.

## Commands

Use `rtk proxy sh scripts/build-validator-image.sh` to build the pinned image and print its immutable ID. Checks do not install packages or pull images. `wpc validator recover` performs cleanup only; new checks also reconcile first.

Focused checks are `rtk proxy go test -race ./internal/validator/... ./internal/app ./internal/config`. The repository gate also covers the worker command. Live fault experiments use private scratch profiles and explicitly authorized keys; retained tests use synthetic values.

_Drafted by /sync from the introducing change, worth a quick human pass._
