# User service

## Summary

You manage one native user service through `wpc service`. Installation binds it to explicit local paths, and its managed entry point runs the existing application coordinator. Native supervision handles unexpected process failure while saved work and validator recovery retain their existing owners.

## Decision

Use a Linux systemd user unit on amd64 and arm64. Service platform admission rejects other operating systems and architectures before manager access or file publication. The service runs as your current account. There is no privileged installer, shell wrapper, detached child, control socket, or new scheduling loop.

**Platform amendment, 2026-10-12:** launchd is outside supported operation and verification. Existing macOS code need not be deleted, but it adds no acceptance requirement. Existing Linux manifests and removal receipts retain schema version 1 and their current paths. A foreign manager value is refused, never converted.

## Feature design

### Location and identity

Use the fixed native name `warp-plus-connector.service` on both Linux architectures. An installation ID distinguishes successive installations of that account slot.

The service root is `~/.local/state/warp-plus-connector/service` on Linux. Here `~` means the home returned by the effective account lookup below, not caller `HOME`. The service root deliberately ignores profile `state_dir`, caller `HOME`, and `XDG_STATE_HOME`. Save its absolute canonical path and bind the native service name to the current effective UID.

Resolve the effective UID with `os.Geteuid()`. On Linux query `getent passwd UID`, resolving `getent` only in `/usr/bin` and `/bin`, and require exactly one passwd record with that numeric UID and an absolute home field. These are bounded account lookups through the host's account database; directory services may perform their own configured lookup. Do not substitute `HOME`, `USER`, `LOGNAME`, shell expansion, or `os.UserHomeDir()` on failure. Missing tools, ambiguous results, and lookup failure report `service_account_unavailable` before service mutation. No raw account output is logged. This account lookup preserves the build with CGO disabled and requires verification on both Linux targets.

The root contains `installation.json`, `observation.json`, `removal.json`, `control.lock`, `runner.lock`, and `logs/`. It can remain after uninstall. Never remove lock files while another process could hold them. The native definition lives at the install time configuration base's `systemd/user/warp-plus-connector.service` on Linux. Resolve that base from an absolute `XDG_CONFIG_HOME`, otherwise account home plus `.config`, and save the exact definition path. Linux preflight confirms the chosen directory belongs to the current user manager's unit search paths; otherwise installation reports `service_manager_unavailable` without publishing files.

### Installation manifest

`installation.json` uses schema version 1. All fields are required unless marked nullable. Reject duplicate or unknown fields, trailing data, files larger than 64 KiB, unknown versions, and invalid identity or path combinations.

| Field | Type and rule |
| --- | --- |
| `schema_version` | Integer, exactly 1 |
| `installation_id` | 32 lowercase hex characters from 128 random bits |
| `uid` | Nonnegative integer equal to the current effective UID |
| `manager` | Exactly `systemd` for supported Linux operation |
| `service_name` | Fixed native name above |
| `installed_at` | UTC timestamp, microsecond precision |
| `home_dir`, `service_dir`, `definition_path` | Absolute canonical paths |
| `executable_path`, `config_path`, `state_dir`, `validator_state_dir` | Absolute bound paths |
| `working_dir` | Absolute install working directory, used to resolve existing relative state settings consistently |
| `config_home`, `state_home` | Absolute resolved configuration and default state bases, including install time XDG choices |
| `docker_executable`, `docker_socket` | Nullable absolute paths, both present when validation was enabled at installation |
| `definition_sha256` | Lowercase SHA256 of the exact rendered native definition bytes |
| `autostart_enabled` | Nullable boolean, null only before initial native enablement is acknowledged; required boolean in `ready` |
| `operation` | `install`, `enable`, `disable`, or `uninstall` |
| `phase` | `prepared`, `definition_written`, `manager_applied`, `definition_removed`, `manager_removed`, or `ready`, subject to the operation table below |

These last three fields describe installation control, not runtime history. Transient start and stop outcomes come from the manager and observation. There is no growing control history table.

Resolve an existing executable symlink to a regular executable file at install time. Require that it is not writable by other accounts. Record its path, not a copy or a content hash, so replacing a binary at that path supports upgrades. The configuration file must pass the existing private configuration rules. Canonicalize existing state path ancestors without initializing state; retain the normalized remaining path if state does not yet exist. Reject overlap between the selected candidate state directory and the service root in either direction, except that the ordinary platform default state directory may contain its `service/` child. The service root itself can never be the candidate state directory.

Install and managed configuration resolution use the effective account home, saved configuration base, state base, and working directory. Install derives the bases with the existing platform rules but substitutes the authoritative account home before applying explicit XDG bases. It applies the file's settings and existing defaults, with no copied shell environment or runtime flags. Compare the resulting state and shared validator directories with the saved binding before opening either. For Docker, the saved executable and socket are authoritative. A nonempty socket in the file must match the saved socket. A changed binding reports `service_binding_changed` and needs a stopped uninstall and new install. A collection installation without Docker paths cannot later enable validation without reinstalling.

At install, resolve the Docker executable and local endpoint using the existing local endpoint rules only when validation is enabled. Context inspection is a local Docker CLI read. Save the resulting executable and socket paths. Do not require a running daemon, invoke `docker info`, pull an image, or create a container during install. The runner retains its existing delayed recovery when Docker is temporarily unavailable. Doctor supplies the optional readiness probe.

### Control ownership and publication

Control commands acquire `control.lock` without waiting. Contention is `service_busy`; access errors are distinct. Read commands take no control lock. The managed runner acquires `runner.lock` exclusively, then rereads and validates the manifest and installation ID before publishing any observation, loading runtime configuration, or doing work. A manifest read before locking is only a path hint and cannot authorize admission. The lifetime lock remains held until the final observation and all entered operations close. State and validator ownership retain their existing independent locks.

Use a whole file POSIX record lock for `runner.lock`, with `F_SETLK` for exclusive acquisition and `F_GETLK` for a nonmutating owner PID query. Keep its descriptor private, close it on exec, and never open and close a second descriptor for that inode in the owning process, since process associated record locks can be released by such a close. Preserve existing lock implementations elsewhere. Readers can query the existing file without creating it. This supplies an owner PID rather than assuming that a held lock belongs to the PID in a record.

Uninstall holds `control.lock`, acquires `runner.lock` exclusively without waiting, rereads the manifest, then retains both locks throughout removal. A live owner returns `service_running`. An already launched runner waiting to enter either loses the nonblocking lifetime acquisition or, after acquiring it later, rejects the removal phase or absent manifest. Other controls never hold `runner.lock` while requesting native start or waiting for readiness. The lock order for a process needing both is control then runner; runners never acquire the control lock. This avoids both uninstall admission races and start deadlocks.

Install first performs argument, platform, configuration, path, native manager, and conflict checks. A native definition or registered native service without a matching manifest is a conflict, even when its name resembles this project. A ready matching installation is an unchanged success only if the input config path, bound paths, and native definition bytes match. Different settings require stopped uninstall. Changed settings inside the same file are read at next start, subject to binding checks.

After preflight, installation saves a `prepared` manifest with `autostart_enabled=null` before publishing the native definition without replacement. It synchronizes the file and its directory, saves `definition_written`, enables native login startup, then saves `manager_applied` with `autostart_enabled=true` and finally `ready`. It never bootstraps or starts the service during install. There is no transaction spanning native commands and files. Every step is inspected before retry; a matching already completed step is adopted, never repeated blindly. Unknown native results leave an incomplete manifest and a stable error. A rename or directory synchronization failure stops further service mutations in that invocation.

An exact repeated install can finish an incomplete installation with the same ID and definition. It cannot start a runner while `phase != ready`. When an enable, disable, or uninstall operation is incomplete, repeating that same action reconciles its steps before making further changes. A different mutation reports `service_operation_incomplete`. Doctor explains the pending action. Foreign bytes, IDs, paths, permissions, or owners always block reconciliation.

Enable and disable persist operation intent before their native action, and acknowledge the new `autostart_enabled` value only after success. Disabling never sends a stop signal. Start requires acknowledged enabled startup; otherwise it returns `service_disabled` and suggests enable. This preserves explicit disabled startup intent. Start is unchanged success when the matching runner already holds its lifetime lock.

Stop targets the identity checked native service, never a PID read from disk. It keeps `autostart_enabled` unchanged. Restart completes stop before attempting start. Uninstall requires no live runner and no stopping native job, persists removal intent, and disables future startup. It removes the matching definition, reloads the manager, and then confirms that the unit is absent. Only acknowledged native and definition removal permits manifest deletion. Logs, observation, removal receipt, lock files, configuration, candidate state, and validator state remain. Missing managed pieces from a previously started uninstall are safe to reconcile; altered pieces are not deleted.

### Durable control transitions

Each manifest update is an atomic private replacement followed by directory synchronization. A completed step means both its effect and its acknowledgement are known. Before retry, validate the existing identity and inspect the effect specified here. A native command timeout stays at the prior step; it is never interpreted as rollback.

| Operation | Durable sequence | Reconciliation |
| --- | --- | --- |
| `install` | `prepared` with null startup intent; `definition_written`; `manager_applied` with true; `ready` | Adopt exact definition bytes already published. Reject foreign bytes or registration. Reissue only the same enable intent after identity checks when native enablement cannot be observed; enable must not start work. |
| `enable` | `prepared` preserving prior startup value; `manager_applied` with true; `ready` | Require matching definition and acceptable loaded identity. Observe enabled state where supported, otherwise safely reissue enable. Never start or stop a process. |
| `disable` | `prepared` preserving prior startup value; `manager_applied` with false; `ready` | Same checks and same action retry, without a stop. The last acknowledged startup value stays visible until acknowledgement of false. |
| `uninstall` on Linux | `prepared` under both locks; `manager_applied` with false after disabling and confirming no active process or pending job; remove matching definition and save `definition_removed`; reload the user manager, confirm unit absence, and save `manager_removed`; synchronized removal receipt; remove manifest and synchronize its directory | Before definition removal require the matching effective unit identity, disabled startup, and no active process or job. A unit remains loadable while its definition exists. If deletion succeeded while the saved phase is still `manager_applied`, adopt the missing definition under both locks, synchronize its parent directory, and save `definition_removed`. After `definition_removed`, require the definition to remain absent, reload, then require `LoadState=not-found`, no main PID, and no pending job. A newly present definition or different effective unit is a conflict. Repeat an uncertain reload only with these file checks; publish the receipt only after `manager_removed`. |

`removal.json` is one private schema version 1 receipt, at most 64 KiB, retaining `installation_id`, `uid`, `manager`, `service_name`, `definition_path`, `definition_sha256`, and `definition_removed_at`. Its publication follows acknowledged native and definition removal and precedes manifest deletion. It certifies those removals, not final manifest deletion. Retain it after completion. A later completed uninstall replaces it with the newer installation's receipt; it is not a growing history.

When the manifest is absent and a valid removal receipt exists, repeated uninstall checks that the recorded definition and native registration remain absent, synchronizes the service directory, and returns unchanged success with that installation ID. A newly present definition or registration is a conflict and is never removed using the old receipt. A receipt with a different ID never authorizes deletion of a present manifest. When both manifest and receipt are absent, uninstall is unchanged success with `installation_id=none` only if no fixed native definition or registration exists. Unknown native absence prevents success. These rules cover losing the acknowledgement after final manifest deletion. Install can create a fresh ID after completed removal and does not erase the old receipt or diagnostics.

### Loaded identity

Before a native mutation, verify the saved definition bytes and the loaded identity separately. On Linux require `FragmentPath` to equal the canonical saved path, `DropInPaths` to be empty, and the effective `ExecStart` executable and argument vector to match the installation ID and saved executable. Verify `Type`, `Restart`, `RestartUSec`, `TimeoutStopUSec`, `KillMode`, `KillSignal`, `SendSIGKILL`, `UMask`, `WorkingDirectory`, `Environment`, and `EnvironmentFiles` against the rendered policy. Reject unexpected environment files, extra execution commands, runtime property overrides, or pending reload as `service_definition_changed`. A required reload is allowed only after validating owned definition bytes, then the effective checks run again. The Linux uninstall exception uses acknowledged definition removal and checks for absence or a foreign replacement before reload; its subsequent check requires unit absence instead of the removed effective definition. No same name foreign unit is stopped or replaced.

### Native policies

| Concern | Linux amd64 and arm64 |
| --- | --- |
| Execution target | Current systemd user manager |
| Program | Absolute `wpc` path with `service exec --installation-id ID` arguments in `ExecStart` |
| Login activation | `[Install] WantedBy=default.target` and user enablement |
| Restart | `Type=exec`, `Restart=on-failure`, `RestartSec=30s` |
| Stop | `TimeoutStopSec=75s`, `KillMode=mixed`, SIGTERM then native escalation |
| Permissions | `UMask=0077` |
| Working directory | Saved working directory |

Use proper systemd argument escaping, including literal percent and dollar characters. Reject NUL and line breaks in installation paths instead of trying to encode them. Supply no shell command string. Bind the service root through the saved absolute home environment and fixed platform root rule. Preserve only the explicit home and XDG bases needed by configuration resolution. Do not copy credentials, `DOCKER_HOST`, `DOCKER_CONTEXT`, arbitrary shell variables, or a shell startup script. The validator uses the saved absolute Docker executable directly.

For Linux, installation reloads the user manager and enables the exact installed unit without `--now`. Use explicit user commands for start, stop, enable, disable, and reload. Restart uses the shared stop confirmation before start. Stop does not remove enablement. Disable does not use `--now`. No command calls `loginctl enable-linger`. Existing host lingering and multiple login sessions may keep the user manager alive after a logout; the CLI reports that host behavior and does not promise shutdown at every logout.

The policy paces retries using native systemd semantics, waiting 30 seconds after a failed exit. Do not add an application restart loop. A normal managed fatal return writes a failed observation and exits 0 on both Linux architectures so native restart does not turn it into an error loop. Its application failure remains explicit in status and doctor; native exit 0 is not a health claim. Unexpected panic and native failure classifications remain eligible for restart. Do not promise restart after every signal: a handled SIGINT or SIGTERM exits cleanly, and systemd also treats some signal exits as clean. Explicit native stop suppresses restart. Verify a nonzero crash and SIGKILL separately from graceful signals.

The private `service exec` command is hidden from normal help. It accepts only the installation ID, obtains the lifetime lock, rereads and requires a matching ready private manifest, and installs diagnostic sinks before loading runtime configuration. It calls the same runner as the public foreground command. Public `run` retains exit 1 for errors and 130 or 143 for cancellation. Only the private managed entry maps handled failures and graceful signals to native exit 0, after attempting to publish their actual reason. `cmd/wpc` remains responsible for that mapping.

Known managed errors include any returned configuration, filesystem, store, identity, journal, output, or observation error. They do not retry at the process layer. `state_busy` is also a visible stopped failure, not a launch loop. Temporary source errors and validator waits already handled inside the runner never reach this mapping. A native failure to execute a missing binary cannot write an application observation; report the native failure or an unavailable observation, without inventing a permanent error latch that never ran.

### Control bounds and observations

Native query commands have a five second deadline and a 1 MiB combined output cap. Mutations have a ten second request budget, except stopping and restart, which have an 80 second total stop budget plus a ten second start request budget. A killed or timed out control client does not imply the manager cancelled its job. Record uncertainty and inspect before any next action. Native raw output is never relayed to ordinary diagnostics.

Before requesting start, capture the latest valid invocation ID (or absence), the lifetime owner PID, and manager state. If an active matching owner already exists, do not launch: return unchanged running only after the identity checks and readiness observation agree. Otherwise return unchanged `starting` for an identified startup owner. Conflicting or unavailable identity is an error.

After a new start is accepted, wait at most ten seconds for an observation with the same installation ID and an invocation ID different from the captured baseline. Never accept the prior invocation's `running` or `failed` record. For live readiness, read the candidate observation, sample native PID and lifetime owner PID, then reread the observation. Both reads must retain the invocation and PID, and the native and lock owner PIDs must equal it. Readiness means phase `running` or `clock_paused`, after writer ownership and initial schedule preparation; Docker readiness is separate. A new terminal failed observation with a released lifetime lock and no conflicting native process returns `service_start_failed`. A restart has the same rule after confirmed stop and a newly captured baseline. A native automatic restart during polling can supply a still newer matching invocation, but mixed samples never establish success. If samples remain inconclusive, known native acceptance returns `state=starting`; an unknown request result remains an error.

Confirm stop using both a completed native stop request and release of the existing lifetime lock. Probe that lock without creating it. If the file is missing or inaccessible, keep the ownership observation unavailable until the native adapter independently confirms no active process. A released lock alone is insufficient while a native start or stop job remains pending. Repeated stop is unchanged success only when stopped state is established. Report `service_stop_timeout` after the total deadline; restart and uninstall do not proceed from that result.

Native manager status remains an observation. On Linux use bounded `systemctl --user show` property reads for load state, active and substate, main PID, exit result, pending job, and unit fragment path. Read enablement separately and timestamp it. Unavailable or unrecognized observations stay unknown. File identity checks and the service lifetime lock remain available from another session.

### Shutdown and failure

Cancellation stops admission and enters the existing shared cleanup window. The observation becomes `stopping`. Neither a second signal nor another control invocation resets the cleanup clock. The service manager allows 75 seconds before force. The CLI never reports remote cleanup success from native stopped state, lock release, or container disappearance.

The managed process also starts one 75 second monotonic watchdog on its first internal fatal cancellation or handled shutdown signal. `cmd/wpc` terminates the managed process at that deadline if entered local operations still prevent return, using the managed handled failure exit classification and no invented terminal record. This is needed because a diagnostic failure can initiate shutdown without a native stop request, so native stop timeout alone would not bound it. It is not a restart loop and does not change public foreground command behavior. Remote cleanup and local finalization still use their existing 65 second shared budget.

A diagnostic failure requests cancellation once. The failed sink receives no further writes. Other safe sinks may record a stable failure; SQLite and remote cleanup retain their existing acknowledgement rules. Failure to save the final observation leaves the earlier one, which becomes stale. Crash restart acquires existing owners, recovers interrupted work, and respects shared cooldowns before any new attachment.

### Value sourcing and verification

Manifest values come from the install invocation, local account, effective config, and existing Docker endpoint resolver. Native bytes derive solely from those values and the fixed policy table. Runtime settings derive from the chosen file under the saved resolution context. Native exit classification derives from the managed result, never from parsing a log.

Prove install without work, explicit start, login activation, temporary stop, independent disable, disabled start refusal, stopped uninstall, and same input retry on both Linux architectures. Inject failures around every manifest, definition, and native action boundary. Prove writer exclusion, graceful restart, forced termination, changed paths, and preserved recovery obligations. These scenarios cover AC-1 through AC-8, AC-14, AC-15, and AC-16.

## Rationale

Explicit native adapters make the lifecycle contract visible and avoid a second process supervisor. Private metadata remains readable when configuration or SQLite is broken. The cost is verifying one native behavior on two architectures and preserving uncertainty when observations are unavailable.
