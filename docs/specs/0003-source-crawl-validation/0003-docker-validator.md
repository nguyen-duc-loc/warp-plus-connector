# Docker validator backend

## Summary

You can check one saved candidate with the official WARP client in an isolated Linux arm64 container. The application saves a confirmation only after the returned license matches the input and temporary registration cleanup succeeds. A private recovery record survives interruption and prevents another attachment while cleanup is uncertain.

## Decision and evidence

This child completes the final build milestone requested on 2026-10-10. You selected automatic reconciliation of an earlier session before a new check. It extends the existing Go, Docker, SQLite, and unsupported adapter decisions.

The experiment used official WARP `2026.8.2100.0` on Debian Bookworm arm64, ordinary Docker capabilities, bridge networking, no mounts, and no privileged access. Candidate 29 returned `account.type=unlimited` with a matching `account.license`. Twenty temporary registrations received successful remote deletion responses. A separate authorized registration later confirmed that the successful experiment device was absent from the same account. No host registration was used to validate candidates.

The experiment also observed `ApiErrorTooManyDevices` and delayed account updates after successful license application. Neither command success nor an unchanged Free account establishes candidate entitlement. Interruption and uncertainty measurements remain required before the build milestone is ticked.

## Requirements

This child implements parent AC-7 and AC-8 and preserves AC-3 through AC-6.

1. A selected candidate can be checked, confirmed with attributable Unlimited evidence, saved, and listed after process restart.
2. A prior session must be reconciled before another registration or attachment. Uncertainty retains its container and private recovery state.
3. Cancellation, process termination, and ambiguous operation results cannot cause a replacement attachment or mistaken cleanup success.
4. Host registration, routing, mounts, and network namespace are never used by the backend. The host client may serve only as an independent read only observer during development experiments.

## Runtime and configuration

Use a separate Go worker executable inside an image built by `scripts/build-validator-image.sh`. The Dockerfile pins the tested Debian arm64 base and official WARP package version and verifies the package repository signature. The build prints its immutable image ID. No package installation or image pull happens during a check.

The existing `validator` setting gains `docker`; its default remains `unsupported`. Add string settings `validator_image` and `validator_socket`, with matching check flags. A Docker image must be an immutable `sha256:` ID. The optional socket must be an absolute local Unix socket path. When absent, inspect the selected Docker context or Docker host setting and accept only a local Unix endpoint. Pin that endpoint for the entire session. Require a Linux arm64 daemon and an arm64 image with the expected worker protocol and WARP version labels.

These settings remain subject to existing private JSON parsing and explicit flag precedence. Help and version never inspect Docker. Read and crawl commands never invoke a validator.

`wpc check <candidate-id> --validator docker --validator-image sha256:...` retains the existing saved attempt output and exit semantics. Add `wpc validator recover` with the same validator settings to reconcile only, without creating a candidate attempt or a new registration. Automatic recovery also runs before every Docker check. Recovery outputs `cleanup=complete` or a fixed diagnostic and exit 1.

## Records and ownership

Existing SQLite attempts remain the public result record. No SQL migration is needed. Extend the validator input with the profile state directory, candidate ID, run ID, and attempt ID, supplied by the application after those records exist.

Keep the backend recovery directory at the platform default state directory plus `validator`, independent of a profile's explicit `--state-dir`. An account owned private directory and nonblocking advisory lock serialize all checks and recovery across profiles. The directory contains a single active session manifest and its lock. All files are regular, owned by the current account, private, and opened without following symlinks. Manifest replacement uses a private temporary file, file synchronization, atomic rename, and directory synchronization. Invalid state blocks work.

The manifest contains version, random session identity, immutable container name and eventual ID, Docker endpoint and daemon ID, image ID, originating profile and attempt IDs, creation time, and cleanup acknowledgement. It contains no candidate key. Record intent before container creation. A lost creation acknowledgement is reconciled by the recorded unique name and matching session label. Never adopt or delete a container with a different identity.

Inspect all containers bearing the backend's fixed label before a new session. An untracked container blocks new work, including when someone removed or relocated the manifest. A different Docker daemon cannot silently replace one with a pending session.

Inside the container, the worker owns a second private journal and advisory lock. It receives the key and request identity through standard input, never a host argument, environment variable, image layer, or label. The vendor CLI requires the license in its argument list inside this isolated container. The worker journal and vendor logs can contain secrets and never reach routine output. Preserve a private copy of vendor registration state before deletion so uncertainty retains recovery material.

## Worker lifecycle

The container entry point starts only the WARP daemon. Registration starts only when the worker receives an explicit check request. No tunnel connection command is issued. Containers have ordinary capabilities, no mounts, no host network, no Docker socket, no automatic removal, and bounded memory, CPU, and process counts.

The host first initializes the worker journal through a separate operation that cannot register or attach. After the worker acknowledges durable `prepared` state, the host records submission intent and issues the check command without resending the key. Recovery can discard a container before submission, or reconcile its prepared journal after submission. Worker ownership and phase validation prevent a delayed check from starting after recovery has already cleaned that session.

Journal phases are `prepared`, `registration_pending`, `registered`, `attachment_pending`, `observed`, `cleanup_pending`, and `cleaned`. Persist each pending phase before the corresponding remote command. Store the registration and device identities as soon as available. Persist normalized evidence before cleanup, but do not publish confirmation until cleanup is acknowledged.

Run the tested commands through structured argument arrays and capture bounded output. Each vendor operation has a 60 second deadline; active observation has a four minute budget and cleanup a separate 60 second budget. A failed or timed out mutation is uncertain, not proof it never reached Cloudflare. No automatic registration or attachment retry occurs.

After successful registration, explicitly disconnect and capture its identity. Apply the candidate once. Poll registration information for up to ten seconds, without reapplying, until the returned license matches the requested key. Record the observation clock in UTC. A match with lowercase JSON account type `unlimited` maps to the existing typed evidence value `Unlimited`. A matching Free account remains inconclusive with reason `not_unlimited`; this version does not infer definitive provider rejection semantics.

Map device capacity to `device_limit`, provider throttling to `rate_limited`, unmatched identity to `validation_unattributed`, and unknown provider responses to `validation_provider_error`. None becomes `invalid`. Never echo vendor error text. Missing Docker, incompatible versions, unsafe container settings, and inaccessible recovery state have distinct fixed diagnostics.

The integrated experiment observed a generic CLI error for a registration HTTP 429. In this pinned vendor version, the private daemon log records the same request ID with `ApiFailure { code: 429` and `MaxRateLimitAttemptsReached(0)`. A fresh container with exactly one registration request, exactly one failure event followed by exactly one matching terminal event, and `MissingRegistration` supplies explicit creation rejection evidence. That narrow case can close without deletion and returns `rate_limited`; recovery can use the same retained events. Multiple requests, additional failure or completion events, an incomplete log, any other error, or a registration still present cannot use this exception. There is no automatic retry or change of network identity to evade the limit.

Cleanup persists `cleanup_pending`, retains vendor state, invokes registration deletion, and requires both a successful remote acknowledgement from the tested CLI and a subsequent `MissingRegistration` response. Persist `cleaned` durably before reporting success. The host persists that receipt before removing the exact owned container; only then may it clear its manifest. A lost removal acknowledgement is harmless when a durable cleanup receipt already exists.

On host cancellation, send a bounded cancellation request to the worker and retain the manifest. A surviving worker can finish cleanup independently. A later invocation first asks the existing worker to reconcile. It may wait for the current worker lock; it never starts a second attachment. If the worker died after registration or attachment intent, recovery inspects the same registration and performs cleanup only. It never resumes attachment.

Missing registration after an uncertain creation or deletion is not by itself remote cleanup evidence. Missing container state without a saved cleanup receipt, mismatched registration identity, a failed deletion, and an uncertain deletion acknowledgement leave the session blocked. Retain state and return `validation_cleanup_pending`; no override that merely discards state is provided. An operator may investigate retained state, but absence must not be invented. Interruption tests include both successful reconciliation and this deliberate blocked outcome.

## Application integration

The application creates its normal run and attempt before invoking the backend. Store recovery can mark an interrupted local attempt inconclusive, but cannot clear the separate remote session. Network and Docker operations remain outside SQL transactions. A new check reconciles any old session under the shared validator lock before beginning its own remote work.

Return a confirmation only when cleanup succeeded. Cleanup uncertainty produces an inconclusive attempt and retains private observed evidence for investigation. Existing definitive evidence remains untouched. A successful result that is lost before its SQLite commit is not silently recreated as a second remote operation within that invocation. A later deliberate check is a new attempt after reconciliation.

## Value sources

| Value | Source |
| --- | --- |
| Candidate and attempt identity | Existing store records supplied by the application |
| Worker version and image | Tested WARP version constant and explicit immutable image ID |
| Docker endpoint and daemon identity | Local endpoint selection followed by Docker inspection |
| Session and container identity | Cryptographic random local identity recorded before creation |
| Account evidence | JSON registration response whose license matches the input |
| Evidence time | Worker UTC observation clock |
| Remote cleanup receipt | Successful tested deletion response, followed by missing local registration |
| Recovery action | Durable phase, registration identity, worker lock, and persisted cleanup receipt |

## Build plan

1. [x] Build and exercise the private journal and container worker, including interruption before attachment and during cleanup. Retain ambiguous sessions and prove that new attachments are blocked. Covers AC-7.
2. [x] Build the host Docker adapter, shared ownership, recovery, and configuration. Preserve the unsupported path. Covers AC-4 through AC-7.
3. [x] Wire saved attempt input and result handling, then prove a real selected check, saved outcome, and independent CLI read. Covers AC-3 and AC-8. Live matching Free and device limit outcomes passed; confirmation persistence passed with controlled responses. The fresh positive CLI check remains in the verification checklist.
4. [x] Run focused recovery and privacy tests, the repository gate, and both arm64 builds. Emit the feature verification checklist. Keep Verify it and Test it for their separate workflow passes.

## Build observations, 2026-10-11

The actual CLI fetched a new latest page with 20 posts, retained coverage, checked saved candidates through the real adapter, and read `not_unlimited`, `device_limit`, and `validation_provider_error` outcomes in a separate process. A real HTTP 429 supplied the narrow creation rejection fixture; the retained session was reconciled without another registration.

Live fault tests killed the checking process after attachment intent, after a matching account response, and after remote deletion acknowledgement before delivery. Recovery completed each retained session. Stopping the isolated container during cleanup also recovered by restarting that same container. A separate lost worker acknowledgement test retained uncertainty and rejected another check; the lab operator disposed of that test session only after inspecting its captured real deletion acknowledgement and missing registration response.

Focused tests passed on macOS and in a Linux arm64 container with networking disabled. The independent model review found orphan inventory, acknowledgement parsing, private evidence retention, and returned attempt consistency gaps; each was corrected and covered by a regression test. One observed running container used 63.18 MiB of its 512 MiB limit and 0.47 percent CPU. These are point observations, not performance guarantees.

The available Unlimited account now reports its device limit. The user chose to preserve the working host registration and leave a fresh positive run through the final CLI pending. No acceptance pass is claimed for that unexercised check.

## Rationale and limits

A Go worker keeps lifecycle logic in the project language and survives loss of the host Docker CLI connection. The private journal describes one external session, while SQLite retains application history. A persistent worker service or a general job framework would add lifecycle complexity without helping this serial slice.

An immutable local image makes runtime behavior reproducible and avoids unexpected downloads. It requires an explicit preparation step. Strict uncertainty handling can block checks until a remote state problem is resolved; the alternative would risk leaving subscription devices behind. Public shared accounts can become full or change entitlement at any time, so a confirmation is an observation rather than a reservation.
