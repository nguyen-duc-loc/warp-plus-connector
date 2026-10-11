# Verify: Complete results and export · spec 0006 · updated 2026-10-11

_Verified on 2026-10-11 against the current working tree on macOS arm64 and Linux arm64. All 12 acceptance criteria and all 21 checklist steps passed. The separate Beta test workflow remains next._

## Controlled data and behavior

You can use synthetic candidates and evidence saved through the public store operations. No live source or WARP operation is needed. For commands below, you can supply `--config PATH` and `--state-dir PATH` for that private fixture.

1. [x] Vary private configuration and explicitly supplied common flags. Compare the resolved state directory and freshness interval with defaults, configuration, and flag precedence. Invalid configuration should fail before state or temporary output access. Covers AC-11.
2. [x] Compare repeated and comma separated filters, combined filters, invalid tokens, positive limits, formats, destinations, and disclosure flags across `list` and `export`. Check canonical filter order, deduplication, empty entries, case sensitivity, overflow, and rejection before configuration access. Covers AC-2, AC-4, AC-6.
3. [x] Save 338 distinct candidates, restart, and pause traversal after the first 100 rows. In another process, insert a candidate, complete evidence for a later candidate, and perform permitted writer recovery. The original invocation should retain its membership, summaries, and ascending IDs; a new invocation should see the commits. Covers AC-1, AC-3.
4. [x] Compare exported candidate IDs and authorized key values with the saved records. Text and JSON should omit keys by default; `--show-keys` should disclose complete values, and `--format keys` should emit exact stored bytes plus one LF per key. Control characters, U+2028, U+2029, and invalid UTF-8 should fail key output before delivery. An omitted extra match should not undergo representation checks. Covers AC-5, AC-6.
5. [x] Add repeated sightings and observations at different times. Compare `first_seen_at` and `last_seen_at` with the minimum and maximum saved observation times through sightings. Check UTC output and stored fractional precision. Covers AC-5.
6. [x] Complete attempts with timestamps out of order. Compare latest outcome, reason, and attempt reference with the highest completed attempt ID. A never completed candidate should remain `unchecked` with null completed attempt fields. Covers AC-2, AC-5.
7. [x] Follow confirmation with an inconclusive attempt, then with a later rejection. Compare definitive outcome, evidence time, and attempt reference with the highest definitive attempt ID. Inconclusive results should preserve confirmation and its original freshness time. Rejection evidence should remain visible. Covers AC-2, AC-5.
8. [x] Leave an attempt unresolved. Check its active attempt reference without changing the latest completed outcome. A queued batch item should not invent an active attempt, and read commands should not recover unresolved work. Covers AC-5, AC-11.
9. [x] Fix the application clock and effective interval. Check just before expiry, exact expiry, future evidence, and absent definitive evidence. Compare `as_of`, `max_age`, freshness, and anomaly with that one evaluation across every page. Covers AC-2, AC-3.
10. [x] Parse JSON with an integer capable parser. Check schema version 1, canonical filter arrays, explicit or null limit, null optional fields, hidden keys, exact integer IDs, and UTC timestamps. An empty result should contain `items: []`. Covers AC-5, AC-7.
11. [x] Compare limits 1, 100, 338, and 339, including empty selections and filters whose first match occurs after several pages. Counts should equal successful emissions. Truncation should require one additional matching row in the same snapshot, including when a writer commits during traversal. Covers AC-1, AC-3, AC-4, AC-7.
12. [x] Race a file, directory, and symlink into the destination after preparation starts. Check that publication preserves each existing destination, private modes hold under a permissive umask, and output through a parent alias into state is rejected. A sibling whose name merely shares the state directory prefix should work. After successful publication, cancellation, directory sync failure, and cleanup failure should retain the complete destination and report `published=true`. Covers AC-9, AC-10.
13. [x] Inject query, callback, encoding, staging write, flush, seek, reader closure, output write, directory sync, and cleanup failures. Cancel during preparation and during an entered stdout write. Check stable diagnostics without keys or raw payloads, truthful delivery state, preserved primary errors, and safely quoted remaining temporary paths. Preparation failure should deliver no stdout or named file. No further writes should begin after observed cancellation, and entered work should finish before resource release. Block stdout delivery and confirm the snapshot and reader are already closed. Covers AC-3, AC-6, AC-8, AC-10.

## Commands and platform checks

These checks can be repeated on macOS arm64 and Linux arm64. A successful cross compilation alone does not complete Linux runtime verification.

1. [x] `wpc list` and `wpc export` against the 338 candidate fixture should expose every candidate exactly once. `wpc export --format keys` should produce 338 exact key lines. Covers AC-1, AC-5.
2. [x] `wpc list --status inconclusive --definitive confirmed --freshness fresh` and the equivalent `export` selection should select the same retained confirmations. Covers AC-2.
3. [x] `wpc export --limit 338` should report no truncation for exactly 338 matches; a smaller limit should report truncation when another match exists. Covers AC-4.
4. [x] `wpc export --output PATH` should publish one complete private JSON file and keep stdout empty. A second invocation with that path should fail with `export_exists` and preserve the original bytes. Repeat with `--format keys`. Covers AC-5, AC-9.
5. [x] Empty state and contradictory valid filters should succeed. JSON should contain an empty array, key output should have zero bytes, and text should distinguish no saved candidates from no matching candidates. Missing, old, newer, inaccessible, and damaged state should remain errors. Covers AC-7, AC-11.
6. [x] `wpc list --help`, `wpc export --help`, and `wpc version` should succeed without configuration, state, or temporary output access. Check ordinary error exit 1 and existing SIGINT and SIGTERM exit behavior, including broken stdout and stderr pipes. Covers AC-10, AC-11.
7. [x] Repeat command execution, private file modes, publication races, cancellation, and failure checks on both supported platforms. Retain meaningful regression tests after runtime verification. Covers AC-12.
8. [x] Run the repository gate in order: `rtk proxy sh scripts/check-format.sh`, `rtk proxy go vet ./...`, `rtk proxy go test -race ./...`, and `rtk proxy golangci-lint run ./...`. Each should pass. Covers AC-12.

## Acceptance criteria coverage

| Criterion | Controlled behavior steps | Command and platform steps |
| --- | --- | --- |
| AC-1 | 3, 11 | 1 |
| AC-2 | 2, 6, 7, 9 | 2 |
| AC-3 | 3, 9, 11, 13 | 7 |
| AC-4 | 2, 11 | 3 |
| AC-5 | 4, 5, 6, 7, 8, 10 | 1, 4 |
| AC-6 | 2, 4, 13 | 7 |
| AC-7 | 10, 11 | 5 |
| AC-8 | 13 | 7 |
| AC-9 | 12 | 4, 7 |
| AC-10 | 12, 13 | 6, 7 |
| AC-11 | 1, 8 | 5, 6 |
| AC-12 | All, repeated on both platforms | 7, 8 |

## Verification evidence, 2026-10-11

PASS. The actual `wpc list` and `wpc export` commands ran against fresh private fixtures on both supported platforms. Each platform recorded 119 setup and CLI commands, plus fault, signal, concurrency, publication, and memory probes. Linux containers used no network or Docker socket. No application code changed during verification.

Evidence root: `/private/tmp/wpc-reverify-0006-quxro3sv`.

| Criterion | Verdict | Evidence |
| --- | --- | --- |
| AC-1 | Met | Both command ledgers show 338 distinct ascending IDs and exact key lines. Limits cross every internal page. |
| AC-2 | Met | Both command ledgers cover shared filters, retained confirmation, canonical values, and invalid arguments before configuration. The boundary probe confirms exact expiry and future evidence. |
| AC-3 | Met | `TestQuerySnapshotAcrossWriterProcess` passes with a separate writer changing evidence, inserting row 339, and recovering row 250 after row 100. Original summaries, membership, and truncation at limits 337 and 338 remain fixed. The next query sees every change. |
| AC-4 | Met | Limits 1, 100, 338, and 339, empty selections, and a first match at ID 338 all return correct counts and truncation. |
| AC-5 | Met | JSON, key lines, stdout, and named files pass. The extra probe compares every summary field with saved records. Exact ID 9007199254740993 survives JSON parsing. |
| AC-6 | Met | Hidden and disclosed keys, exact lines, controls, invalid UTF8, and omitted extra matches pass. Error output contains no key values. |
| AC-7 | Met | Empty JSON, zero key bytes, distinct text messages, and missing, incompatible, damaged, and insecure state all produce the expected outcomes. |
| AC-8 | Met | Late query, encoding, staging write, final flush, seek, and reader closure faults deliver no premature output. A database checkpoint succeeds while stdout is blocked. Memory observations are below. |
| AC-9 | Met | Private modes, canonical state aliases, sibling prefixes, and existing destinations pass. A separate process races a file, directory, or symlink into the destination after staging begins; each remains unchanged. |
| AC-10 | Met | SIGINT exits 130, SIGTERM exits 143, broken pipes exit 1. Entered cancellation, short writes, cleanup obstruction, and failed sync after publication preserve truthful delivery status and complete published files. |
| AC-11 | Met | Configuration precedence, help and version without runtime paths, unchanged persisted data, and unresolved attempts surviving reads pass. No source or validator operation occurs. |
| AC-12 | Met | Runtime checks pass on both arm64 platforms. Retained query and output fault tests pass. Formatting, vet, race tests, and lint pass. The separate `/test` workflow can broaden the retained coverage. |

### Commands and artifacts

1. [macOS CLI ledger](/private/tmp/wpc-reverify-0006-quxro3sv/macos/evidence/ledger.json) and [Linux CLI ledger](/private/tmp/wpc-reverify-0006-quxro3sv/linux/evidence/ledger.json) record exact commands, exit codes, stderr, and captured stdout paths.
2. [macOS runtime ledger](/private/tmp/wpc-reverify-0006-quxro3sv/evidence/run-macos.json) and [Linux runtime ledger](/private/tmp/wpc-reverify-0006-quxro3sv/evidence/run-linux.json) record the public API probes and retained test binaries. Every probe exits 0. The subprocess helper is intentionally skipped only when run outside its parent test.
3. [macOS preparation faults](/private/tmp/wpc-reverify-0006-quxro3sv/faults-macos/ledger.json) and [Linux preparation faults](/private/tmp/wpc-reverify-0006-quxro3sv/evidence/faults-linux.json) retain late query, encoding, staging, and invalid byte failures.
4. [macOS final probes](/private/tmp/wpc-reverify-0006-quxro3sv/extra-macos/ledger.json) and [Linux final probes](/private/tmp/wpc-reverify-0006-quxro3sv/evidence/extra-linux.json) retain exact field checks, late matches, final flush failures, and destination races between processes.
5. [macOS signals](/private/tmp/wpc-reverify-0006-quxro3sv/signals-macos/evidence/signals.json), [Linux signals](/private/tmp/wpc-reverify-0006-quxro3sv/evidence/signals-linux.json), and [repository gate](/private/tmp/wpc-reverify-0006-quxro3sv/evidence/gate.json) retain process exits and tool output.

The retained checks are in `internal/app/query_test.go`, `internal/store/export_close_test.go`, and `internal/cli/output_stage_test.go`. The fault cases exercise the real CLI and SQLite driver. The sync fault replaces the destination parent after successful publication with an alias to `/dev/null`, provoking a real sync error; restoring the parent confirms that the complete published file survives.

### Memory and limits of the observation

The scratch memory probe seeds data through the public store and prepares real JSON output to disk. It samples Go heap during export after fixture creation and garbage collection.

| Platform | Candidates | Peak Go heap bytes |
| --- | --- | --- |
| macOS arm64 | 338 | 4288472 |
| macOS arm64 | 33800 | 5050072 |
| Linux arm64 | 338 | 3173696 |
| Linux arm64 | 33800 | 3822824 |

These measurements support bounded live Go memory over the observed range. They do not measure total process RSS or promise unlimited disk capacity. Staging disk use grows with output size, as designed.

Missing surfaces: none. Runtime failures: none. Blocked acceptance criteria: none. The first exact timestamp probe hit an older Python parser limitation with valid fractional seconds; the corrected scratch parser passed. No application change was needed.
