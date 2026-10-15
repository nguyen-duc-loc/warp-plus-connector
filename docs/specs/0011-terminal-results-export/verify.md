# Terminal results and export verification

Verification passed on 2026-10-15 for commit `bfdc949617eb496025e9ef00167d5de0a42c78a7`. The real application, controlled public API probes, private pipe scenarios, and native final archives meet AC-1 through AC-16. All fixtures contained synthetic values. The separate Beta test stage remains available for broader permanent coverage.

## Real terminal journey

1. [x] On native Linux amd64 and arm64, run the final extracted archive without host Node or network access. Open Overview, enter Results, reach every one of 338 distinct candidates, inspect the first and last details, mark rows across pages, and export both matching and marked sets. Verify exact identities and counts through a safe fixture comparison. Covers AC-1, AC-3, AC-4, AC-7, AC-8, AC-16.
2. [x] Repeat packaged startup and the complete browse and file export path in the pinned glibc 2.28 baseline on each architecture. Retain current supported distribution PTY evidence as well. Compilation or emulation alone is not native acceptance. Covers AC-6, AC-13, AC-15, AC-16.
3. [x] Exercise 100 columns, 60 columns, fewer than 60 columns, fewer than 16 rows, no color, help, text input containing shortcut letters, and return navigation. Verify full IDs remain reachable, row ranges are accurate, and revealed keys disappear on every clearing trigger. Covers AC-5, AC-6.

## Data and consistency

| Scenario | Required result | Criteria |
| --- | --- | --- |
| [x] More than 338 candidates and IDs above JavaScript exact integer range | Every row and selected export remains exact, with bounded page memory | AC-1, AC-7, AC-8, AC-12, AC-13 |
| [x] Inconclusive after confirmation, exact expiry, future evidence, unresolved attempt | Latest and definitive facts stay separate; freshness matches existing rules | AC-2, AC-4 |
| [x] Every filter value, combinations, contradictory filters, unchanged filter submission | Correct complete membership; changed filters clear marks; unchanged ones preserve them | AC-2, AC-3 |
| [x] Writer inserts and changes evidence while paging, reviewing, and exporting | List and export retain the original snapshot; refresh installs a complete new one | AC-2, AC-8, AC-12 |
| [x] Refresh removes marked matches or reuses an ID for another key | Only unchanged matching identities retain marks and focus; removed count is exact | AC-3, AC-11 |
| [x] Provenance and attempts exceed page boundaries; legacy attempts lack retry facts | All records are reachable once; absent facts remain unavailable | AC-4 |
| [x] Detail opens after new evidence is saved | Its own fixed time and newer summary are labeled; Results and export remain unchanged | AC-4, AC-8 |
| [x] Collection disappears or is replaced before detail open | Details fail safely without binding a reused ID; retained Results remains explicit | AC-4, AC-11 |
| [x] Initial absent, initialized empty, unreadable, incompatible, or corrupt state | Distinct states, no invented zero results and no migration or recovery | AC-1, AC-11, AC-14 |

## Privacy and protocol

1. [x] Inspect the private protocol under controlled fixtures. Ordinary list, details, progress, review, receipt, and errors contain no key or request token. Only an explicit valid reveal contains its one key. Reveal for another view, closed detail, or stale generation is rejected or discarded. An oversized key produces an explicit reveal error and can still be exported exactly. Covers AC-5, AC-13.
2. [x] Exercise malformed UTF-8, duplicate and unknown fields, null required fields, wrong version, mismatched IDs, invalid counters, oversized frames, stale handles, rapid navigation, busy starts, and lost replies. Failures do not dispatch arbitrary work, restart sessions, or repeat publication. Covers AC-10, AC-13.
3. [x] Put escape sequences, newlines, wide characters, and oversized nonsecret strings in controlled detail fields and paths. Display escaping and explicit shortening prevent terminal injection. Source URLs remain text. Check file bytes separately from rendered text. Covers AC-5, AC-6, AC-8, AC-13.
4. [x] Confirm configuration resolves once, ordinary reads use no writer, and no source, Docker, service control, or network path is called. Help, version, redirected bare root, and explicit CLI commands retain their existing isolation. Covers AC-14, AC-16.

## Export and cleanup

| Scenario | Required result | Criteria |
| --- | --- | --- |
| [x] Matching and marked JSON, with and without explicit keys | Correct exact data, subset metadata, null limit, false truncation, privacy default | AC-7, AC-8 |
| [x] Key lines, unsafe line characters, empty matching set, empty marked set | Exact safe keys; all or nothing preparation; valid empty matching output; explicit empty mark error | AC-7 through AC-9 |
| [x] Relative path, spaces, invalid parent, state alias, final symlink, path too large for request | Review shows exact resolved destination or fails explicitly before staging | AC-7, AC-9, AC-13 |
| [x] Destination created after review or during preparation | Existing file, directory, or symlink remains untouched | AC-9 |
| [x] Parent identity changes after review, mark or view changes, repeated start | Review becomes invalid; no unintended or duplicate export | AC-7, AC-9, AC-10 |
| [x] Encoding error, full temporary filesystem, sync or link failure | No destination before successful publication, safe code, owned resource cleanup | AC-9, AC-12 |
| [x] Cancellation before link, cancellation racing with link, failed directory sync, failed cleanup | Accurate publication and durability facts; complete published file preserved | AC-9, AC-10, AC-15 |
| [x] Refresh or detail preparation fails at each phase | Old Results remains usable; detail failures do not become empty history | AC-11, AC-12 |
| [x] Snapshot cleanup fails repeatedly | Remaining path visible; new preparation blocked instead of accumulating abandoned copies | AC-12 |
| [x] Cleanup fails after view activation; cancellation arrives immediately before or after view or mark commit | Before commit retain old state; after commit install the returned committed view, with any cleanup warning separate | AC-3, AC-10 through AC-12 |
| [x] Cancel returns `cancelling`; cancel is pressed repeatedly | Polls continue to the terminal result; the first cancellation deadline is never extended | AC-10, AC-15 |
| [x] Export preparation lasts longer than twelve seconds | Polls and cancellation remain responsive; no total job timeout mistaken for request failure | AC-10, AC-12 |
| [x] Entered operation resists cancellation, child crashes, SIGINT, SIGTERM, forced shutdown | No replacement work; terminal restoration and exit deadlines hold; publication uncertainty remains truthful | AC-10, AC-13, AC-15 |

Use controlled syscall or writer barriers to prove the publication races, not timing guesses. Verify private `0700` directories and `0600` files under a permissive umask, with no names containing keys. Check that live readers close before browsing and publication, and that temporary resources are not deleted while an entered user still holds them.

## Gates and evidence

Run the repository formatting, vet, race, and lint gate, followed by UI build and tests. Validate the release configuration and prepare both final matching archives with protocol 2. Retain exact commit, archive checksums, native OS and architecture, terminal dimensions, fixture counts, and safe outcomes. Update this record with observed results during verification; do not treat the design checklist as a passed test. Covers AC-1 through AC-16.

**Acceptance evidence, 2026-10-15**

[CI run 38703513452](https://github.com/nguyen-duc-loc/warp-plus-connector/actions/runs/38703513452) passed all five jobs for `bfdc949617eb496025e9ef00167d5de0a42c78a7`. Both native quality jobs passed formatting, vet, race tests, lint, workflow validation, UI compilation and all 40 UI tests. Snapshot creation, final archive checksums, and every bundled payload digest passed. Each final archive then passed its complete terminal journey on the native host and the pinned Debian baseline, without host Node or baseline network access.

| Architecture | Native host | Baseline | Final archive journey |
| --- | --- | --- | --- |
| amd64 | Linux 7.0.0-1017-azure, glibc 2.43 | glibc 2.28 | Passed on both, 32.80 and 35.82 seconds |
| arm64 | Linux 7.0.0-1017-azure, glibc 2.43 | glibc 2.28 | Passed on both, 35.65 and 37.80 seconds |

Final archive SHA256 values:

```text
amd64 dc5c9d73602a6eceb4db7dc82e30af474dfd3dfd7ac8472011983511f678d511
arm64 7dd8eb4ca78b4c07c5943bac9ffd3fbf40fbafe3330afd48f7d23a09fa6ff8f6
```

The downloaded archives were also checked locally with `scripts/check-ui-archives.py`. Both manifests bind protocol 2 and the exact verified commit.

Three confirmed defects were repaired during this acceptance pass. Reply bytes could reach the child before the parent recorded completion, causing a false overlapping request error. A pending key reveal could still disclose its reply after cancel, hide, leave, or refresh intent. Export cleanup also replaced the phase of an earlier failure. Each repair has a regression that fails with the former behavior and passes with the fix. The original isolated mark timeout was not conclusively attributed to the reply race; subsequent complete local and hosted journeys pass, and a failed packaged wait now captures its terminal bytes.

The local runtime ledger is `/private/tmp/wpc-verify-0011/`. It includes actual PTY captures, xterm.js visual replays, private pipe records, syscall traces, public API executable output, exact commands, and source hashes. The ordinary journeys use 338 and 350 candidates, including IDs above the JavaScript exact integer range. Separate probes traverse 61 sources and 62 attempts and prepare a 300000 row collection. All data is synthetic. No live key, validator registration, or service control was used.

| Criterion | Verdict and evidence |
| --- | --- |
| AC-1 | Met. Native final archives reach all 338 rows. `journey.log` reaches 350 exact IDs once each. `edges.log` distinguishes absent, empty, insecure, corrupt, and incompatible state without changing it. |
| AC-2 | Met. `data-linux.jsonl` covers all 2048 filter combinations, exact expiry, future evidence, unresolved attempts, and fixed evaluation. `filters.log` proves concurrent evidence appears only in newer Details or explicit refresh. |
| AC-3 | Met. Packaged journeys and `journey.log` cover paging, unchanged and changed filters, marks, identity replacement, focus, and removed counts. `marks-linux.jsonl` proves cancellation before and after mark commit. |
| AC-4 | Met. Complete source and attempt navigation is captured in `independent.raw`. `data-linux.jsonl` proves saved retry facts, unavailable legacy facts, and an independent fixed detail transaction during a writer commit. |
| AC-5 | Met. `protocol/results.json` proves ordinary replies contain no key or request token and validates sole reveal admission. `protocol/late.json` proves seven late reply clearing paths. Oversized reveal fails explicitly while exact file export succeeds. |
| AC-6 | Met. Native journeys exercise wide, compact, small, no color, help, forms, progress, and outcomes. Full IDs, escaped hostile content, and explicit shortening remain accessible. Visual replays are retained in `review.png` and `results.png`. |
| AC-7 | Met. Matching and marked JSON, independent disclosure, exact key output, empty selection rules, review facts, relative paths, and named destinations were exercised through the real UI. |
| AC-8 | Met. `fixed-evidence.json`, `retained.json`, marked output, and final form probes preserve exact snapshot values and membership. JSON stays at schema 1 with subset metadata, null limit, and false truncation. Unsafe key lines fail without publication. |
| AC-9 | Met. `faults/export-faults.log`, `filesystem.log`, and `full-disk.log` prove real sync and link errors, disk exhaustion, file/directory/symlink races, parent identity rejection, cleanup failure, and truthful retained output. |
| AC-10 | Met. Long collection and export preparation stay responsive beyond twelve seconds. Busy starts are rejected. Real syscall barriers distinguish cancellation before and during publication. Repeated cancel does not extend its first deadline. |
| AC-11 | Met. Real SQLite and filesystem failures at collection, view, summary, sources, and attempts preserve Results or discard partial Details as specified. Real child projection names each failed phase and keeps history unavailable. Retained exports remain usable. |
| AC-12 | Met. Under umask 000, active snapshot directories are 0700 and database/journal files are 0600. Live WAL truncation succeeds while browsing. Increasing rows from 1000 to 50000 raises retained Go heap only from 277184 to 291440 bytes while disk scales. Cleanup barriers prevent new allocation and recover after permissions are restored. |
| AC-13 | Met. Actual parent and child reject malformed, duplicate, unknown, null, mismatched, oversized, and noncanonical records. Stale handles and consumed reviews fail safely. Rapid sequential requests succeed while true overlap fails. Private replies remain within the disclosure contract. |
| AC-14 | Met. `protocol/isolation.json` and syscall tracing show one configuration read, only read access to live state, no Internet/Docker/source/service control, unchanged state, and paused Overview polling until return. Explicit commands retain their packaged checks. |
| AC-15 | Met. Native process journeys cover quit, signals, child failure, startup deadlines, and terminal restoration. Controlled publication stalls record unknown outcome, preserve complete files, and request exit within five seconds with codes 1, 130, or 143. |
| AC-16 | Met. Both final native archives, both baselines, complete local gates, and all hosted jobs pass. Behavior probes preceded the corresponding regression pass. |

For forced shutdown probes, strace deliberately held a kernel thread after the application's exit request. The ledger therefore records the timestamped `exit_group` call and terminal restoration separately from the tracer's artificial wait. Publication diagnostics correctly say `publication=unknown`; complete output is preserved. Tracing containers and browser replay processes were removed or stopped.

Every specified screen was exercised. No missing surface, unapplied migration, or blocked acceptance criterion remains. The live database stays at schema version 5. The feature verification step is complete; the separate scope test step remains open.
