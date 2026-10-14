# Verify: readable development logs · spec 0009 · updated 2026-10-14

Steps derived from spec 0009 acceptance criteria. You can run `/check verify readable development logs` against this checklist, then use `/test` for the durable regression suite. The unchecked steps are the verification plan, not a claim of native acceptance.

## Runtime checks

- [x] V1. Run a controlled crawl, concurrent check, and foreground runner on Linux amd64 and arm64. Compare info and debug events with the acknowledged saved runs. Starts and completions appear at info. Successful page, attempt, and mutation activity requires debug. Retries and inconclusive work remain visible at warn. A definitive invalid candidate with proven cleanup stays debug. Saved read commands add no activity chatter. AC-1, AC-4, AC-9, AC-11, AC-12.
- [x] V2. Run the real CLI with stderr attached to a terminal and stdout redirected, then reverse the streams. Try automatic, pretty, text, and JSON diagnostics, plus `TERM=dumb`, nonempty `NO_COLOR`, and `--color never`. Compare mode and styling with each actual destination. Check structured text quoting and unchanged command results. AC-2, AC-5, AC-9, AC-12.
- [x] V3. Inject foreground event times across midnight, an offset change, and a backward jump. Repeat with historical service entries whose `at` values differ from reader time. Expect local milliseconds, full level labels, date and location context before the first visible event, and another header on every date or offset change. Append order stays intact. Metadata and gap notices have no invented time. AC-3, AC-7.
- [x] V4. Exercise IDs above 2^53, zero counts, false cleanup flags, normalized source names through their accepted length, nonempty optional outcomes, and omitted optional fields. Compare IDs with acknowledged coordinator records, retry timing with the committed retry decision, elapsed values with the coordinator clock, and totals with saved reports. Expect exact JSON integers, full readable IDs, duration strings, and no session identifiers. AC-4, AC-9, AC-11.
- [x] V5. Read existing service archives and archives written by the new capture adapter. Select the latest N records, then apply each minimum level in pretty and text modes. Check original text indices, selected, displayed, and filtered counts, all filtered and empty selections, retention flags, and continuity notices. JSON contains every selected record and no display metadata. Managed capture preserves its code catalogue, timestamps, sequence, identities, and event shape. AC-7, AC-8, AC-11.
- [x] V6. Follow archives through hidden entries, invocation changes, rotation, and gaps. Verify that the reader advances over filtered entries and preserves snapshot, gap, and entry order. Show full saved installation and invocation identities, sequence, stream, origin, lane, and counts. Do not infer absent lane reasons from other entries or invent historical attempt detail. Snapshot counts remain limited to the initial selection. AC-7, AC-8, AC-11.
- [x] V7. Exercise every accepted saved status and code category. Compare levels with the spec tables. Failed results are errors, cancelled and interrupted results are warnings, and completed validation with any positive inconclusive, unresolved, or unstarted total is a warning. Large counts must not overflow the severity predicate. Lane reasons can raise the level only when present on that same entry. AC-1, AC-8.
- [x] V8. Supply invalid presentation options and parser inputs containing synthetic credentials. Expect safe argument errors before configuration or state access. Check unavailable configuration, typed service errors, export publication followed by delivery failure, and unknown errors. Preserve typed publication, delivery, control, and ownership evidence without parsing an error string. Help and version ignore unused runtime options. AC-5, AC-6, AC-9.
- [x] V9. Send SIGINT and SIGTERM to foreground work, including at `--log-level error`. Expect one final warning with the existing 130 or 143 exit status. Close diagnostic output during admitted work and check saved finalization without recovery hiding the outcome. A failed diagnostic stream receives no fallback or second write. Doctor report suppression remains effective. AC-6, AC-10.
- [x] V10. Inject simultaneous writes, a short write, an unsupported field shape, an unknown event, and an oversized record. Expect complete serialized events, a first error latch, one cancellation callback after unlocking, and existing cleanup budgets. Fail a service reader consumer and confirm that only the reader stops. Fail managed transport or archive output and confirm that observations retain the managed diagnostic failure. AC-9, AC-10, AC-11.
- [x] V11. Inspect every output mode and managed archive at debug level with synthetic keys, credentials, raw upstream errors, and control characters in rejected input fixtures. No raw private values or terminal control sequences from dynamic data reach display. Only the renderer may introduce permitted color. AC-9, AC-10, AC-12.

## Commands

You can run these on both supported Linux architectures. Controlled validator fixtures can prove logging behavior without making a new provider attachment.

- [x] `rtk proxy go build -o bin/wpc ./cmd/wpc` builds the real CLI. AC-12.
- [x] `rtk proxy sh scripts/check-format.sh` reports no formatting violations.
- [x] `rtk proxy go vet ./...` passes.
- [x] `rtk proxy go test -race ./...` passes, including the diagnostic, CLI, service capture, and process pipe cases. AC-1 through AC-11.
- [x] `rtk proxy golangci-lint run ./...` reports no issues.
- [x] `rtk proxy rg -n 'slog|managedHandler|NewTextHandler' cmd internal -g '*.go'` finds no superseded logging path. AC-11.

## Value sourcing coverage

| Value from spec 0009 | Check |
| --- | --- |
| Foreground event time | V1 and V3 compare the injected clock with the actual boundary |
| Historical event time | V3 uses the original saved `at` |
| Local date, zone, and UTC offset | V3 changes event time through the invocation location |
| Pretty or structured mode | V2 varies flags and the actual destination |
| Styling | V2 varies terminal status, color, `NO_COLOR`, and `TERM` |
| Severity and message | V1 and V7 exercise event and typed code tables |
| Run, source, candidate, and attempt identity | V4 compares acknowledged records and omitted facts |
| Retry timing and worker counts | V1 and V4 compare saved policy, committed delay, and coordinator counters |
| Completion counts and status | V1 and V4 compare acknowledged terminal reports |
| Service identity, sequence, stream, origin, lane, and counts | V5 and V6 compare validated saved entries |
| Selection and retention metadata | V5 and V6 compare the finite view and follow envelopes |
| Displayed and filtered counts | V5 checks the predicate after selecting N records |
| Final diagnostic details | V8 compares permitted typed error fields |
| Cancellation and exit result | V9 compares process signals, saved finalization, and exit status |

## Acceptance criteria coverage

AC-1: V1 and V7. AC-2: V2. AC-3: V3. AC-4: V1 and V4. AC-5: V2 and V8. AC-6: V8 and V9. AC-7: V3, V5, and V6. AC-8: V5, V6, and V7. AC-9: V1, V2, V4, V8, V10, and V11. AC-10: V9, V10, and V11. AC-11: V1, V4, V5, V6, V10, and the removal search. AC-12: native Linux execution of V1, V2, and V11 plus the repository gate.

## Build evidence and limits

Local development on macOS arm64 passed a real one page public crawl, terminal and redirected presentation checks, unsupported validator check verbosity, SIGINT and SIGTERM exits, archive compatibility fixtures, and the repository gate. Zerolog `v1.35.1` was exercised. Linux amd64 and arm64 binaries compile. These observations do not complete native Linux runtime verification or establish new live entitlement and remote cleanup evidence.


## Verification result, 2026-10-14

PASS. All twelve acceptance criteria were met on native Linux amd64 and arm64 with Go 1.27.2. Each architecture recorded 81 distinct CLI cases, plus controlled application and diagnostic probes. The tested snapshot includes the stderr metadata fix and its regression tests. All 296 captured source files matched on both hosts, and the local working tree matched before these checklist updates.

Evidence is retained at `/private/tmp/wpc-reverify-logs.MJqdjr/`, under `amd64/` and `arm64/`. Each directory has `ledger.jsonl` with exact CLI arguments, expected process exits, and stdout and stderr artifact names. `native.log`, `native.status`, and `validation.log` record the complete gate and source and value comparisons. Every basename below exists in both architecture directories.

| Criterion | Verdict | Observed evidence |
| --- | --- | --- |
| AC-1 | Met | `crawl-info.log`, `crawl-debug.log`, `runner.log`, and `probe-batch-info.jsonl` show starts, completions, debug detail, warning retries, and quiet saved reads. |
| AC-2 | Met | The `destination-*` ledger cases independently attach each stream to a terminal. `dumb`, `no-color`, `color-never`, and `pretty-dumb` verify styling suppression. |
| AC-3 | Met | `archive-clock.stdout` and `probe-diagnostic-pretty.log` preserve original event time, date and offset context, and append order across clock changes. |
| AC-4 | Met | `probe-diagnostic-*` preserve full IDs above 2^53 and accepted source lengths. `probe-clock.jsonl` matches injected mutation durations of 250 ms and 500 ms and attempt duration of 3.75 s. Saved retry decisions and all completion counts match the batch event records. |
| AC-5 | Met | `invalid-*`, `parser-secret`, `help`, and `version` ledger cases prove early safe validation and isolation from unused runtime settings. |
| AC-6 | Met | `published-delivery-*.strace` show exactly one failed stderr write in each format. Exports remain complete and exit 1. Signal cases retain exits 130 and 143 and one warning on healthy stderr. `probe-final-errors-*` and `managed-control-delivery.stderr` preserve safe typed facts. |
| AC-7 | Met | `archive-*`, `follow-*`, and `managed-*` exercise real finite and following service reads with original identities, timestamps, rotation, and gaps. |
| AC-8 | Met | `archives.log` confirms 144 saved event category cases. Selection precedes filtering, text indices and display counts remain accurate, and JSON retains all selected records. |
| AC-9 | Met | Rejected input and final error probes omit synthetic private data and control sequences. Results and diagnostics use separate streams. `archive-permissions.stderr` records rejection of nonprivate archive files. |
| AC-10 | Met | `probe-concurrent.jsonl` contains complete concurrent events. Short writes and malformed records latch one failure callback. The fixed metadata path makes no second write. `managed.log` proves reader failure isolation and retained managed archive failure observations. |
| AC-11 | Met | The production removal search found no superseded logging path. `managed-archive.jsonl` retains schema version 1 and permitted event shapes. New capture and existing archive fixtures use the same public reader. |
| AC-12 | Met | Both `native.status` files report `overall exit=0`. The real CLI, archive reader, coordinator probes, formatting, vet, full race suite, and lint all ran natively on both supported architectures. |

All specified command and presentation surfaces were exercised. No surface was missing and no migration was required. Validation used controlled backend responses, as permitted by this plan. Earlier live provider entitlement and remote deletion limits remain separate.

The previous AC-6 and AC-10 failure is resolved: a failed metadata write retains its diagnostic failure state, so final rendering never writes to that stream again. Complete publication and delivery facts remain available on the typed error.
