# Validator status

The default adapter is `unsupported`. It records an inconclusive attempt without network access, subprocesses, Docker access, or registration changes. The optional `docker` adapter checks selected authorized candidates using the official WARP client in an isolated Linux container matching the executable and daemon architecture. Linux amd64 and arm64 are admitted; native amd64 provider evidence remains pending. Earlier definitive evidence stays intact after any inconclusive attempt.

The assessment on 2026-10-10 found a local Colima endpoint under the account's `.colima/default/docker.sock`. Docker client `29.9.0` and server `29.5.2` reported macOS arm64 and Linux arm64 respectively. This establishes a working local container runtime. It does not establish WARP compatibility, isolation, entitlement, or cleanup. A separate Debian container ran application and store probes without external networking or WARP registration.

Later experiments on 2026-10-10 established matching Unlimited evidence with official WARP `2026.8.2100.0`. All twenty temporary registrations received successful remote deletion acknowledgements. An independent authorized host registration subsequently confirmed that the successful experiment device was absent from the same subscription. The host served only as a read only observer. The [backend contract](../../docs/specs/0003-source-crawl-validation/0003-docker-validator.md) records the resulting implementation and recovery requirements.

The installed `warp-cli registration` help describes `show` as inspecting the current registration and `license` as attaching that registration to a different account. Plan evidence therefore needs attribution to the submitted candidate in an isolated registration. The application must never use your host registration as a fallback.

The controlled experiment established normal slot cleanup through independent device membership inspection. The backend uses the tested remote deletion acknowledgement and durable receipt, and retains uncertainty rather than treating container removal as remote cleanup. Runtime interruption checks separately exercise recovery and blocking of further attachments.

## Runtime ownership

Build the image with `sh scripts/build-validator-image.sh` and pass its immutable ID through `--validator-image` or private JSON configuration. The image contains a Go worker and the tested official package. Checks never install software or pull images. Only local Unix Docker endpoints and Linux images matching the native Linux amd64 or arm64 executable and daemon are accepted. Preparation detects the daemon architecture or accepts one matching `amd64` or `arm64` argument. Existing arm64 image identities and the shared registry path remain valid.

The shared host journal lives under the platform default state directory at `validator/session.json`, with `owner.lock` retaining one owner across profile overrides. It records up to four sessions, each with its Docker endpoint, daemon identity, image, exact container, originating attempt, submitted operation, and cleanup receipt. The version 2 registry remains present when empty. Candidate keys stay out of host process arguments and this manifest.

The worker receives its key through standard input and retains a private journal at `/var/lib/wpc/session.json` inside its container. The vendor license argument exists only inside that isolated runtime. Vendor logs and a registration backup remain private there. The worker initializes its journal before a separately admitted registration operation can create remote state. Attachment requires its own host admission. Status and cancellation remain accessible while the operation lock is held.

The host accepts a confirmation only after matching input attribution, explicit deletion acknowledgement, and a durable cleaned receipt. A failed, timed out, or interrupted check retains its session. `validator recover` and the next Docker check reconcile every retained session before any new attachment. Neither recovery path creates a registration or reapplies a license.

Missing local registration without a receipt is uncertain. A narrowly measured exception handles an explicit HTTP 429 rejection of the sole creation request, provided the retained log has exactly the matching failure and terminal events and local registration is absent. It reports `validation_rate_limited` and performs no automatic retry. Unknown cases stay blocked; there is no command that discards uncertainty merely to continue.

Protocol 2 separates registration and attachment so the host can serialize vendor mutations across overlapping observations and cleanup. One owner spans the batch and retry waits. Registry replacement failures stop writes in that invocation, preserving the actual on disk journal for the next recovery. The batch implementation and controlled checks do not replace the separately authorized live acceptance in [spec 0005](../../docs/specs/0005-concurrent-checks-bounded-retries/verify.md).

## Package availability update (2026-10-13)

The build and runtime now pin WARP `2026.8.2110.0`. The official Bookworm indexes for amd64 and arm64 listed this version and no longer listed `2026.8.2100.0` when inspected. The image labels, worker readiness check, installed package inspection, and build and doctor fixtures use the new pin. Worker protocol remains 2.

The real Linux arm64 image build succeeded with ID `sha256:ded134517782b4e4a6e195949d9487546aa12d6380d645f34d52a2939f332743`. With networking disabled and synthetic input, the worker passed readiness, initialization, cancellation, and recovery from `prepared` to `cleaned`. The smoke container was removed. No registration or license attachment was attempted. Formatting, vet, race tests, and lint passed on the macOS arm64 development host. An amd64 image build and live entitlement and remote deletion verification remain outstanding for this release.

The pinned version statements in the Docker backend spec and spec 0005 still describe `2026.8.2100.0` and need reconciliation through `/architect`. Earlier live verification records remain evidence for that earlier release only.
