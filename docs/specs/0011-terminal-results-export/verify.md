# Terminal results and export verification

This is the planned acceptance procedure. None of these checks has been run for feature 13 yet. Use controlled private fixtures and retain safe evidence without keys. Verify behavior first, then add meaningful Beta regression coverage.

## Real terminal journey

1. On native Linux amd64 and arm64, run the final extracted archive without host Node or network access. Open Overview, enter Results, reach every one of 338 distinct candidates, inspect the first and last details, mark rows across pages, and export both matching and marked sets. Verify exact identities and counts through a safe fixture comparison. Covers AC-1, AC-3, AC-4, AC-7, AC-8, AC-16.
2. Repeat packaged startup and the complete browse and file export path in the pinned glibc 2.28 baseline on each architecture. Retain current supported distribution PTY evidence as well. Compilation or emulation alone is not native acceptance. Covers AC-6, AC-13, AC-15, AC-16.
3. Exercise 100 columns, 60 columns, fewer than 60 columns, fewer than 16 rows, no color, help, text input containing shortcut letters, and return navigation. Verify full IDs remain reachable, row ranges are accurate, and revealed keys disappear on every clearing trigger. Covers AC-5, AC-6.

## Data and consistency

| Scenario | Required result | Criteria |
| --- | --- | --- |
| More than 338 candidates and IDs above JavaScript exact integer range | Every row and selected export remains exact, with bounded page memory | AC-1, AC-7, AC-8, AC-12, AC-13 |
| Inconclusive after confirmation, exact expiry, future evidence, unresolved attempt | Latest and definitive facts stay separate; freshness matches existing rules | AC-2, AC-4 |
| Every filter value, combinations, contradictory filters, unchanged filter submission | Correct complete membership; changed filters clear marks; unchanged ones preserve them | AC-2, AC-3 |
| Writer inserts and changes evidence while paging, reviewing, and exporting | List and export retain the original snapshot; refresh installs a complete new one | AC-2, AC-8, AC-12 |
| Refresh removes marked matches or reuses an ID for another key | Only unchanged matching identities retain marks and focus; removed count is exact | AC-3, AC-11 |
| Provenance and attempts exceed page boundaries; legacy attempts lack retry facts | All records are reachable once; absent facts remain unavailable | AC-4 |
| Detail opens after new evidence is saved | Its own fixed time and newer summary are labeled; Results and export remain unchanged | AC-4, AC-8 |
| Collection disappears or is replaced before detail open | Details fail safely without binding a reused ID; retained Results remains explicit | AC-4, AC-11 |
| Initial absent, initialized empty, unreadable, incompatible, or corrupt state | Distinct states, no invented zero results and no migration or recovery | AC-1, AC-11, AC-14 |

## Privacy and protocol

1. Inspect the private protocol under controlled fixtures. Ordinary list, details, progress, review, receipt, and errors contain no key or request token. Only an explicit valid reveal contains its one key. Reveal for another view, closed detail, or stale generation is rejected or discarded. An oversized key produces an explicit reveal error and can still be exported exactly. Covers AC-5, AC-13.
2. Exercise malformed UTF-8, duplicate and unknown fields, null required fields, wrong version, mismatched IDs, invalid counters, oversized frames, stale handles, rapid navigation, busy starts, and lost replies. Failures do not dispatch arbitrary work, restart sessions, or repeat publication. Covers AC-10, AC-13.
3. Put escape sequences, newlines, wide characters, and oversized nonsecret strings in controlled detail fields and paths. Display escaping and explicit shortening prevent terminal injection. Source URLs remain text. Check file bytes separately from rendered text. Covers AC-5, AC-6, AC-8, AC-13.
4. Confirm configuration resolves once, ordinary reads use no writer, and no source, Docker, service control, or network path is called. Help, version, redirected bare root, and explicit CLI commands retain their existing isolation. Covers AC-14, AC-16.

## Export and cleanup

| Scenario | Required result | Criteria |
| --- | --- | --- |
| Matching and marked JSON, with and without explicit keys | Correct exact data, subset metadata, null limit, false truncation, privacy default | AC-7, AC-8 |
| Key lines, unsafe line characters, empty matching set, empty marked set | Exact safe keys; all or nothing preparation; valid empty matching output; explicit empty mark error | AC-7 through AC-9 |
| Relative path, spaces, invalid parent, state alias, final symlink, path too large for request | Review shows exact resolved destination or fails explicitly before staging | AC-7, AC-9, AC-13 |
| Destination created after review or during preparation | Existing file, directory, or symlink remains untouched | AC-9 |
| Parent identity changes after review, mark or view changes, repeated start | Review becomes invalid; no unintended or duplicate export | AC-7, AC-9, AC-10 |
| Encoding error, full temporary filesystem, sync or link failure | No destination before successful publication, safe code, owned resource cleanup | AC-9, AC-12 |
| Cancellation before link, cancellation racing with link, failed directory sync, failed cleanup | Accurate publication and durability facts; complete published file preserved | AC-9, AC-10, AC-15 |
| Refresh or detail preparation fails at each phase | Old Results remains usable; detail failures do not become empty history | AC-11, AC-12 |
| Snapshot cleanup fails repeatedly | Remaining path visible; new preparation blocked instead of accumulating abandoned copies | AC-12 |
| Cleanup fails after view activation; cancellation arrives immediately before or after view or mark commit | Before commit retain old state; after commit install the returned committed view, with any cleanup warning separate | AC-3, AC-10 through AC-12 |
| Cancel returns `cancelling`; cancel is pressed repeatedly | Polls continue to the terminal result; the first cancellation deadline is never extended | AC-10, AC-15 |
| Export preparation lasts longer than twelve seconds | Polls and cancellation remain responsive; no total job timeout mistaken for request failure | AC-10, AC-12 |
| Entered operation resists cancellation, child crashes, SIGINT, SIGTERM, forced shutdown | No replacement work; terminal restoration and exit deadlines hold; publication uncertainty remains truthful | AC-10, AC-13, AC-15 |

Use controlled syscall or writer barriers to prove the publication races, not timing guesses. Verify private `0700` directories and `0600` files under a permissive umask, with no names containing keys. Check that live readers close before browsing and publication, and that temporary resources are not deleted while an entered user still holds them.

## Gates and evidence

Run the repository formatting, vet, race, and lint gate, followed by UI build and tests. Validate the release configuration and prepare both final matching archives with protocol 2. Retain exact commit, archive checksums, native OS and architecture, terminal dimensions, fixture counts, and safe outcomes. Update this record with observed results during verification; do not treat the design checklist as a passed test. Covers AC-1 through AC-16.
