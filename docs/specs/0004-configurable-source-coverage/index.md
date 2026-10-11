# 0004. Configurable source coverage

**Date**: 2026-10-11
**Status**: Accepted

## Summary

You can crawl several public Telegram channels with a bounded number of pages for each. One saved run explains which channels and pages succeeded, where work stopped, and which discoveries were committed. This extends the existing CLI and preserves earlier reports and validation evidence.

## Requirements

**User story**: As the local operator, you can choose public sources and coverage, retain every candidate within successfully processed pages, and inspect partial results after restarting.

| ID | Acceptance criterion |
| --- | --- |
| AC-1 | Without source configuration, crawl `warpplus`, `warppluscn`, `warpPlusHome`, and `warp_veyke`, in that order, with one page each. An explicit source list replaces these defaults. Disabled channels receive no requests. |
| AC-2 | Resolve shared and per channel page limits through private JSON and explicit flags. Reject invalid configuration and an empty crawl selection before state or network access. Accept at most 32 configured channels and page limits from 1 through 100. |
| AC-3 | `wpc sources` shows configured sources and effective limits without database, Docker, or network access. `crawl --source` selects configured enabled sources, and `--page-limit` overrides all selected limits. Help and version remain free of configuration and state access. |
| AC-4 | Create one run and its complete source plan before fetching. Record each page request before sending it, then parsed facts before discoveries. Reports preserve requested coverage, parsed coverage, saved counts, source outcomes, and page outcomes after restart. |
| AC-5 | Fetch sequentially with at least one second between request starts, a 20 second deadline per request, and a 2 MiB decoded body bound. Follow only validated older page cursors within the selected channel, never exceed its page budget, and make no extra probe request. |
| AC-6 | Preserve exact candidate identity and source message provenance across channels, overlapping pages, and repeated runs. Rediscovery creates no duplicate candidate, sighting, or observation within its existing uniqueness boundary and does not change entitlement evidence. |
| AC-7 | A source fetch or parse failure preserves earlier commits and allows later channels to run. The invocation exits nonzero if any channel fails. HTTP 429 stops all further requests, records rate limiting, and explains unattempted sources. |
| AC-8 | Cancellation, storage failure, and process interruption retain committed work and truthful unfinished status. The next manual crawl creates a new run from the latest pages. It does not resume or automatically retry an earlier request. |
| AC-9 | Coverage distinguishes a page budget stop from the end of the visible preview and from failures. It always reports `history_complete=false`. A recognized empty feed succeeds, while unrecognized markup fails. Posts without keys count toward parsed coverage. |
| AC-10 | One versioned migration preserves existing records, old reports, store ownership, and recovery on macOS and Linux arm64. New reports remain readable when current source settings change. Crawling never invokes the validator or reveals harvested keys in output or diagnostics. |

## Decision

**Chosen option**: Extend the current packages and add source and page records beside legacy coverage. Keep one run per invocation, sequential requests, private JSON as the editable source list, and the existing candidate and observation identities.

This is an enhancement for scope feature 5. It adds a new contract to spec 0003 without replacing that completed slice. You accepted the completed feature on 2026-10-11 after runtime verification and regression tests passed. No new dependency, service, community skill, or MCP server is required.

You confirmed this design on 2026-10-11 after independent review and approved the resulting recovery and reporting clarifications. Scope feature 5 records the build milestones.

Use concrete application coordination and small source interfaces. Keep cursor validation in the Telegram adapter, plan selection in configuration and application code, and transactions in the store. A generic crawler framework is the runner up; it would add abstractions without another supported provider.

## Feature design

### Configuration and selection

Add the following fields to the existing private JSON object. Existing fields and their validation remain supported.

```json
{
  "crawl_page_limit": 1,
  "sources": [
    {"channel": "warpplus"},
    {"channel": "warppluscn", "page_limit": 3},
    {"channel": "warpPlusHome", "enabled": true},
    {"channel": "warp_veyke", "enabled": false}
  ]
}
```

This example illustrates an override and a disabled source. The built in list enables all four seeds. Omitting `sources` uses that list. Supplying `sources`, including an empty array, replaces it completely. Omitted `enabled` means true. Omitted per channel `page_limit` inherits `crawl_page_limit`, whose default is 1.

Accept bare channel names containing 1 through 64 ASCII letters, digits, or underscores. This is the application's path safety rule, not a claim that every such name exists on Telegram. Reject whitespace, `@`, URLs, slashes, dots, control characters, and non ASCII input. Normalize ASCII case to lowercase. Source identity is `telegram:<normalized-channel>`. Reject duplicate normalized channels, including disabled entries. The limit of 32 counts all entries.

Page limits are JSON integers from 1 through 100. Reject strings, fractional values, `null`, and out of range values. Retain the 64 KiB file limit, private ownership rules, rejection of duplicate keys at every object depth, unknown fields, wrong types, and trailing documents. Validate the complete supplied configuration, including disabled entries, before applying invocation selection. An explicit flag cannot rescue an invalid file.

Resolve limits in order: built in default, shared JSON default, per channel JSON override, explicit `--page-limit`. Use Cobra's changed flag state. Source flags select a subset; they do not create sources or enable disabled entries. Reject an empty flag value, duplicate normalized selection, unknown source, or disabled selection. Preserve configuration order regardless of flag order. Use a repeatable string array flag; commas are not list separators.

The configuration resolver may return zero enabled sources. Only `crawl` rejects that empty effective selection, with `source_selection_empty`, before opening state. This lets `sources`, saved reports, listing, and checking remain useful while sources are disabled. No config file is automatically created. Configuration is read once per command; edits affect later invocations.

### Retrieval and traversal

Generalize the existing adapter from a fixed source to a normalized channel and an optional older cursor (a positive decimal message ID). Construct the first URL as `https://t.me/s/<channel>`. Construct older requests with the same origin and path and the sole query `before=<cursor>`. Construct message URLs as `https://t.me/<channel>/<message-id>`. Never fetch a supplied URL verbatim.

Preserve spec 0003's extraction, visible text, direct source post, optional publication time, body limit, content type, redirect rejection, user agent, and no retry rules. Compare channel identity without ASCII case sensitivity, then store only its normalized name. Message IDs are positive `int64` values, canonicalized without leading zeros. Duplicate message IDs within one page remain a parse error. Candidate bytes remain case sensitive.

Recognize pagination only through a dedicated older navigation control belonging to the recognized feed. Resolve its link relative to the canonical request URL for validation only. Accept exactly the HTTPS `t.me` origin, the selected `/s/<channel>` path, no credentials, fragment, or explicit port, and exactly one `before` query with a positive `int64` value. An absolute or relative link has the same checks. If a navigation cursor attribute is also present, it must agree. Multiple controls may describe the same cursor; conflicting cursors fail parsing. Ordinary links, forwarded widgets, and post text never supply pagination.

On the first page, an older cursor must be no greater than the minimum parsed message ID. On later pages it must also be strictly less than the requested cursor. This guarantees forward progress through older coverage without assuming contiguous message IDs. A cursor on an empty feed, a malformed recognized control, or a nonprogressing cursor is `source_pagination_error`. Fully validate the page and its navigation before returning any parsed facts or messages.

After successfully saving the whole page, stop if it advertises no older control, using `preview_end`. Otherwise stop at the configured page limit using `page_limit`, or request its validated next cursor. If both conditions coincide, `preview_end` takes precedence. Neither condition proves complete channel history. Do not guess a cursor from a message ID when the control is absent. A recognized empty feed has zero posts and no next cursor.

Exact selectors are an implementation detail that must be established with a sanitized live fixture before pagination is accepted. This design does not claim that current Telegram markup or pagination has been verified. Recognized navigation elements must not make an otherwise empty feed fail the existing empty feed check. Unknown nonmessage feed content continues to fail explicitly.

Overlapping pages may repeat a source message. Preserve the first observation of each sighting within a run. Later occurrences can add newly seen candidates from an edited post, but do not overwrite an earlier sighting's known URL or publication time. Page parsed counts describe occurrences, including repeated posts and posts without keys. Their sum is named `parsed_post_occurrences`, never a distinct post count. No complete list of parsed IDs is stored.

Use a monotonic timer to enforce at least one second between HTTP request starts across all channels in this invocation. If processing already took a second, no further delay is needed. Waiting responds to cancellation. The first request starts immediately. Keep the existing dedicated transport with connection reuse disabled and no automatic replay. No proxy bypass or transport policy change is introduced. The maximum plan has 3,200 page requests; there is no separate whole run timeout or throughput promise.

### Saved model

Retain the existing `runs`, candidates, sightings, observations, attempts, and `crawl_coverage` records. Add three tables. Primary and foreign keys identify records and their parents; all timestamps use UTC integer microseconds. Required fields are nonnullable. Fields listed as optional remain absent until known.

| Record | Required fields | Optional fields | Keys and relationships |
| --- | --- | --- | --- |
| `crawl_sources` | `run_id` integer, `source_id` text, `channel` text, `position` integer, `page_limit` integer, `status` text | `started_at`, `finished_at`, `recovered_at` times; `stop_reason` text | Primary key `(run_id, source_id)`; foreign key to run; unique `(run_id, position)`. One run has 1 through 32 source results. |
| `crawl_pages` | `run_id`, `page_number` integers, `source_id`, `requested_url`, `status` text, `requested_at` time | `before_id` integer, `fetched_at` time, `parsed_post_count` integer, `min_message_id`, `max_message_id` integers, `next_before_id` integer, `finished_at`, `recovered_at` times, `reason_code` text | Primary key `(run_id, source_id, page_number)`; composite foreign key to source result. One source has 0 through its page limit records. |
| `crawl_page_observations` | `run_id`, `sighting_id`, `page_number` integers, `source_id` text | None | Primary key `(run_id, sighting_id)`; foreign keys to the existing observation pair and to its page triple. Links each new plan observation to exactly one page. |

The identity, order, and limit fields in source rows are the immutable snapshot of the enabled, selected plan. Their lifecycle fields change as described below. Unselected and disabled entries are absent from that run. `position` is contiguous from 1 in effective order. Request fields are immutable; page numbers are contiguous from 1. The first page has no cursor. Later pages use exactly the preceding completed page's saved `next_before_id`.

The source statuses are `pending`, `running`, `completed`, `failed`, `cancelled`, `interrupted`, and `skipped`. Page statuses are `requested`, `parsed`, `completed`, `failed`, `cancelled`, and `interrupted`. A source starts pending, becomes running just before its first page request, and ends in a terminal state. A page starts requested, becomes parsed after validated facts commit, and completes only after every message write finishes. Fetch or parse failure leaves parsed fields absent. Failure after parsing preserves them.

Parsed facts fill once. Zero posts requires absent ID bounds and no next cursor. Positive counts require positive ordered bounds. Absence of `fetched_at` means parsed facts are unavailable. With parsed facts present, absent `next_before_id` means the recognized preview advertised no older control. Capture `fetched_at` once after the bounded body read, and use it for all message observations from that page.

Normal terminal source and page states have `finished_at`, with no recovery time. Recovery alone produces `interrupted`, records `recovered_at`, and leaves finish time absent. Pending sources skipped before a request keep `started_at` absent. A skipped source has a finish time and a reason. Completed pages have no error reason. Completed sources have `preview_end` or `page_limit` as their stop reason. Other terminal states require a stable reason code.

Use SQL checks for local field constraints, foreign keys with restricted deletion, and transactional checks for lifecycle and parent relationships. Confirm that a run is a running crawl, a source is running, and a page is parsed before saving its message. The message source must match its parent source. Validation attempts never belong to these records. Keep all history indefinitely.

### Store operations and atomicity

Keep SQL behind `internal/store` and domain types in `internal/state`. Extend the existing report with a source collection and its pages; legacy coverage stays optional and separate. New plan runs never receive a legacy coverage row. Existing legacy store operations remain available for old callers and fixtures, with their prior contract. They reject attempts to mutate a new plan run through the legacy crawl path.

| Operation | Contract |
| --- | --- |
| `StartCrawlPlan` | Accept a request token, start time, and complete selected source plan. Atomically create one crawl run and all pending sources. Matching normalized token replay returns the saved plan; mismatched inputs conflict. |
| `StartCrawlRequest` | Accept run, source, page number, and request time. Derive URL and cursor from the immutable source and preceding page. Atomically start the source if pending, using this first request time as `started_at`, and create request intent before HTTP. Reject pages beyond the budget or after source termination. |
| `RecordCrawlParse` | Accept page identity and validated parsed facts. Fill once, moving requested to parsed. Identical normalized replay succeeds; conflicting replay fails. |
| `RecordCrawlMessage` | Accept page identity and the existing message input. Atomically save candidates, sightings, observations, and the page links for newly inserted observations. Existing observations retain their original page and time. Reuse the existing discovery transaction logic. |
| `FinishCrawlPage` | Accept page identity, terminal outcome, application supplied finish time, and reason when required. The application attests that every returned message write succeeded before requesting completed status. The store validates lifecycle, not an expected message count. Terminal identical replay succeeds; conflicting changes fail. |
| `FinishCrawlSource` | Finish a source only when its active page is terminal. Validate normal stop conditions against its last page and budget. |
| `FinishCrawlPlan` | Finish the parent and, when aborting, its unfinished children in one transaction. Reject success unless every selected source completed. Return the persisted report. |
| `GetCrawlReport` | Read run, legacy coverage or source and page records, and committed counts in one short read transaction, including active runs. Missing runs and wrong kinds retain existing errors. |

All mutations check cancellation, own their inputs while work is entered, and preserve the store's existing error and locking contracts. Request creation replay preserves the original time and immutable inputs. Replay checks for request creation, parsed facts, and terminal completion precede closed state checks; an identical stored operation succeeds without changing records. Message writes require an active parsed page, even for duplicates, matching the existing running run requirement. An existing observation encountered on a different active page is a successful duplicate, not a conflict, and keeps its original page link.

Compute saved sighting counts from observation links and saved candidate counts from distinct linked candidate IDs. Page counts use page links; source counts use all its page links; run counts use existing run observations. Do not sum page candidate counts to obtain a distinct source or run count. Existing `RecordMessage` must reject new plan runs so observations cannot omit their required page link.

A request row is durable intent, not proof that bytes reached Telegram. A crash between its commit and HTTP start can leave a requested page with no known response. Call its count `pages_requested`, never confirmed network requests. This avoids inventing network outcomes during recovery.

Parsed post counts include posts without keys, and saved observation counts exclude duplicates. Neither proves that every message was processed. The application holds the fully parsed page in bounded memory, processes each message with candidates, and requests page completion only after all those writes return success. No expected message count or saved message manifest is added. Tests must prove that any failed write prevents the completion call and another page request.

### Coordination, failure, and recovery

Resolve configuration and selection, acquire the single writer, and save the whole plan before any source request. Process each selected channel in order. Wait for pacing, capture the application UTC clock immediately before request intent persistence, persist that value as `requested_at`, fetch and fully parse, persist facts, write messages in numeric ID order, then finish the page. The first request's saved timestamp also supplies the source's `started_at`. Pacing uses actual monotonic HTTP start times, not these intent timestamps. Do not hold a database transaction during a request or wait.

| Event | Page and source outcome | Remaining work and parent outcome |
| --- | --- | --- |
| Recognized page with no candidates | Save parsed coverage and complete the page | Continue its cursor if any; empty candidate output is success |
| Ordinary HTTP, transport, timeout, body, or parse error | Fail page and source with the adapter code | Continue later channels; finish parent failed with `source_failed` |
| HTTP 429 | Fail page and source with `source_rate_limited` | Skip pending sources with `rate_limit_abort`; parent failed with `source_rate_limited`; no sleep and retry based on `Retry-After` |
| Store mutation returns an error | Preserve whatever committed; do not attempt terminal writes | Stop requests and further mutations. Return the original classified error; leave unfinished work for writer recovery. A terminal write that actually committed remains terminal. |
| Nonmutating state operation fails while the plan is known to be active | When safe to write, best effort fail the active page and source with the classified state error | Skip pending sources with `state_abort`; fail the parent with the same classified error. If an abort mutation fails, use the mutation error rule and stop writing. |
| Cancellation | Active page and source become cancelled where safely writable | Pending sources become skipped with `cancelled`; parent cancelled; preserve signal exit semantics |
| Process stops without terminal writes | Leave committed facts as they are | Next writer recovery marks active pages, running sources, and parent interrupted with reason `interrupted`; pending sources become skipped with `interrupted_before_start` |

On recovery, pending sources skipped because of interruption receive `recovered_at` and no finish or start time. This is the sole exception to normal skipped source finish times. Recovery updates all affected records in the same transaction as existing run and attempt recovery. Terminal child records remain unchanged. Only children of unfinished plan runs are recovered.

An abort without an active page can finish its running source directly. A normal parent completion or ordinary source failure finalization happens only after all source rows are terminal. An abort can finalize unfinished children atomically. Source outcomes never change any candidate's check result or freshness.

Use one separate cleanup context of at most five seconds for an abort that is safe to persist, not one allowance per source. The existing store error contract does not prove whether every failed mutation committed. Therefore treat any mutation error conservatively: stop all further mutations, including cleanup, and leave unfinished status for the next writer. This applies equally to cancellation returned by a mutation and to an error from a terminal write. A later report may expose a commit that succeeded despite the error, but it does not authorize a conflicting write. Report the original error on stderr and through the exit status.

Cancellation between successful operations and an external source failure may use cleanup while all previous mutation outcomes are known. A nonmutating state failure may also use that path when the plan is known to remain active. For a safe state abort, active page `reason_code`, active source `stop_reason`, and parent `reason_code` receive the same classified state error, such as `state_error`; pending sources receive `state_abort`. Recovery instead records `interrupted` for active pages, sources, and the parent, even if the initiating command reported a storage error. It does not invent the historical cause of an unfinished write. Never downgrade an already terminal record after a report read fails. Store close continues to wait for entered native operations while retaining ownership, as it does today.

New manual invocations always get a new random request token and begin from the latest preview of each selected channel. Store mutation replay supports uncertain local acknowledgements; it does not authorize another network request. There are no retries, cursor resume commands, crawl scheduling, or source concurrency in this slice.

### CLI and report surface

All actions run as the current operating system account. Existing common flags, command parsing, help behavior, and signal handling remain in force.

| Command | Inputs | Output and effects | Important errors |
| --- | --- | --- | --- |
| `wpc sources` | No positional arguments; common profile flags | Configured order, normalized channel, source ID, enabled state, effective JSON page limit. No state initialization or network work. | Config or output errors |
| `wpc crawl` | Common flags; repeatable `--source <channel>`; optional `--page-limit <integer>` | Persist one plan and print the saved report. Crawl output has no key values. | Selection, config, busy, source, state, cancellation, output |
| `wpc crawl show <run-id>` | Existing positive decimal run ID and common flags | Read the complete saved report without contacting sources. Source selection and limit flags are not accepted. | Missing run, wrong kind, state or schema error, output |

`sources` prints `CHANNEL`, `SOURCE`, `ENABLED`, and `PAGE_LIMIT` columns, including disabled entries. An empty configured list prints `No sources configured.` and exits 0. Only `crawl` accepts selection and coverage override flags. `sources` therefore describes the next crawl with no invocation overrides.

For new plans, print a run header with `run_id`, `status`, `reason`, `counts_current`, `selected_sources`, `saved_candidates`, and `saved_sightings`. `selected_sources` is the saved source row count for this run. Follow it with source blocks in saved order and page lines in page order. Print every page, up to the finite 3,200 record bound, without silent truncation.

Each source block prints source identity, canonical initial URL, effective `page_limit`, status, stop reason, `counts_current`, start/finish/recovery times, `pages_requested`, `pages_parsed`, `pages_completed`, `parsed_post_occurrences`, observed minimum and maximum IDs, distinct saved candidate count, saved sighting count, `more_available`, and `history_complete=false`. `more_available` is `unknown` without last page parsed facts, otherwise true or false from its next cursor. A failed page after a successful page makes the source value unknown if that last requested page has no parsed facts. ID bounds aggregate only pages with facts and never imply continuous history.

`pages_requested` counts page rows, `pages_parsed` counts rows with `fetched_at`, and `pages_completed` counts completed rows. `parsed_post_occurrences` is absent if no page has parsed facts, otherwise the sum of available parsed counts. Bounds are absent when none of those pages contain posts. Saved counts are always numeric, including zero. These derivations also apply to active reports without treating an unknown parse as an empty successful page.

Each page line prints page number, status, request URL, request cursor, request/fetch/finish/recovery times, next cursor, parsed post count and bounds, saved counts, and reason. Absent fields print `none`; unavailable booleans print `unknown`. Times use UTC RFC3339 with stored precision. Source `counts_current` is true only while that source is pending or running; run `counts_current` follows the existing running status rule. Counts are derived from this report's transaction.

Legacy reports continue to show their existing coverage fields or `coverage unavailable`. They never acquire invented source or page rows. Reports use stored plans even when the current valid configuration removes or disables a source. Current malformed configuration continues to fail runtime commands under the existing configuration contract.

Exit 0 means all selected sources completed their bounded coverage. A page budget stop is successful and visibly limited. Ordinary failures and HTTP 429 use exit 1. SIGINT and SIGTERM retain 130 and 143. A valid `crawl show` exits 0 even when the saved run failed. Output failure returns an error without rewriting saved run outcomes. Keep diagnostic codes stable and send them to stderr. Add `source_selection_invalid`, `source_selection_empty`, `source_pagination_error`, `source_rate_limited`, and `source_failed` where needed; malformed config retains the existing config error category.

### Security and value sources

Preserve private config and state permissions, symlink rejection, local writer ownership, normal TLS verification, and sanitized errors. Send no Telegram credentials or cookies. Reject redirects and arbitrary navigation destinations. Source names and message IDs are validated before URL construction. Raw HTML, post text, key values, and upstream errors remain absent from diagnostics and stored coverage. Crawl and source inspection do not construct or contact a Docker backend. No new auth service, tenant model, environment setting, or regulatory workflow is introduced.

Logs use the existing `slog` stderr handler with run ID, normalized source ID, page number when known, and stable outcome code. Do not add a metrics service or remote telemetry.

| Action | Produced value | Authoritative source |
| --- | --- | --- |
| Configure and inspect | Source order, enabled state, effective limit | Built in seeds, validated JSON replacement list and overrides |
| Select crawl | Selected order and final budgets | Effective enabled config filtered by explicit source flags; explicit page limit applied last |
| Start crawl | Run ID, start time, request identity | Store generated run ID, application UTC clock, fresh `crypto/rand.Text()` token |
| Fetch | Request URL, source ID, page number, cursor | Saved plan, fixed Telegram URL construction, preceding completed page's validated cursor |
| Request intent | Page request time and source start time | Application UTC clock immediately before request intent persistence; first request time reused for source start |
| Parse | Candidates, message IDs, publication time, next cursor | Scoped recognized post markup and validated feed navigation; invalid optional publication time stays absent |
| Save | Fetch and observation times | One application clock capture after body receipt, normalized to UTC microseconds |
| Report | Source plan, page facts, times, states and reasons | Saved source and page rows and parent run; no current config lookup for historical fields |
| Report | Saved distinct candidates and sightings | Observation and page links joined to candidate identities in one read transaction |
| Report | Parsed occurrences, range, page counts, more available | Aggregation of saved page facts and states; absent facts remain unknown |
| Report | Selected source count and source progress flag | Saved source row count; source status pending or running for `counts_current` |
| Report | Coverage completeness | Constant false for complete history; stop reason and last parsed next cursor explain the visible boundary |
| Stop and recover | Finish or recovery times | Application finish clock or existing writer recovery clock, never inferred from fetch times |

## Migration plan

**Strategy**: Add new records beside legacy coverage in schema version 3. Keep the old report path. This is one local writer migration, with no backfill or external coordination.

1. Add `crawl_sources`, `crawl_pages`, and `crawl_page_observations`, their constraints, and a page link index on `(run_id, source_id, page_number)`. Existing primary key prefixes cover ordered source and page reads; add no speculative indexes.
2. Leave all existing tables and rows unchanged. Update the schema version in the same migration transaction. Add child recovery to the existing writer recovery transaction.
3. Route new CLI crawls through the plan API. Reader reports select the new format when source rows exist, otherwise the old format. The two formats are mutually exclusive for a run.

**Rollback**: A failed migration rolls back completely. After a successful migration, an older binary rejects the newer schema; reverting code alone is not a database downgrade. Keep the upgraded binary or restore a separately verified private preupgrade snapshot while no writer is active, accepting loss of later records. Do not delete new tables automatically or copy an active WAL database as a backup.

**Risks**: A missing page link could make saved counts misleading, so observation and link insertion must be atomic. Legacy and new APIs must reject mixed run formats. Verify record preservation and migration failure on both supported platforms.

## Build plan

Tracer Bullet delivery proves a small configured path across every layer before expanding traversal and failure handling.

1. Prove one configured channel and one page through config, the schema migration, plan persistence, generalized adapter, CLI crawl, and saved report after restart. Preserve old reports. Satisfies AC-2, AC-4, AC-6, AC-10.
2. Expand that path to the four defaults and a custom source list, sequential source outcomes, `sources`, source selection, limit precedence, and failure in one source followed by success in another. Satisfies AC-1, AC-2, AC-3, AC-4, AC-7.
3. Prove two real public preview pages using a sanitized navigation fixture and bounded cursor traversal. Complete per page records, overlap handling, stop reasons, request pacing, and coverage limits. Satisfies AC-4, AC-5, AC-6, AC-9.
4. Complete HTTP 429 abort, cancellation while waiting or writing, uncertain commits, child recovery, empty selection, output errors, and privacy behavior. Satisfies AC-2, AC-3, AC-7, AC-8, AC-10.
5. Run `/check verify configurable source coverage` against the acceptance criteria, then `/test` for meaningful retained coverage. Run the repository formatting, vet, race, and lint gate and the supported platform checks. Satisfies AC-1 through AC-10. The verification plan is in `verify.md`.

## Consequences

You get one inspectable record for each invocation and durable explanations of partial coverage. Four default sources broaden the first run compared with the current executable. Sequential fetching and pacing are simple to operate but can make large plans slow. At the maximum limits, thousands of requests can take many hours.

Public preview markup remains an external dependency. End of visible pagination does not establish complete historical coverage. Stopping on 429 preserves completed work but can leave later sources untouched. Retained page metadata grows with crawl history, and exact distinct counts of all parsed posts remain unavailable by design.

## Follow-up

1. Verify live channel identity and older navigation markup during implementation, recording only sanitized evidence. A dead seed is an explicit source failure, not a reason to replace the configured list silently.
2. Keep retries, concurrent validation, export formats, scheduling, and background supervision in their existing later scope slices. Cursor resume and a database source registry are outside this design.
3. After implementation, durable source and store context will need updates to describe the new contracts. Spec 0003 also contains an older progress paragraph inconsistent with its accepted status; that historical cleanup is separate from this feature.

## Rationale

Reasoning and alternatives are in [rationale.md](rationale.md).
