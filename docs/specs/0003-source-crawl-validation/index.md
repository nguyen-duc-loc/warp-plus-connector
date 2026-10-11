# 0003. One source crawl and validation loop

**Date**: 2026-10-10
**Status**: Accepted

## Summary

You can crawl the latest public page from `warpplus`, retain its candidates and coverage, and inspect saved results after restarting the CLI. Selected checks use the default unsupported adapter or the explicitly selected isolated Docker backend. The native CLI targets macOS and Linux arm64; the real validator uses Linux arm64 containers and durable cleanup recovery.

## Structure

1. [Source crawl](0001-source-crawl.md) defines retrieval, parsing, provenance, and the coverage extension to the existing store.
2. [Validator feasibility gate](0002-validator-gate.md) defines the Docker experiment, prerequisites, evidence, and the boundary before a real backend can be built.
3. [Docker validator backend](0003-docker-validator.md) records the final build milestone, the measured runtime, private recovery, and application integration.

## Requirements

**User story**: As the local operator, you can collect candidates from a real public source, select one deliberately for checking, and read its saved outcome without confusing source claims with entitlement evidence.

| ID | Acceptance criterion |
| --- | --- |
| AC-1 | A manual crawl reads only the latest public `warpplus` preview page, without Telegram login. It retains every distinct candidate matching the agreed extraction rule within that page, with exact case and source message provenance. Repeated crawls merge candidate identities without losing observations. |
| AC-2 | Each new crawl has durable requested coverage before its request begins. After parsing, its fetched time, parsed post count including posts without keys, and observed message ID range remain inspectable after restart. Missing observations remain absent on failure. Coverage never claims complete channel history. |
| AC-3 | Text listing reads saved state without source or validator access and reaches every candidate through bounded store pages, including all 338 records in a controlled dataset. IDs, outcomes, reasons, definitive evidence, and freshness remain distinguishable. Keys are hidden unless `--show-keys` is supplied. |
| AC-4 | An explicit check of an existing candidate ID against the unsupported backend saves one inconclusive attempt with reason `validation_unsupported`, with no external registration work. Earlier definitive evidence remains intact. The command explicitly reports lack of validation support and returns a failure exit status. |
| AC-5 | Configuration resolves before state or network side effects. Defaults, JSON, and explicit flags have the accepted precedence. Help, root invocation, and version retain their absence of configuration, state, Docker, and network access. |
| AC-6 | Empty candidate results, unrecognized markup, source failures, state failures, contention, and cancellation are explicit. One request has a 20 second deadline and a 2 MiB body limit, with no automatic retries. Earlier committed messages survive a later failure. Diagnostics reveal no key or credential values. |
| AC-7 | Before real validation is enabled, the experiment proves isolated Linux arm64 execution, attributable Plus evidence, remote device slot cleanup, and recovery after interrupted attachment or cleanup. Uncertain cleanup retains private state and blocks further attachments. No host WARP registration is used. |
| AC-8 | Feature completion requires a real sourced candidate, a check through the proven backend, saved attributable outcome, and a read after process restart. A successful Plus confirmation needs the accepted `Unlimited` evidence. Unsupported operation or controlled fixtures alone cannot satisfy this criterion. |
| AC-9 | The coverage migration preserves existing candidates, provenance, runs, attempts, evidence, and recovery behavior. Old crawl runs report unavailable coverage honestly. Writer ownership and reader behavior remain consistent with spec 0002 on macOS and Linux arm64. |

## Decision

**Chosen option**: Keep one native Go CLI with separate crawl, check, and read actions. Use the existing SQLite store, add one crawl coverage table, and use goquery for public preview parsing. Treat the local Docker experiment as the first feasibility gate, with no real backend enabled by configuration alone. (basis: [scope item 4](../../scope/scope.md), [architecture](../0001-stack-architecture/index.md), [persistent state](../0002-persistent-key-state/index.md), your confirmed choices)

This is a feature spec for scope item 4. You confirmed its initial content on 2026-10-10 after independent review. The Docker experiment established attributable Unlimited evidence and independently observed normal cleanup. The final CLI then confirmed a real sourced candidate, saved its evidence, and returned that confirmation from a separate process after cleanup. Runtime verification and retained tests passed on macOS and Linux arm64, and you confirmed feature completion on 2026-10-11. Your working registration remained unchanged. The [verification record](verify.md) and [scope item 4](../../scope/scope.md#4-one-source-crawl-and-validation-loop--done) retain the evidence and test results.

**Implementation boundary**: The [Docker backend child](0003-docker-validator.md) now owns the tested commands, image, evidence, and recovery contract. Its explicit Docker settings and recovery command extend the initial configuration and CLI tables below. The default unsupported behavior remains supported. No host registration fallback, unverified vendor version, or discard of cleanup uncertainty is authorized.

Small implementation recommendations are settled here. Use concrete application coordination, small source and validator interfaces, the standard HTTP client, a single fetch attempt, and sequential store writes. The alternatives are a generic workflow framework, a separate worker service, or early retry and concurrency machinery; none helps prove this one page path. No additional skill or MCP discovery is requested. (basis: root project conventions, your previous decline, Tracer Bullet delivery)

## Feature design

### Boundaries and flow

`cmd/wpc` owns process signals, stdout, stderr, and exit codes. `internal/cli` parses and renders. `internal/config`, `internal/app`, `internal/source/telegram`, and `internal/validator` implement the runtime path. `internal/state` contains transport independent inputs and records. `internal/store` owns SQL and transactions. `cmd/wpc-worker` runs only inside the isolated validator image. Scheduling remains outside this feature.

`wpc crawl` resolves configuration, acquires the writer, creates the run and coverage atomically, retrieves and fully parses the bounded page, records parsed coverage, saves messages in increasing numeric message ID order, and finishes the run. Network work never occurs inside a database transaction. Persist each message through the existing atomic `RecordMessage` contract. Render the saved report after finishing.

`wpc check <candidate-id>` validates the ID and configuration, acquires the writer, resolves that saved candidate, creates a validation run and attempt, and invokes the selected adapter. The only initially supported adapter selection is `unsupported`. It returns inconclusive without network, subprocess, or WARP calls. Save the attempt, finish the run as `completed`, print the saved result, and report unsupported on stderr with exit 1. Run completion describes execution, not entitlement. An unknown candidate creates no run or attempt. Opening a writer retains the existing initialization and recovery behavior.

Each new command invocation uses new run and attempt request tokens from `crypto/rand.Text()`. Rediscovery is idempotent for candidate identity; separate deliberate checks remain separate history. A retry of the same store mutation retains its token and original normalized inputs. An uncertain write result does not authorize creating a replacement external operation.

### Data model and transitions

The confirmed model reuses all five existing record types. Detailed fields and constraints of the sixth record are in [the source contract](0001-source-crawl.md#coverage-storage).

| Record | Key | Relationship and lifecycle |
| --- | --- | --- |
| Candidate | Existing candidate ID | One candidate has many sightings and attempts; key identity remains exact trimmed text. |
| Sighting | Existing sighting ID | Belongs to one candidate and source message. |
| Observation | Existing run and sighting pair | Connects a crawl run to observed provenance. |
| Run | Existing run ID | `running` becomes `completed`, `failed`, or `cancelled`; recovery alone produces `interrupted`. |
| Attempt | Existing attempt ID | Belongs to a validation run and candidate; unresolved becomes a terminal outcome once. |
| Crawl coverage | Run ID, primary and foreign key | At most one per crawl run; created for every crawl through the new CLI. Legacy runs may lack it. Requested fields are immutable; parsed observations are filled once. |

No source configuration table, message body store, duplicate summary counters, or production registration schema is introduced now. All records remain retained. Store recovery marks unfinished work as specified in spec 0002; it never claims remote cleanup. The laboratory recovery journal is a separate experimental artifact defined in the gate child, not a substitute for the future backend storage contract.

### CLI surface

Every action uses the current operating system account. Result writes propagate output errors. Invalid arguments are rejected before loading configuration.

| Command | Inputs | Output on stdout | Important failures |
| --- | --- | --- | --- |
| `wpc crawl` | No positional arguments; common profile flags | Run ID, status, fixed source and coverage, parsed count and range when available, saved distinct candidate and sighting counts | Busy, HTTP or parser failure, state failure |
| `wpc crawl show <run-id>` | One positive decimal `int64` ID; common flags | The saved crawl report, including active or interrupted state and unavailable fields | Unknown ID, wrong run kind, missing state or migration required |
| `wpc check <candidate-id>` | One positive decimal `int64` ID; common flags | Candidate ID, run and attempt IDs, outcome, reason, backend and version, evidence when present | Unknown ID, busy, unsupported validation, state failure |
| `wpc list` | No positional arguments; `--show-keys` boolean; common flags | All candidate summaries in ascending ID order, streamed through store pages | Missing state, schema mismatch, output failure |

Common runtime flags are `--config`, `--state-dir`, and `--recheck-interval`. Only `check` also accepts `--validator`. `--show-keys` belongs only to `list` and cannot be enabled by JSON. The source is fixed to `warpplus`; there is no source argument or arbitrary URL option. Help for any command remains free of runtime side effects.

Listing uses a page size of 100 and follows every cursor until absent, without prompting or a total limit. Capture `AsOf` once per invocation and reuse it and `MaxAge` for the entire traversal. Preserve the store's membership boundary and current per page details; do not promise a frozen database snapshot. Columns are `ID`, `LATEST`, `REASON`, `LAST_DEFINITIVE`, `EVIDENCE_AT`, `FRESHNESS`, and `CLOCK_ANOMALY`, plus `KEY` only with `--show-keys`. An absent value renders `none`. Times render in UTC using RFC3339 with stored fractional precision. `EVIDENCE_AT` comes from the last definitive attempt, whether confirmation or rejection. An inconclusive latest attempt never obscures that distinction.

An initialized empty collection prints `No candidates saved.` and exits 0. Missing state is an explicit error, not an empty collection. Crawl reports always label coverage as `latest_preview`, with `page_limit=1` and `history_complete=false`; message bounds describe observed IDs, never contiguous historical coverage. An active run reports persisted progress as current, not final. A legacy run prints `coverage unavailable`.

Exit 0 means the requested action completed and any check produced a definitive supported result. Exit 1 covers ordinary command failures and unsupported or inconclusive checks. A definitive invalid result is data and would use exit 0 once a proven backend exists. SIGINT and SIGTERM cancel work and return 130 and 143 respectively. `main` chooses exit status; the application returns typed errors and results. A partial crawl report may be printed if its run is readable, but stderr and the exit status retain the failure.

### Configuration

Reuse the default configuration and platform state paths from spec 0001. On macOS, state defaults to the account's `Library/Application Support/warp-plus-connector`. On Linux, use absolute `XDG_STATE_HOME` plus `warp-plus-connector`, otherwise the account's `.local/state/warp-plus-connector`; ignore a relative `XDG_STATE_HOME`. Explicit state paths are converted to absolute paths from the invocation directory. There is no shell expansion inside JSON or flags.

The initial JSON object has only `state_dir` (nonempty string when present), `recheck_interval` (Go duration string, default `6h`), and `validator` (default and only accepted value `unsupported`). Require a positive interval. There is no scheduler yet; this interval supplies freshness evaluation. Flags override JSON only when explicitly supplied. Reject wrong types, `null` values, duplicate object keys, unknown fields, extra JSON documents, and invalid values. The root value must be an object. Limit configuration input to 64 KiB. These are parser checks, not another configuration framework.

A missing default config file uses defaults; a missing explicit `--config` file fails. Do not create a config file. Existing config must be a regular file owned by the current account with no group or other access, and not a symlink, following the accepted private configuration boundary. Resolve and validate all supplied configuration even for read commands. Help and version bypass this path entirely. No custom environment settings or Telegram credentials are introduced. A requested `docker` backend fails configuration validation until its contract and implementation are proven; it cannot silently fall back to a host client.

### Failure handling and security

Use stable reason codes such as `source_timeout`, `source_transport_error`, `source_http_error`, `source_body_too_large`, `source_parse_error`, `state_error`, `cancelled`, and `validation_unsupported`. Never put upstream error text, message content, keys, raw HTML, or validator output into a reason or diagnostic. Logging uses `log/slog` on stderr with run IDs and fixed fields. No metrics server or remote telemetry is introduced.

A competing writer fails before network work. Readers use `OpenReader` and perform no migrations or recovery. The current account's private state permissions and symlink rules remain those of the existing store. The CLI does not connect to Docker during ordinary crawl, read, or unsupported check operations. No public server, authentication service, tenant model, or regulated data workflow is added.

On fetch or parse failure, finish the crawl as failed with its coverage request retained. An accepted page with no matching keys is a completed zero candidate crawl. On a later message write failure, stop new writes and retain prior commits. Do not mark the whole run successful or replay the network request automatically. Repeated manual crawls can fill missing provenance safely.

On cancellation, stop work and make a best effort to save terminal run and attempt state under a separate cleanup context lasting at most five seconds. An unresolved attempt is inconclusive with reason `cancelled` if that completion can be written safely. If a prior completion has an uncertain commit, do not replace it with a conflicting payload; leave recovery to the next writer. Cancel entered work before closing the store. Store `Close` retains ownership until entered native operations finish, as in spec 0002; the cleanup deadline is not a promise to abandon a live transaction or release its lock early.

### Value sourcing

| Action | Produced value | Source |
| --- | --- | --- |
| Configure | Effective state path, backend, freshness interval | Accepted platform defaults, validated JSON, then explicitly changed flags |
| Crawl | Source and request URL | Constants `telegram:warpplus` and `https://t.me/s/warpplus` |
| Crawl | Candidate text and message identity | Scoped visible post text and validated numeric message identity from the source adapter |
| Crawl | Publication time and message URL | Parsed message metadata; missing or invalid optional publication time stays absent; URL constructed from validated channel and ID |
| Crawl | Fetch and observation times | Application UTC clock: fetch time after bounded response read; one observation time per parsed page, reused across its message writes |
| Crawl report | Requested coverage and parsed count and range | Persisted coverage row; range is numeric minimum and maximum parsed message IDs |
| Crawl report | Saved candidates and sightings | Distinct candidate IDs and observation links for this run, counted within one store read transaction |
| Check | Candidate, run and attempt identity | Positional candidate ID resolved in the store; store generated run and attempt IDs |
| Unsupported check | Outcome, reason, backend identity and version | Adapter constants `inconclusive`, `validation_unsupported`, `unsupported`, and executable version with `dev` fallback |
| Real check | Entitlement, registration attribution, cleanup proof | Only a completed experimental record and the subsequent ratified backend contract; unavailable in the initial implementation |
| List | Latest result, reason, last definitive result and evidence time | Existing candidate summary and its attempts from `ListCandidates` |
| List | Freshness and anomalies | Existing evaluation using the invocation UTC `AsOf`, effective interval, and persisted evidence times |
| List | Keys and total traversal | Stored candidate text only with `--show-keys`; all pages in the store's initial membership boundary |

### Critical verification scenarios

1. Crawl the real preview, inspect provenance through the store, close the process, and use the CLI to read its report and candidates. A changing live page has no guaranteed yield. Use controlled sanitized HTML to prove exact extraction and zero candidate behavior. Covers AC-1, AC-2, AC-3.
2. Repeat a crawl, including repeated keys, several keys per post, posts without keys, and case variants. Inject failure after a committed message and verify both retained progress and failed status. Covers AC-1, AC-2, AC-6.
3. Check a saved candidate with the unsupported adapter, restart, and observe the inconclusive attempt. Use controlled existing confirmation to prove the new attempt does not erase or refresh definitive evidence. Covers AC-3, AC-4.
4. List 338 controlled candidates, verify every ID exactly once, and assert no key or key fragment is shown unless requested. Exercise absent state, legacy coverage, active reports, and output errors. Covers AC-2, AC-3, AC-9.
5. Exercise malformed config, explicit missing config, default absence, precedence, newer schema, permissive files, writer contention, HTTP failures, oversized and compressed bodies, structural changes, and cancellation. Confirm help and version remain independent. Covers AC-5, AC-6, AC-9.
6. Follow the gate child's controlled experiment, including interruption and cleanup reconciliation, before the real sourced check and restart demonstration. These are unavailable tests until prerequisites and the backend contract exist. Covers AC-7, AC-8.

## Build plan

Tracer Bullet delivery begins with the feasibility gate and then proves one thin command path across the existing layers. A blocked experiment does not forbid building the explicitly unsupported path, but it forbids claiming the whole feature is done.

1. Assess and record the Docker experiment prerequisites using the gate child. Provision the isolated environment and controlled subscription when available; otherwise record the block and keep real validation disabled. Satisfies AC-7 and is prerequisite to AC-8.
2. Build the first collection thread: configuration, one coverage migration, atomic crawl start, one page adapter with goquery, message persistence, and `crawl`, `crawl show`, and `list` across process restart. Add packages only as this path needs them. Satisfies AC-1, AC-2, AC-3, AC-5, AC-9.
3. Add deliberate `check <candidate-id>` through the unsupported adapter, recording run and attempt history and preserving prior evidence. Satisfies AC-3, AC-4, AC-5.
4. Complete the same path's error classification, limits, cancellation, privacy, all page traversal, legacy coverage, and platform checks. Satisfies AC-1, AC-2, AC-3, AC-5, AC-6, AC-9.
5. When prerequisites exist, run the controlled Docker experiment. Record measured results, then complete and confirm a concrete backend child through `/architect` before implementing any real adapter or backend recovery schema. Failed or incomplete proof leaves this step blocked. Satisfies AC-7.
6. Only under that completed contract, implement the proven adapter and perform the real source, selected check, persistence, and restart demonstration. Satisfies AC-7, AC-8.
7. Run `/check verify one source crawl and validation loop`, recording partial and blocked criteria truthfully. Then use `/test` for meaningful retained coverage and run the repository formatting, vet, race, and lint gate on the supported platforms. Do not mark the scope feature done while AC-7 or AC-8 is unmet. Satisfies AC-1 through AC-9.

## Consequences

You get a useful collection path and a durable record of what each crawl observed. Explicit unsupported attempts preserve intent without inventing entitlement. The single page and strict grammar keep coverage finite and inspectable.

Public preview markup can change, and this slice intentionally misses older posts and future key formats. Docker requires a prepared local image and private recovery state. Public account capacity and entitlement can change between checks. Future live positive checks need an available authorized Unlimited slot; unknown remote state deliberately blocks further attachments.

## Follow-up

1. Complete: you confirmed the spec after independent review, and scope item 4 links its build milestones. The real backend design gate remains visible.
2. Complete: `go.mod` and `go.sum` record tested goquery `v1.13.0`, cascadia `v1.3.4`, and `golang.org/x/net v0.58.0`.
3. Complete: live parsing and sanitized fixtures established DOM compatibility, as recorded in [verification](verify.md).
4. Complete: the isolated Docker experiment, backend contract, live confirmation, cleanup, and interruption checks passed without changing the host WARP registration. The [backend child](0003-docker-validator.md) and [verification record](verify.md) retain the evidence.
5. Keep multiple channels, historical backfill, parallel checks, retries, export formats, schedules, and background supervision in their existing later scope slices.

## Rationale

Reasoning, alternatives, and research evidence are in [rationale.md](rationale.md).
