# Linux platform transition verification

This checklist was added on 2026-10-12. It records no new pass. It supplies the platform proof required by spec 0008 AC-17 through AC-19. Historical feature verification records retain their original targets and results.

| Target | Release and CLI | Local validator | Native service | Regression gate |
| --- | --- | --- | --- | --- |
| Linux amd64 | Pending | Pending | Pending | Pending |
| Linux arm64 | Recheck changed paths | Recheck compatibility and recovery | Reuse unchanged recorded evidence, verify remaining faults | Run on final change |

For each result, record the commit, executable version, machine architecture, distribution and kernel, toolchain, Docker and systemd versions where used, commands, exit codes, and private evidence location. Evidence from macOS or an emulated mismatched executable does not close Linux runtime checks.

## Release and command path

1. Validate the release configuration and build both `linux/amd64` and `linux/arm64` with CGO disabled. Generate local snapshot archives and checksums without publishing. Inspect the archive inventory and executable formats, proving no Darwin artifact and no missing Linux target. Covers AC-17.
2. Execute each target's binary on its Linux architecture. Verify help, version metadata, exit behavior, and absence of configuration, state, manager, and network work for help and version. Covers AC-15, AC-17, AC-19.
3. Run the existing public command and storage checks, including crawl coverage, migrations, cancellation, writer ownership, all 338 listing and export results, saved checks and history, independent schedules, restart recovery, private permissions, and output failures. Preserve exact keys only at the existing disclosure boundaries. Covers AC-19.

## Validator preparation and operation

1. Exercise default daemon architecture detection and explicit `amd64` or `arm64` selection. Reject an unknown argument, a second argument, nonlocal endpoint, non Linux daemon, and target mismatch. Verify the worker build target, Docker platform, base digest, exact signed package version, labels, resulting image architecture, and stdout containing only an immutable image ID. Covers AC-18.
2. On each Linux architecture, accept only matching executable, local daemon, image, and worker platforms. Exercise canonical and permitted daemon aliases, wrong OS, unsupported architecture, mismatched image, missing labels, wrong protocol, wrong WARP version, and a mutable tag. Rejection must precede registration or attachment. Read only doctor must make no container or provider mutation. Covers AC-13, AC-18.
3. Use separately authorized controlled subscriptions for new amd64 provider experiments. Prove matching Unlimited evidence, independent device slot reclamation, persisted results after restart, host registration isolation, and retained recovery after interruption. Reuse the controlled protocol and safety boundaries in spec 0003. Prove concurrent overlap and pacing under spec 0005. Do not use fixtures as real provider proof. Covers AC-18, AC-19.
4. Retain an existing compatible arm64 image and registry 3 session, then use the amended CLI to reconcile that exact daemon, image, and container. Prove newly configured images cannot replace old session identity, unknown cleanup blocks admission, unavailable daemons retain obligations, and no architecture specific registry path bypasses ownership. Covers AC-18, AC-19.
5. Exercise controlled provider classifications on both worker architectures, including the narrow creation HTTP 429 rejection evidence. Unknown output, missing registration without deletion acknowledgement, or mismatched identity must retain uncertainty. Record whether real pinned amd64 output supports each existing special case before relying on it. Covers AC-18, AC-19.

## Service and completion

1. Complete [spec 0008's service procedure](../0008-background-operation-diagnostics/verify.md) under a real systemd user manager on each Linux architecture. Ordinary application containers and mocked managers do not prove native login activation or supervision. Covers AC-1 through AC-16.
2. Run formatting, vet, the full race suite, lint, and release configuration validation on both Linux targets. Record environment failures and unmet checks separately from passing results. A Go build for another architecture does not replace these runs. Covers AC-17, AC-19.
3. Record the remaining combined managed recovery, identity and control faults, and privacy cases. Retain pending status for any unmet criterion. No macOS GUI access, launchd proof, or macOS artifact is required. Covers AC-16 through AC-19.
