# Verify: one source crawl and validation loop, spec 0003

You can run these checks with private scratch profiles and keys you are authorized to activate. The Docker backend needs the immutable image ID printed by `rtk proxy sh scripts/build-validator-image.sh`. Provider capacity and entitlement can change, so a controlled available slot is needed for the positive check.

## Collection and saved state

1. [x] Run `wpc crawl --state-dir <scratch>`, then read its run with `wpc crawl show <id> --state-dir <scratch>` in a new process. Expect latest_preview, page_limit=1, history_complete=false, fetched time, parsed count, numeric message bounds, and saved counts. Requested coverage must already exist when the request starts. Covers AC-1 and AC-2.
2. [x] Exercise controlled HTML with duplicates, case variants, inline and block boundaries, hidden content, nested widgets, empty feeds, media posts, invalid dates, and malformed or duplicate IDs. Repeat discovery and inspect canonical provenance and retained observations. Covers AC-1, AC-2, and AC-6.
3. [x] List 338 controlled candidates through the real CLI. Expect every ID once, keys hidden by default, and explicit key output only with --show-keys. Verify absent state differs from an initialized empty collection. Covers AC-3.
4. [x] Check a saved candidate with unsupported, including one with earlier definitive evidence. Expect exit 1, validation_unsupported, one inconclusive attempt, unchanged definitive evidence, and unchanged freshness. An unknown ID creates no attempt. Covers AC-3 and AC-4.
5. [x] Vary private JSON and explicit flags. Reject malformed, duplicate, unknown, null, oversized, permissive, and symlinked configuration before state or network work. Verify explicit flag precedence and help, root, and version independence. Docker requires an immutable image and a local socket. Covers AC-5.
6. [x] Exercise source HTTP errors, redirects, unrecognized markup, decoded body overflow, a 20 second request deadline, partial message failure, state corruption, contention, output errors, and SIGINT or SIGTERM. Expect explicit safe reasons, no key leaks, preserved commits, and correct exit codes. Covers AC-6.
7. [x] Open legacy schema 1 with a reader, then a writer. Expect explicit migration required, successful schema 2 migration, preserved records and evidence, unavailable legacy coverage, interrupted recovery, and unchanged writer ownership on macOS and Linux arm64. Covers AC-9.

## Isolated validation

8. [x] Build the pinned Linux arm64 image and inspect its immutable ID and runtime labels. Reject an incompatible image, remote endpoint, foreign container identity, host networking, privileged mode, or mounts before registration work. Confirm host registration and routing remain independent. Covers AC-5 and AC-7.
9. [x] Run `wpc check <sourced-id> --validator docker --validator-image <image-id> --state-dir <scratch>`. With an authorized available Unlimited account, expect matching license attribution, confirmed evidence, a UTC observation time, backend and image version, and completed temporary cleanup. List it in a separate process and expect the saved confirmed result. Covers AC-7 and AC-8.
10. [x] Verify delayed account updates do not become premature confirmation. Matching Free, device capacity, provider throttling, and unknown responses remain inconclusive with distinct reasons. Only the exact measured creation rejection proof can close a refused registration without deletion. Covers AC-6 and AC-7.
11. [x] Kill the checking process after attachment intent, after a matching account response, and after remote deletion acknowledgement but before result delivery. Run `wpc validator recover` or another check. Expect reconciliation of the same session before new registration, with no repeated attachment and no lost private observation. Covers AC-7.
12. [x] Lose the worker's deletion acknowledgement. Expect retained container, registration backup, journal, and observed evidence. Missing local registration alone must not release ownership or permit another attachment. Independently reconcile the remote state before lab disposal. Covers AC-7.
13. [x] Hold ownership from another profile, inject journal write failure, introduce an untracked labeled container, and attempt delayed worker start after prepared state was recovered. Expect no unauthorized second attachment, no secret in host arguments or diagnostics, and no bypass through a cleaned manifest. Covers AC-6 and AC-7.

## Commands

```sh
rtk proxy go build -o bin/wpc ./cmd/wpc
rtk proxy sh scripts/build-validator-image.sh
rtk proxy go test ./...
rtk proxy pre-commit run --all-files
rtk proxy gofmt -l cmd internal scripts
rtk proxy env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bin/wpc-linux-arm64 ./cmd/wpc
```

## Value source coverage

| Source | Steps |
| --- | --- |
| Defaults, private JSON, explicit flags, freshness clock | 3, 4, 5 |
| Fixed source URL, scoped message text, canonical provenance | 1, 2 |
| Requested coverage, fetch time, parsed range, committed counts | 1, 2, 6 |
| Stored candidate, run, attempt, and observation identities | 1, 2, 4, 9 |
| Saved outcomes, reasons, definitive evidence, freshness | 3, 4, 9, 10 |
| Immutable image and tested vendor version | 8, 9 |
| Local Docker endpoint and daemon identity | 8, 13 |
| Random session identity recorded before creation | 11, 13 |
| Matching registration evidence and worker UTC time | 9, 10 |
| Explicit deletion acknowledgement and durable cleanup receipt | 9, 11, 12 |
| Recovery phase, exact resource identity, and shared ownership | 11, 12, 13 |

## Acceptance criteria coverage

AC-1: steps 1 and 2. AC-2: steps 1 and 2. AC-3: steps 3 and 4. AC-4: step 4. AC-5: steps 5 and 8. AC-6: steps 2, 6, 10, and 13. AC-7: steps 8 through 13. AC-8: step 9. AC-9: step 7.


## Acceptance result, 2026-10-11

PASS. AC-1 through AC-9 are met. Every listed runtime surface was exercised. At the end of this acceptance run, the Beta test step was still open. The subsequent test pass recorded below completed the feature.

The continuing acceptance run used private scratch state at `/private/tmp/wpc-check-dmen8wm0`. The initial collection and persistence observations remain part of this run. Later checks completed the live validator cases that were initially blocked. No application code was changed during verification.

| Criteria | Observed result | Scratch evidence |
| --- | --- | --- |
| AC-1, AC-2 | Two real crawls each parsed 20 posts and saved 100 candidates. Candidate identities remained stable, with 200 retained observations. Requested coverage was readable before fixture HTTP requests. Controlled parsing covered case, boundaries, hidden and nested content, media, dates, entities and invalid IDs. | `cli-ledger.jsonl`, `probe.log`, `remaining/source-extra.log` |
| AC-3, AC-4 | The CLI returned all 338 controlled IDs, hid keys by default, preserved earlier confirmation and freshness after unsupported checks, and rejected unknown candidates without creating work. Separate process reads and operation with network access denied passed. | `cli-ledger.jsonl`, `linux.log` |
| AC-5, AC-6 | Private configuration, explicit flag precedence, help independence, safe errors, a 20 second deadline, decoded body limits, partial commits, contention and cancellation passed. SIGINT returned 130 and SIGTERM returned 143. | `cli-ledger.jsonl`, `probe.log`, `docker-probe.log` |
| AC-6, AC-7 | Seventeen cases ran the actual Linux worker with a synthetic provider executable and networking disabled. Delayed attribution did not confirm early. Free, capacity, throttling, unknown and unattributed responses remained distinct. Incomplete or conflicting creation rejection evidence stayed blocked. Journal write failure prevented attachment; a delayed start after recovery performed no provider calls. | `remaining/provider-cases.jsonl` |
| AC-7 | The live CLI was killed after attachment intent, after matching Unlimited evidence, and after remote deletion acknowledgement before delivery. Each session retained one registration and one attachment, then recovered successfully. A competing profile was blocked. Host process arguments contained no submitted key. | `remaining/live-faults.jsonl` |
| AC-7 | Losing deletion acknowledgement retained the container, journal, registration backup and Unlimited evidence. Another check was blocked without another attachment. The lab operator independently checked the captured real deletion acknowledgement and missing registration response before disposing of that exact session. | `remaining/live-faults.jsonl` |
| AC-7 | Unsafe images, remote endpoints, foreign identities, host networking, privilege and mounts were rejected. An orphan remained blocked even with a cleaned manifest. Host registration and default route hashes were unchanged after the live checks. | `docker-probe.log`, `remaining/cleaned-orphan.log`, `remaining/final-live.json` |
| AC-8 | Candidate 19 from Telegram message 17385 returned confirmed Unlimited through the rebuilt unmodified validator image. Run 12, attempt 10 exited 0. A separate list process returned confirmed and fresh evidence. No validator container or shared session remained. | `remaining/final-live.json` |
| AC-9 | Schema 1 readers required migration. Writers migrated to schema 2 with six tables, preserved provenance and evidence, and recovered interrupted work. Legacy coverage, writer ownership and concurrent reads passed on macOS and Linux arm64. | `linux.log`, `probe.log`, `acceptance-report.txt` |

The final image was `sha256:edfd8265e115516d574aebac5023239f85286d3444ea32ec999e7396c28146c9`, built by `rtk proxy sh scripts/build-validator-image.sh`. Inspection reported Linux arm64, protocol 1 and WARP `2026.8.2100.0`. The final evidence time was `2026-10-10T17:43:19.574387Z`.

Provider response fixtures verify classification and recovery behavior; they are not live entitlement proof. Live confirmation and cleanup used the authorized sourced candidate. One interruption probe initially read the wrong evidence field name, missed its checkpoint, and allowed normal successful cleanup. The corrected probe reached the checkpoint and passed. No missing surface or unresolved application failure remained.

The repository gate passed formatting, vet, race tests and lint. All temporary verification containers were removed. The subsequent test pass on 2026-10-11 completed retained regression coverage on macOS and Linux arm64, as recorded in [scope item 4](../../scope/scope.md#4-one-source-crawl-and-validation-loop--done). You confirmed feature completion that day. Both Verify it and Test it are complete, and the spec status is Accepted.
