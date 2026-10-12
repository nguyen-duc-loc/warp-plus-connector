# Background operation and diagnostics verification

This is the acceptance procedure for the proposed feature. No item is marked passed during architecture. Record the operating system, architecture, native manager version where available, executable version, commands, outcomes, and evidence location when implementing it. Use disposable private service and state fixtures on isolated test accounts where changing login startup would affect your working account.

## Native service path

1. On Linux amd64 with a real systemd user manager, install a valid collection configuration. Prove no database, source request, or runner starts during installation. Explicitly start, observe one scheduled crawl, inspect status from another process, stop, and inspect saved work. Covers AC-1, AC-2, AC-3, AC-6, AC-8, AC-15.
2. Repeat under a real Linux arm64 systemd user manager. Verify native unit validation and the installed definition's effective settings. Record the user manager's session and lingering state without changing it. Covers AC-1, AC-2, AC-3, AC-16.
3. Prove activation at the next native login, stop without disabling future startup, disable without stopping the current runner, enabled start after a stop, and disabled start refusal. Prove stopped uninstall retains data and diagnostics. Covers AC-2, AC-4, AC-16.
4. Repeat identical install and control commands. Reject different config bindings, foreign definitions, absent managers, wrong ownership, broad permissions, managed symlinks, malformed metadata, and unsupported versions. Covers AC-3, AC-4, AC-14.
5. Change caller `HOME` and prove the effective account lookup still locates the same installation. Exercise missing or ambiguous native account lookup. Load a different same name job, add a Linux drop in or runtime override, and present a manifest with a foreign manager value. Require refusal before a destructive native action, with the documented recovery guidance. Covers AC-1, AC-3, AC-4, AC-14.

## Process failure and recovery

1. Crash the managed process with controlled local work pending. Observe native restart pacing, a new invocation ID, recovered local work, and retained remote recovery obligations. Do not infer remote cleanup from process or container disappearance. Covers AC-5, AC-6, AC-7, AC-16.
2. Introduce invalid configuration, a changed state path, an absent executable, a writer conflict, a permanent validator identity error, and failed diagnostic writes. Distinguish errors the application records from failures before it can execute. Prove handled fatal errors do not create a native restart loop. Covers AC-3, AC-5, AC-6, AC-14.
3. Hold controlled cleanup open, stop while crawl and validation are active, and send another stop request. Prove one shared cleanup budget, no timer reset, 75 second native escalation, and no new work after cancellation. Verify normal completion, forced termination, CLI timeout, and the next start's recovery separately. Covers AC-6, AC-7, AC-14, AC-16.
4. Interrupt each installation, enablement, disablement, and removal boundary. Inject rename and directory synchronization uncertainty, native request timeout, and output failure after an acknowledged operation. Repeat the same action and prove truthful reconciliation. Covers AC-4, AC-14, AC-15.
5. Pause runner admission between its first manifest hint and lifetime acquisition while uninstall proceeds. Prove it cannot start after removal. Pause removal after manifest deletion but before directory acknowledgement, then retry using the receipt. A newly present foreign definition or registration must block that retry. Covers AC-4, AC-6, AC-14.
6. Retain an old running or failed observation while starting a new process, reuse a PID in a controlled adapter, and force an automatic restart during readiness polling. Require an identified new invocation with agreeing samples. Hold an observation filesystem operation beyond its deadline: cleanup must continue, ownership must remain held, and the managed watchdog must bound an internally initiated shutdown without a native stop request. Covers AC-5, AC-7, AC-8, AC-14.

## Status and local checks

1. Use the controlled 338 candidate collection with checked, unchecked, definitive, inconclusive, and active records. Compare totals, queue partitions, latest runs, sources, and text versus JSON while a second process commits more work. Prove a single database snapshot. Covers AC-9, AC-10, AC-11.
2. Inspect before installation, before first start, during startup, while idle, while validation waits, during clock regression, after a crash, and after uninstall. Distinguish missing, unavailable, stale, historical, and live observations. Test the exact 60 second stale boundary and future timestamps. Covers AC-8, AC-9, AC-10, AC-11.
3. Break configuration and database access separately and prove the other status components still report. Change installation identity during collection and require an incomplete report. Prove no migration or new state files from inspection. Covers AC-8, AC-10, AC-14.
4. Run doctor with Docker disabled, executable missing, daemon stopped, wrong daemon architecture, missing image, incompatible worker image, and valid local readiness. Trace native and Docker calls to prove no container creation, registration, source request, provider request, or repair. Covers AC-11, AC-13, AC-15.
5. Begin a new crawl and batch after failed terminal runs. Prove the complete prior sources, totals, and failure reasons remain present in the same work snapshot. Exercise every doctor condition table row, especially stale live observations versus old stopped observations, absent installation and logs, historical failures, and unverified native identity. Covers AC-9, AC-10, AC-11, AC-13.

## Logs and privacy

1. Produce more than 20 MiB of controlled diagnostic records. Inspect every rotation boundary and prove at most five files of 4 MiB each, complete JSON records, correct recent selection, and explicit retention metadata. Covers AC-12.
2. Follow logs through multiple rotations and process restarts. Slow the follower until data is removed, require a gap event, and prove no duplicate delivery of retained entries. Cancel the reader and break its output pipe without stopping the service. Covers AC-11, AC-12, AC-15.
3. Crash during rotation and truncate the final current record. Prove bounded recovery of only that tail, retained complete records, and rejection of malformed complete records or unsafe files. Covers AC-12, AC-14.
4. Place synthetic keys and credentials in controlled source and validator fixtures. Search service files, arguments, environment, observations, reports, logs, stdout, and stderr for disclosure. Preserve the explicit key output behavior of existing list and export commands. Covers AC-12, AC-13, AC-15.
5. Validate each event against its closed field and counts catalogue. Stop a follower while an entire intervening invocation rotates out, then resume in a later invocation and require a continuity gap. Crash after every rotation namespace step and verify allowed holes, correct order, synchronization failure behavior, and bounded deduplication. Covers AC-12, AC-14.

## Completion

Runtime proof precedes the separate test pass under the Beta workflow. Native supervision needs real user managers on both Linux architectures; mocked command tests and ordinary Docker containers do not close AC-16. Existing real provider behavior is reused where this feature does not alter it. Any new experiment that attaches a real candidate uses separately authorized keys and preserves host registration.

After runtime verification, run the meaningful regression suite, formatting, vet, race tests, lint, and Linux amd64 and Linux arm64 builds. Record unavailable target evidence as an unmet acceptance criterion rather than declaring the feature complete.

## Linux removal checkpoint correction

1. Stop and disable the owned unit, then query it while its definition still exists. A loaded inactive unit is expected and does not prevent removal. Covers AC-4 and AC-14.
2. Interrupt removal after definition deletion before its phase acknowledgement, after that acknowledgement before manager reload, and after reload before acknowledgement. Retry under both locks, confirm file absence and no foreign replacement, reload, and prove `LoadState=not-found`, no main PID, and no pending job before the receipt and manifest deletion. Covers AC-4, AC-6, AC-14, and AC-16.

## Platform transition

The 2026-10-12 amendment removes native macOS requirements. Existing Linux arm64 evidence may be reused for unchanged behavior when its exact commands and versions remain recorded; none closes the added amd64 checks. The sections above apply on each Linux architecture.

1. Complete the [platform checklist](../0001-stack-architecture/verify.md), including packaging, validator admission, real provider proof, retained recovery, and existing command regressions. Covers AC-17, AC-18, AC-19.
2. Use doctor on each target with matching, mismatched, missing, and mislabeled images. Prove the same runtime admission rules as manual checks without any provider or container mutation. Covers AC-13, AC-18.
3. Run the shared graceful cleanup, crash recovery, privacy, and controlled identity fault scenarios on both targets. macOS GUI session availability cannot block this completion. Covers AC-5, AC-6, AC-7, AC-12, AC-14, AC-16, AC-19.
