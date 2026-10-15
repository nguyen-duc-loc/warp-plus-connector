# Terminal foundation and overview verification

Acceptance verification passed on 2026-10-15 for code commit `3c76a4310a704f31f01f542968a3fd3e2128e106`. The checklist is covered by the cumulative runtime evidence and the acceptance entry below. Earlier dated entries retain the build history. Fixtures use synthetic keys and controlled service inputs; no live provider attachment was needed.

1. [x] Extract each release archive on native Linux amd64 and arm64 with no host Node or npm. Test the complete tree in the pinned Debian 10 glibc 2.28 userland with stock loader and C++ runtime, then on native Ubuntu 26.04 with its actual user service manager. Disconnect external networking. Launch the real `wpc` in a PTY (a test controlled terminal), read a real saved collection, and quit. Record baseline image digest, glibc, kernel, Node shared library prerequisites, architecture, Node, Ink, React, and dependency lock identity. Covers AC-1, AC-3, AC-15.
2. [x] Exercise help, version, explicit commands, redirected input, redirected output, `TERM=dumb`, and unsupported platforms with deliberately unavailable configuration and state. Prove no Node launch or state access on help and fallback paths. Remove and mismatch UI assets and prove only interactive startup fails. Covers AC-2, AC-3, AC-12.
3. [x] Use at least 338 synthetic candidates and IDs above 2^53. Include fresh, exactly expired, stale, future dated, invalid, unchecked, and later inconclusive outcomes. Compare all aggregates with the existing candidate rules and ensure no key enters the protocol. Covers AC-4, AC-6, AC-12, AC-14.
4. [x] Save more than ten mixed manual and scheduled runs, including legacy reports and saved running rows without a live process. Compare ordering, origin, time labels, missing facts, complete totals, and the eleventh row notice. Covers AC-5, AC-6.
5. [x] Commit changes from an existing writer while a collection refresh is paused inside its snapshot. Verify internally consistent totals, runs, and schedules, followed by updated facts on the next refresh. Prove the UI does not acquire writer ownership or alter records, schema, recovery, configuration, or service state. Covers AC-6, AC-8, AC-14.
6. [x] Exercise absent and initialized empty collections, old and newer schemas, private permission failures, collection disappearance, read timeout, and clock regression. Verify empty guidance, safe codes, retained stale data, original observation times, and successful recovery on refresh. Covers AC-4, AC-8, AC-9, AC-14.
7. [x] Observe installed, missing, inaccessible, stopped, active, and stale service fixtures. Include different selected and bound collections, canonical path aliases, uncertain ownership, installation identity change during observation, and corrupt or unreadable identity after a successful view. Verify metadata survives null component data, identity loss clears caches, and manager failure under a verified identity retains only stale manager data while fresh runner and installation values replace theirs. Confirm no Docker or service controls run. Covers AC-6, AC-7, AC-9, AC-12, AC-14.
8. [x] Drive Tab, Shift+Tab, arrows, paging, Home, End, help, refresh, and quit in the real packaged UI. Resize through wide, compact, and below minimum dimensions, including while a read is pending. Verify focus preservation, complete IDs, bounded refresh coalescing, and visible state. Covers AC-8 through AC-10, AC-15.
9. [x] Run with `NO_COLOR`, `--color never`, light and dark terminal defaults, timezone offset changes, and malicious control characters in safe text fixtures. Inspect output for color independence, accurate parent supplied time labels, and escaped dynamic content. Covers AC-9 through AC-12.
10. [x] Interrupt with keyboard Ctrl+C, parent SIGINT, parent SIGTERM, protocol EOF, hung child, child crash, and terminal hangup. Inject a native read that ignores cancellation and a stalled native manager call. Verify the independent twelve second request watchdog, restoration after the two second child allowance with usable terminal descriptors, and parent exit within five seconds of the first shutdown trigger. Repeated signals must not reset deadlines. Compare exit codes, child reaping, normal reader closure versus explicit forced exit, and untouched background work. Covers AC-12 through AC-14.
11. [x] Inject malformed frames, extra fields, duplicate IDs, incompatible handshake, oversized traffic, unknown values, short pipe writes, broken diagnostic output, and raw JavaScript error text. Validate every closed wire record, nanosecond duration units, signed offsets, and exact integers. Fail both data sources and verify header time and profile suggestions still render from their specified metadata. Verify bounded memory, one outstanding request, no restart loop, no unsafe output, and no second write to failed streams. Covers AC-9, AC-12, AC-13.
12. [x] Run the existing Go formatting, vet, race, and lint gate. Run UI type checking, compiled Node tests for protocol and view logic, package integrity checks, and PTY scenarios. Validate GoReleaser and changed workflows, then build both archives again from committed locks. Hosted execution is recorded only after a successful actual run. Covers AC-1 through AC-15.


## Build evidence, 2026-10-14

You approved recording this build evidence and publishing the feature branch. The opening note above describes the checklist before implementation. The acceptance boxes stay open because these build checks do not cover the full Beta verification matrix.

The implementation uses Node 24.20.0, npm 12.0.2, Ink 7.1.1, React 19.2.8, and TypeScript 7.0.2. The dependency lock SHA256 is `a3b79ee7d4edd945ecf28a2d6b7b1aadb9865acebbd624747df9ea15a1e38d58`. Runtime archive hashes, ELF library requirements, and exact observed platforms are in `ui/runtime.json`.

Local formatting, `go vet ./...`, `go test -race ./...`, and `golangci-lint run ./...` passed on macOS arm64. The affected process and CLI race suites passed again after the final shutdown changes. Lint also passed with `GOOS=linux GOARCH=arm64`. UI compilation, type checking, and five compiled Node tests passed. Actionlint 1.7.7 and `goreleaser check` passed.

`goreleaser release --snapshot --clean` built both Linux archives without publishing. `python3 scripts/check-ui-archives.py` verified the complete directory layout, executable modes, manifest identity, lock digest, and every listed payload hash in both archives. The local snapshot version was `0.1.0-dev-278e099`; its source included the uncommitted terminal implementation, so this is local build evidence rather than a claim that commit `278e099` contains this feature.

The actual arm64 archive passed `TestPackagedTerminal` on both platforms below. The harness launches the packaged Go executable and bundled Node in a real PTY. It seeds 105 synthetic candidates through the real store and checks the rendered total, section focus, help, refresh, resizing, absent state without creation, hidden keys, and absence of decorative color. Normal quit returns 0, keyboard Ctrl+C returns 130, parent SIGTERM returns 143, and child crash returns 1. Every case checks restoration of the original terminal settings and alternate screen exit.

| Platform | Evidence |
| --- | --- |
| Native arm64 Debian 10 container | Image `debian@sha256:bb3dc79fddbca7e8903248ab916bb775c96ec61014b3d02b4f06043b604726dc`, networking disabled, kernel `6.8.0-117-generic`, stock `libc6 2.28-10+deb10u3`, `libstdc++6 8.3.0-6`, and `libgcc1 1:8.3.0-6`. No host Node fallback or replacement system libraries. |
| Native arm64 Ubuntu 26.04.1 | Existing Lima VM `wpc-linux`, kernel `7.0.0-38-generic`, extracted complete archive, existing user account and native service observation path. The test did not install or control a service. |

Store regressions additionally cover a 338 candidate snapshot while a writer commits, the latest ten runs with an eleventh row notice, definitive confirmation after a later inconclusive result, exact expiry, and future evidence. Service regressions cover an unreadable bound collection and cache invalidation when installation identity is unreadable or missing. Protocol and UI tests cover large decimal IDs, malformed and oversized requests, duplicate fields, closed handshake records, retained observation times, service identity changes, and escaped terminal controls.

The disposable baseline container was removed after the checks. The existing Ubuntu VM was returned to its original stopped state.

## Remaining proof

Native amd64 package execution and an actual successful hosted workflow remain pending at this entry. CI now prepares each architecture on its native Ubuntu 26.04 runner, runs the host and pinned Debian baseline PTY checks, and passes the verified UI trees to snapshot packaging.

The original checklist remains the source for separate Beta verification. In particular, finish the installation and fallback fault matrix, full saved value comparisons, active service and identity changes during observation, refresh coalescing and clock changes, cancellation resistant reads, hung children, terminal hangup, failed diagnostics, and protocol fault injection. Compilation, a local archive, or these focused regressions do not close those acceptance checks.


## Initial hosted build, 2026-10-14

[CI run 38575421001](https://github.com/nguyen-duc-loc/warp-plus-connector/actions/runs/38575421001) passed for commit `4e33df712200402733ae39cec70a26bb596927ec`. Both native Ubuntu 26.04 quality jobs passed the full Go gate, UI build and tests, prepared package PTY checks on the host and Debian baseline, and offline validator checks. Snapshot packaging and final archive payload validation also passed.

That run exercised prepared package directories on both native targets. It did not extract the completed snapshot archives on both native targets. A followup CI gate now downloads and extracts those exact archives, checks their hashes, and executes them on each native Ubuntu host and pinned Debian baseline with host Node unavailable on `PATH`. The first build milestone stays open until that stronger gate passes. The broader Beta checklist remains open.


## Final archive proof, 2026-10-14

[CI run 38577012410](https://github.com/nguyen-duc-loc/warp-plus-connector/actions/runs/38577012410) passed all five jobs for commit `7f48d82ed3bb4cc5c69f1c64046ce6a1beeee37d`: both native quality jobs, snapshot packaging, and both extracted archive jobs. The extracted archive jobs checked downloaded checksums and all manifest payload digests, then ran the real packaged executable on the native Ubuntu host and the pinned Debian baseline. Host Node was unavailable on `PATH`, and the baseline containers had networking disabled.

| Final archive | Native host | Stock baseline | PTY suite result |
| --- | --- | --- | --- |
| amd64 | Ubuntu 26.04.1, kernel `7.0.0-1012-azure`, glibc 2.43 | Same native kernel, glibc 2.28, `libc6 2.28-10+deb10u3`, `libstdc++6 8.3.0-6`, `libgcc1 1:8.3.0-6` | All five cases passed on both host and baseline, in 7.63 seconds and 7.65 seconds. |
| arm64 | Ubuntu 26.04.1, kernel `7.0.0-1012-azure`, glibc 2.43 | Same native kernel and stock library versions as the amd64 baseline | All five cases passed on both host and baseline, in 8.34 seconds and 8.47 seconds. |

Both runs used bundled Node 24.20.0. The first build milestone and Build it are now checked in scope. This does not close the broader Beta acceptance checklist above or the separate test workflow.

The final protocol review also reproduced a JSON type error: a null commit field was decoded as an empty string during a development handshake. `TestHelloRequiresStringCommit` failed before the correction. The input decoder now rejects null required fields, while retaining the valid empty string case. The protocol, CLI, and process race suites pass with that regression.


## Acceptance verification, 2026-10-15

**Verdict: PASS.** All fifteen acceptance criteria and the four overview sections have runtime evidence. The complete checklist above is now checked. No missing surface or unresolved verification failure remains. The separate Beta test workflow remains unchecked in scope; this entry does not mark the feature or architecture record done.

[CI run 38677400258](https://github.com/nguyen-duc-loc/warp-plus-connector/actions/runs/38677400258) passed all five jobs for `3c76a4310a704f31f01f542968a3fd3e2128e106`: both native quality jobs, snapshot packaging, and both final extracted archive jobs. This is actual hosted execution of the committed repairs, not an inference from workflow configuration.

| Final archive target | Native Ubuntu host | Offline baseline | Packaged terminal suite |
| --- | --- | --- | --- |
| amd64 | Kernel `7.0.0-1012-azure`, glibc 2.43 | Pinned Debian 10, glibc 2.28 | Passed in 17.44 seconds on the host and 18.29 seconds on the baseline |
| arm64 | Kernel `7.0.0-1017-azure`, glibc 2.43 | Pinned Debian 10, glibc 2.28 | Passed in 17.39 seconds on the host and 22.88 seconds on the baseline |

Both targets used bundled Node 24.20.0, matching version manifests, complete production modules and licenses, and verified archive checksums. The baseline image remained `debian@sha256:bb3dc79fddbca7e8903248ab916bb775c96ec61014b3d02b4f06043b604726dc`, with stock `libc6 2.28-10+deb10u3`, `libstdc++6 8.3.0-6`, and `libgcc1 1:8.3.0-6`. Baseline networking was disabled and host Node was unavailable on PATH. A local native symlink installation also opened the bundled runtime with only the launcher directory on PATH.

The packaged suite exercises thirteen leaf scenarios, including normal quit, keyboard interruption, termination, hangup, child crash, absent state, real saved schedule data, malformed replies through anonymous OS pipes, an open parent reply pipe during child exit, an idle child after hello, early quit, and a delayed first overview. It checks terminal restoration and preserves the normal and signal exit contracts.

### Repairs proven before this pass

The earlier failure reproductions were retained and rerun after the fixes. Valid saved crawl reasons and provider cooldowns now reach the UI. Invalid calendar stamps are rejected. Terminal hangup enters bounded cleanup. Forced cleanup returns failure and attempts `ui_shutdown_timeout` through the invocation sink, while preserving recorded signal codes and the original five second deadline. Normal completion cannot cut off an already claimed timeout record or produce a second final write.

The UI now uses event driven I/O on its inherited pipe descriptors. This removes the blocked filesystem pipe read that prevented malformed reply shutdown. The parent keeps its startup deadline until the first valid overview request proves the child accepted hello. The real malformed hello reproduction now exits with code 1 in about 0.38 seconds. All 23 malformed reply variants pass. These changes do not add a port, background process, mutation method, or dependency.

Durable regressions are in `cmd/wpc/watchdog_test.go`, `internal/cli/diagnostics_test.go`, `internal/terminal/reply_shutdown_linux_test.go`, `internal/terminal/startup_deadline_linux_test.go`, `internal/terminal/packaged_linux_test.go`, and `ui/src/schema.test.ts`. The packaged suite runs in CI on both native architectures and both baseline userlands.

### Runtime evidence by criterion

| Criterion | Result and evidence |
| --- | --- |
| AC-1 | Met. Current committed archives launch the real Ink UI on both native Linux architectures. Selected configuration and explicit state overrides were exercised locally. |
| AC-2 | Met. Separate redirected input and output, dumb terminal, help, version, explicit saved reads, and unsupported host fallback retain their contracts. Native syscall traces show no configuration, state, service, or Node access on fallback, help, and version paths. |
| AC-3 | Met. Both final archives pass integrity and native execution checks. Complete extraction and symlink launch work without host Node. Missing or mismatched assets fail safely while explicit commands remain usable. |
| AC-4 | Met. A native Linux public API probe seeds 338 candidates and independently compares paged candidate rules with overview totals. Later inconclusive evidence, exact expiry, stale and future evidence, invalid and unchecked candidates all agree. Actual private replies and UI rendering preserve IDs above 2^53 and complete totals. |
| AC-5 | Met. Fifteen mixed saved runs produce descending latest ten plus an older history notice. Saved running status, manual and scheduled origin, recovery time, unknown legacy origin, crawl counts, validation counts, and absent legacy selection facts are exercised through the store and actual UI. |
| AC-6 | Met. A real writer commits candidate, run, and schedule changes from the clock callback after snapshot establishment. The current result stays consistent and the next read sees every change. Collection, installation, manager, and runner times remain independent. Saved eligibility is not presented as live execution. |
| AC-7 | Met. Native service API fixtures prove canonical aliases, selected versus bound collection differences, private access failures, and identity changes during observation. The UI labels the other collection and preserves independently available sections. |
| AC-8 | Met. Entry, manual and automatic refresh, six presses during a delayed reply, one pending request, no overlap, focus and scroll preservation, and shrinking content clamping pass. Automatic refresh begins about five seconds after completion. |
| AC-9 | Met. Loading, absent, initialized empty, unavailable, stale retained data, disappearance, schema mismatch, private permission failures, and clock anomalies remain distinct. Failed reads retain original observation time. Identity loss clears old service values. Recovery replaces stale values. |
| AC-10 | Met. Tab, reverse Tab, arrows, paging, Home, End, help, refresh, quit, compact layout, minimum dimensions, and resize while reading all work. Terminal settings and alternate screen restoration pass. |
| AC-11 | Met. Genuine native PTY captures replayed in xterm.js show readable light and dark defaults, persistent focus markers, and English text. Both no color controls remove decorative styling. Actual malicious path controls are escaped. UTC, Kathmandu, and New York offsets agree with the saved instant; January and July records show their different offsets in one view. |
| AC-12 | Met. Malformed frames, duplicate fields and IDs, extra fields, incompatible handshake, oversized input, unknown methods and codes, interrupted reply writes, EOF, invalid dates, nested closed records, and raw JavaScript errors fail safely. Real private reply capture contains no synthetic keys or request tokens. No restart loop occurs. |
| AC-13 | Met. Quit returns 0, interruption 130, termination 143, and failures 1. Stopped child cleanup, cancellation resistant reads, independent request timers, repeated signal precedence, terminal hangup, and forced timeout diagnostics preserve bounded exit and restoration. Real broken and blocked stderr pipes receive one parent destination attempt. |
| AC-14 | Met. Native traces show read only collection access, no writer acquisition, no source retrieval, and no Docker execution. Database, WAL, and lock hashes remain unchanged across reads; writer ownership and saved running rows survive. Missing state remains absent. Service fixture reads leave the bound collection and configuration absent and preserve unrelated service state. |
| AC-15 | Met. Actual final archives pass native amd64 and arm64 startup, interaction, failure, restoration, and offline baseline execution. Formatting, vet, race tests, lint, UI compilation and tests, workflow validation, and archive validation pass. |

The current local runtime drivers recorded 144 successful observations and no failures. Their exact commands, captures, pipe records, syscall traces, and aggregate ledger are in `/tmp/wpc-complete-verify/evidence/`. Native data boundary evidence is in `data-native.log` there. Earlier cumulative fixtures and source probes remain under `/tmp/wpc-reverify-0010/` and `/tmp/wpc-verify-final/`. These local scratch artifacts supplement the committed regressions and hosted logs; they are not release payloads.

Native active service proof used a unique temporary user systemd unit and a synthetic runner. The real manager PID and independently read POSIX lock owner both reported PID 3179. The overview kept installation, manager, and runner observations separate, labeled a different selected collection, and did not create the bound store or configuration. Stopping only the fixture produced inactive manager state, released ownership, and preserved the saved stopped observation. A controlled older timestamp demonstrated stale saved evidence. The fixture and runtime unit link were removed, and the existing service state was unchanged. This proves observation behavior, not real provider work or production service installation.

The visual artifacts under `/tmp/wpc-complete-verify/visual/` are explicitly labeled xterm.js replays of real native PTY recordings, not desktop terminal screenshots. No downloaded font or image asset was needed. The active service fixture, visual browser, scratch server, and child processes were removed or stopped, and the existing Linux VM was returned to its original stopped state.

No live WARP attachment or remote cleanup claim is part of this terminal feature verification. Scope now checks only Verify it for feature 12. Test it remains the separate next Beta step.
