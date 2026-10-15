# Background operation and diagnostics

The contract is [spec 0008](../../docs/specs/0008-background-operation-diagnostics/index.md), with separate [native service](../../docs/specs/0008-background-operation-diagnostics/0001-user-service.md) and [diagnostic](../../docs/specs/0008-background-operation-diagnostics/0002-operational-diagnostics.md) contracts. This package owns native control, private service records, observations, and logs. Cobra parsing and rendering stay in `internal/cli/service.go`, scheduling stays in `internal/app`, and process exits and the managed watchdog stay in `cmd/wpc`.

## Account and ownership

[manifest.go](manifest.go) admits Linux amd64 and arm64 through the effective account database. Caller `HOME` is never account authority. The fixed slot is `warp-plus-connector.service`; service records live under the account home at `.local/state/warp-plus-connector/service`, independently of profile state overrides. Installation binds canonical executable, configuration, state, and optional Docker paths.

[files.go](files.go) enforces private directories and files, strict JSON fields and versions, and separate control and runner lifetime locks. Keep the lifetime lock until entered work and diagnostic writes finish. Do not unlink lock files, infer ownership from a retained PID, change lingering, escalate privileges, or use the host WARP registration.

## Native controls

[control.go](control.go) records operation phases before native effects and reconciles repeated requests. Install enables the next login without starting work or creating candidate state. Publish only complete synchronized definition bytes, atomically without replacing an existing file. Unknown acknowledgements retain changed state and operation uncertainty.

[native.go](native.go) validates definition bytes and effective native settings before control. Reporting a running start requires matching installation and invocation observations, native PID, and lock owner. Stop has one 80 second client budget; restart starts only after proven stop. Uninstall requires stopped ownership, refuses foreign replacements, and retains state, observations, logs, and a removal receipt for repeatability.

## Managed execution

[run.go](run.go) acquires lifetime ownership and rereads the ready manifest before calling the existing application runner. [publisher.go](publisher.go) bounds observation acknowledgement to five seconds while retaining any entered write until it finishes. Heartbeats run every fifteen seconds. Diagnostic failure cancels work; a working observation sink still records startup log failure.

The application shares one 60 second remote cleanup allowance across both lanes, then at most five seconds for local finalization, capped at 65 seconds from stop. The native manager and managed watchdog use 75 seconds. Another signal or stop cannot reset these windows. Forced termination or container disappearance never establishes remote cleanup.

## Read surfaces

[status.go](status.go) reports installation, manager, runner, and work independently, with availability and observation times. Work comes from `GetOperationalWork` in one five second read snapshot. Unreadable components have null data; installation identity changes invalidate all four components. An observation older than 60 seconds is stale; a terminal stopped observation does not fail doctor solely because of age.

[doctor.go](doctor.go) uses a shared twenty second budget and five second native and Docker probes. It performs local reads without repair, recovery, registration, or attachment. Preserve private file access codes and guidance, independently proven saved failures when ownership is unknown, and `retry_doctor` for shared budget expiry. Status report delivery can succeed for an unhealthy service; doctor returns failure after delivering a report containing failed checks.

[overview.go](overview.go) implements the service observations for [spec 0010](../../docs/specs/0010-terminal-foundation-overview/index.md) without opening the service's bound collection. `OverviewStatus` checks installation identity before and after independent manager and runner reads. Missing, changed, or unreadable identity discards all service components; a verified identity can preserve independent successes despite manager failure. `CompareOverviewCollection` compares canonical selected and bound paths without creating them. Cross refresh cache identity belongs to the terminal session, and these reads perform no service controls or Docker probes.

## Logs and privacy

[Spec 0009](../../docs/specs/0009-readable-development-logs/index.md) extends presentation through `internal/cli/service_logs.go` and replaces managed slog capture with `diagnostic.NewCapture` in [run.go](run.go). Validate every transport event, then adapt only `work_diagnostic` run IDs and codes to the existing archive catalogue. The archive remains authoritative for timestamps, sequence, and identities; presentation flags never alter saved capture or schema version 1.

A capture failure latches the archive failure, cancels managed work, and retains `service_log_failed` through finalization. Display filters run after the latest record selection, preserve original timestamps and continuity notices, and leave JSON complete. A failed display consumer stops only its reader.

[logs.go](logs.go) keeps current plus four archives, each at most 4 MiB, with complete records no larger than 16 KiB. Event fields and count names follow a closed catalogue; inapplicable zero, empty, or null fields are invalid. No raw vendor output, keys, credentials, or arbitrary error chains belong in records. Readers can ignore only an incomplete current tail; complete corrupt records remain errors.

Read snapshots capture identities and byte bounds and retry rotation at most three times within five seconds, without partial delivery. [follow.go](follow.go) emits a finite snapshot, later entries, and explicit gaps. Deduplication retains only the current archive view and last delivered position. Cancellation or a failed consumer ends delivery immediately, including after a gap callback, without stopping the service.

## Verification

You can run `rtk proxy go test -race ./internal/service ./internal/app ./internal/store ./internal/cli ./cmd/wpc` for focused coverage, or the root repository gate. Tests use private directories and controlled external boundaries. Native login, supervision, forced stop, and real entitlement require their separate runtime evidence; fixtures and compilation do not replace it. New provider attachment requires explicit authorization.

_Drafted by /sync from the introducing change, worth a quick human pass._
