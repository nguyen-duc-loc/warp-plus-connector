# Background operation and diagnostics rationale

## Context

This is scope feature 9 on the existing Go and Cobra CLI for macOS and Linux arm64. Foreground scheduling, persisted eligibility, single writer ownership, and shared validator recovery are already implemented. You need unattended operation and enough local evidence to distinguish an idle runner, stopped process, temporary validator wait, and permanent setup failure.

Your project uses Tracer Bullet delivery and a Beta workflow. The service must preserve completed work and the existing cleanup budget. Configuration and diagnostics must remain useful when SQLite or Docker is unavailable. The product remains a personal local tool with no remote control service.

## Options considered

### Example native service files

You install and operate launchd or systemd files yourself. This has the least application code and exposes the native tools directly. It leaves path binding, consistent status, configuration validation, and safe removal as manual work, which does not provide the built in experience you selected.

### Built in native service adapters

The CLI owns installation metadata and issues bounded native control commands around the existing runner. You gain one operating interface and clear installation identity. The cost is maintaining two native integrations and being explicit about differences in restart and session behavior. This is the selected option.

### General service management library

A library could reduce platform boilerplate and help if more operating systems were planned. It would not remove this feature's special handling for private metadata, managed fatal exits, enabled but stopped services, bounded cleanup, and source diagnostics. No dependency is added merely to wrap two command adapters.

### Detached application daemon

The application could detach itself and manage PID files or its own supervisor. It would avoid native installation but duplicate supervision, complicate session shutdown, and need a new process control protocol. This conflicts with the established foreground runner and native user manager boundary.

## Rationale

You chose built in commands, one service per account, and login startup. Installation enables later startup but the first run remains a separate command. Stop preserves future startup; disable changes only future startup. Differing installation bindings require stopped uninstall, and uninstall retains your data and diagnostics.

The selected data model keeps a private installation record and the latest runner observation outside SQLite. This makes permanent startup failures inspectable without creating a second execution history. Fifteen second updates and a 60 second stale boundary give useful operational timing. A separate service lifetime lock supplies ownership evidence; a PID file alone cannot do that.

You chose a 20 MiB log archive, recent inspection with optional following, and local diagnostic checks that include Docker readiness. Status combines full collection totals with latest saved run results. Its successful report exits 0; doctor uses a failed check outcome to return 1. Temporary failures remain inside the scheduler, while handled fatal returns stop native retry loops.

Native service managers differ. The design preserves a common user contract while exposing uncertain or unsupported observations. macOS throttles launch frequency; systemd can delay a restart after exit. Native manager output is never treated as candidate evidence. The managed entry point's successful native exit after a handled failure prevents repeated launches, while its actual failure remains in the private observation and doctor report.

The 75 second native stop allowance exceeds the runner's existing 65 second cleanup and finalization budget. This permits bounded process termination without pretending that forced termination completed remote cleanup. The next owner still reconciles the authoritative recovery records.

## Discovery evidence

Preflight found 106 Go source files and a clean working tree. Git fetch succeeded, but no `origin/main` tracking ref was available, so the prescribed behind count could not run. The topic matches planned scope feature 9 and is adjacent to accepted specs 0001 and 0007. You explicitly chose a new spec 0008.

Targeted reads covered the CLI process entry, current runner, schedule reads, configuration resolution, and Docker executable and endpoint selection. Existing `schedule show` intentionally reports saved facts rather than process liveness. The existing Docker adapter invokes a PATH selected executable, so managed execution needs an explicit executable binding while retaining ordinary foreground behavior.

Native lifecycle and stop policy documentation was checked during discovery, including the installed macOS manuals. Those manuals explicitly warn that `launchctl print` output is not an API. The design therefore avoids using its human output as a stable status schema. Exact supported platform behavior still needs the runtime checks in the build plan.

You selected no References section. No additional community skill or MCP discovery was requested. No service was installed, process started, candidate checked, or application code changed during this design.

## Independent review

You requested an independent model critique and authorized all eleven recommended fixes on 2026-10-11. The review supported native supervision and the selected diagnostic model. The revised draft closes these gaps:

1. Runner admission revalidates under the lifetime lock, and removal holds that lock throughout its mutations.
2. Native identity checks cover effective Linux settings and reject unverified macOS registrations. An inactive macOS job may require explicit native inspection and removal; the CLI does not treat its label as proof.
3. Every durable control operation has a transition and retry contract, including a retained removal receipt and initially unknown startup intent.
4. Readiness requires a new invocation relative to a captured baseline and matching native and lifetime owner samples.
5. Log events, count objects, and doctor condition outcomes are explicit contracts.
6. Rotation order and synchronization are fixed. Following reports unknown continuity across invocation boundaries instead of claiming no loss.
7. Effective account lookup locates the service independently of caller `HOME`.
8. Complete terminal aggregates remain visible alongside newer work.
9. Restart guarantees follow native failure classifications and exclude a promise to restart every signal exit.
10. A changed installation invalidates the affected status components using the existing completeness rule.
11. Observation writes have explicit deadlines and retained ownership for unresolved operations. Managed shutdown has an outer watchdog even when no native stop request initiated it.

The changes preserve the 20 MiB archive, 15 second heartbeat, 60 second stale boundary, existing scheduler and validator contracts, and absence of a database migration. You separately accepted the revised spec on 2026-10-11. The design is confirmed and linked to scope feature 9; implementation remains pending and the spec retains `Proposed` status.

## Linux uninstall correction, 2026-10-11

You approved the correction during development. systemd can load an existing unit file whenever it is referenced. It has no explicit unit unload operation, so absence from memory before file removal is not a durable checkpoint. Linux removal therefore disables startup and confirms a stopped unit, removes the owned definition, reloads the manager, and confirms absence before publishing the removal receipt. The added `manager_removed` phase retains a safe retry point after definition removal. macOS keeps its separate registration removal order.
