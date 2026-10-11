# 0006. Complete results and export

**Date**: 2026-10-11
**Status**: Proposed

## Summary

You can filter every saved candidate and export matching summaries as JSON or complete keys as one key per line. Listing and export use a snapshot (one fixed database view), with no total limit unless you choose one. Output is prepared privately before delivery, and named files never replace existing paths.

## Requirements

**User story**: As the local operator, you can inspect saved outcomes and freshness, then obtain the complete matching collection for scripts or manual use.

| ID | Acceptance criterion |
| --- | --- |
| AC-1 | `list` and `export` reach all 338 distinct candidates in a controlled dataset exactly once, in ascending candidate ID order. Defaults include every candidate, without a total cap. Internal page boundaries do not change output membership. |
| AC-2 | Both commands support latest outcome, last definitive result, and freshness filters with identical semantics. Later inconclusive results preserve earlier definitive evidence and its freshness. Invalid filters fail before configuration, state, or output preparation. |
| AC-3 | Both commands use one fixed database snapshot and freshness evaluation per invocation. Inserts, new evidence, and recovery committed after the snapshot begins cannot change its rows, filter decisions, or truncation result. Concurrent writers remain usable. |
| AC-4 | An optional positive `--limit` selects the first matching candidates. Output states the requested limit, returned count, and whether another match was omitted. A dataset of exactly the limit is not reported as truncated. |
| AC-5 | `export` supports JSON and plain key lines, to stdout or a named file. JSON includes versioned collection metadata and the confirmed candidate summary model. Diagnostics never enter exported data. |
| AC-6 | JSON and text hide keys unless `--show-keys` is true. `--format keys` explicitly authorizes complete keys. Key lines preserve stored values exactly and fail explicitly if a selected value cannot be represented as one safe line. Diagnostics never disclose keys or fragments. |
| AC-7 | No matches is success: JSON contains `items: []`, key output has zero bytes, and text explains the empty result. Missing, incompatible, inaccessible, or damaged state remains an error, not an empty collection. |
| AC-8 | Query, encoding, or staging failure delivers no stdout data and publishes no named file. Memory remains bounded independently of the total candidate count. The database snapshot closes before copying to stdout or publishing a named file. |
| AC-9 | Named files are private and appear only after complete preparation. Existing files, directories, and symlinks are never replaced, including a path created by another process during preparation. Failure preserves any existing destination. |
| AC-10 | Cancellation and output failures stop delivery, release owned resources, and return a failure exit status. Stdout can contain a partial copy after delivery begins. Publication followed by cleanup or durability failure is reported truthfully without deleting the published file. |
| AC-11 | Commands use the existing private configuration and read only store boundary, with no migration, recovery, source access, Docker access, or state mutation. Help and version create no temporary output and do not read configuration or state. |
| AC-12 | Verify the commands and file publication on macOS and Linux arm64. Retain meaningful tests for completeness, filters, snapshot concurrency, privacy, cancellation, output failure, and publication races; the repository formatting, vet, race, and lint gate passes. |

## Decision

**Chosen option**: Extend the existing CLI with a shared snapshot query and private output preparation. Keep `list` for text and add `export` for JSON and key lines. Reuse Go, Cobra, the existing SQLite driver, and `encoding/json`; add no dependency, configuration layer, or stored export job.

This is the design for scope feature 7. You confirmed it on 2026-10-11 after independent review and the approved path resolution and cancellation clarifications. It extends spec 0003's CLI listing behavior and adds a separate snapshot read operation to spec 0002's store. Existing paged store APIs and saved crawl and check reports retain their contracts. This feature remains `Proposed` until its implementation lifecycle advances.

Internal recommendations are explicit: filter existing domain summaries in the application to reuse the established evidence rules; a second SQL implementation of freshness is the alternative and risks disagreement. Prepare output on disk rather than collecting every row in memory. Use atomic file creation without replacement rather than an existence check followed by rename. Keep SQL and transaction lifetime inside `internal/store`, query policy in `internal/app`, and parsing and serialization in `internal/cli`.

## Feature design

### CLI and selection

Every command runs as the current operating system account. No positional arguments are accepted.

| Command | Inputs | Stdout | Important errors |
| --- | --- | --- | --- |
| `wpc list` | Shared filters, `--limit`, `--show-keys`, existing common flags | Existing text columns, prepared from one snapshot | Invalid arguments, state failure, staging failure, output failure |
| `wpc export` | Shared filters, `--limit`, `--format json\|keys` (default `json`), `--show-keys`, optional `--output PATH`, existing common flags | Export bytes when `--output` is absent; empty otherwise | The above, unrepresentable key, invalid destination, existing destination, publication failure |

Common flags remain `--config`, `--state-dir`, and `--recheck-interval`. New selection and output flags are invocation only and have no JSON configuration equivalents. In particular, configuration cannot enable key disclosure. Omitted `--output` means stdout; an explicitly empty path is invalid, and `--output -` is rejected with guidance to omit the flag for stdout. Relative paths resolve from the invocation directory without shell expansion. The destination parent must already exist.

| Filter flag | Accepted values | Candidate field |
| --- | --- | --- |
| `--status` | `unchecked`, `confirmed`, `invalid`, `inconclusive` | Latest completed attempt outcome, or `unchecked` when none exists |
| `--definitive` | `none`, `confirmed`, `invalid` | Last definitive attempt outcome, or `none` when absent |
| `--freshness` | `fresh`, `stale`, `unknown`, `not_applicable` | Existing derived freshness |

Each filter accepts repeated flags and comma separated values. Trim surrounding value whitespace, require exact lowercase tokens, reject empty entries and unknown tokens, and deduplicate. Values within one filter are alternatives; different filters must all match. Omitted filters impose no restriction. Canonical metadata arrays follow the order shown in this table, with `[]` meaning unrestricted. Contradictory valid filters yield an empty success.

`--limit` is an optional positive decimal `int64`; explicitly supplying zero, a negative number, a sign, a nondecimal value, or overflow is invalid. There is no `--all`, offset, public cursor, interactive pager, or selectable ordering. Validate syntax and incompatible flags before reading configuration. Resolve the existing configuration before opening state or creating temporary output.

The application walks candidates in ascending ID order, applies filters, and emits matching rows up to the limit. It then looks for one additional matching row in the same snapshot. That row sets `truncated=true` but is never emitted. Without a limit, or with no further match, `truncated=false`. There is no exact total match count requiring a second full query. Representation checks apply only to emitted rows, not the extra match used to establish truncation.

For example, `--status inconclusive --definitive confirmed --freshness fresh` selects a temporary latest failure with retained fresh confirmation. `--definitive confirmed --freshness fresh` selects fresh confirmations regardless of a later inconclusive attempt. Export never claims that a key can still be activated on another device.

### Data model and wire format

There is no schema migration. Stored candidates, observations, and attempts remain unchanged. Export documents are not saved as database records and are not a backup or restore format.

| Record | Identity and relationship | Retention |
| --- | --- | --- |
| Export document | Contains zero or more candidate summaries | Exists only as prepared output and the requested destination |
| Candidate summary | Existing candidate ID; unique within `items` | Derived from the snapshot |
| Attempt references | Zero or one latest, definitive, and active attempt per candidate; all belong to that candidate | References existing retained attempt records |

JSON is a single UTF-8 object followed by a newline. It uses `schema_version: 1` and these required top level fields. Object field order is not contractual; array order is contractual. New optional fields may be added within version 1; incompatible field meanings, removals, or types require a new version. Consumers may ignore unknown fields.

| Field | JSON type | Meaning |
| --- | --- | --- |
| `schema_version` | integer | Constant `1` |
| `as_of` | string | Fixed UTC freshness evaluation time |
| `max_age` | string | Effective positive freshness interval, in canonical Go duration form |
| `filters` | object | Required arrays `status`, `definitive`, and `freshness`, normalized as above |
| `limit` | integer or null | Explicit limit, or null when unlimited |
| `returned_count` | integer | Number of emitted candidate summaries |
| `truncated` | boolean | Whether an additional matching candidate was omitted because of the limit |
| `items` | array | Candidate summaries, including `[]` for no matches |

Every summary includes the following fields except `key`, which is omitted entirely unless authorized. IDs use exact positive integer JSON numbers backed by Go `int64`; consumers need an integer capable parser rather than rounding through floating point. Counts are nonnegative integers. Times use UTC RFC3339 with stored fractional precision, and absent optional values are `null`, never invented timestamps or empty strings.

| Field | Type | Source and absence rule |
| --- | --- | --- |
| `id` | integer | Candidate ID |
| `first_seen_at`, `last_seen_at` | string | Minimum and maximum saved observation times for the candidate |
| `latest_outcome` | string | Latest completed attempt outcome, default `unchecked` |
| `latest_reason` | string or null | Latest completed attempt reason, null when absent |
| `last_definitive` | string or null | Last definitive outcome, null when absent |
| `evidence_at` | string or null | Last definitive attempt's evidence observation time, including invalid evidence |
| `freshness` | string | Existing freshness enumeration |
| `clock_anomaly` | boolean | Existing candidate freshness anomaly flag |
| `latest_attempt_id` | integer or null | Latest completed attempt ID |
| `definitive_attempt_id` | integer or null | Last definitive attempt ID |
| `active_attempt_id` | integer or null | Existing unresolved attempt ID, not a queued batch item or a promise of live execution |
| `key` | string, conditionally present | Exact stored candidate text, only when `--show-keys` is true |

The latest and definitive references may identify the same attempt. An unresolved attempt does not change `latest_outcome`; a never completed candidate with an active attempt remains `unchecked`. No nested history, source messages, backend details, or raw evidence objects are exported.

Text preserves the current columns `ID`, `LATEST`, `REASON`, `LAST_DEFINITIVE`, `EVIDENCE_AT`, `FRESHNESS`, and `CLOCK_ANOMALY`, plus `KEY` only with `--show-keys`. Preserve tab separators, `none` for absent values, and quoted keys. JSON provides the additional discovery times and attempt references. With zero rows, unfiltered text remains `No candidates saved.`; filtered text is `No matching candidates.`.

Key format writes each emitted key unchanged followed by one LF byte. It has no header, quotes, blank separator lines, or metadata. A selected key containing a Unicode control character (category Cc), U+2028, or U+2029 fails with `export_key_unrepresentable`. Invalid UTF-8 also fails; never replace bytes silently. JSON uses normal string escaping and preserves valid control characters; the store already requires valid UTF-8. `--format keys` is sufficient disclosure authorization. Reject an explicitly supplied `--show-keys` with key format, including `false`, as redundant or contradictory. `--show-keys=false` with JSON or list keeps keys hidden.

### Snapshot and resource ownership

Add a dedicated store snapshot traversal instead of changing `ListCandidates` and its portable page cursors. The new operation accepts an explicit fixed `state.Evaluation` and visits candidates through a synchronous callback, with a normal early stop result for the application's extra match probe. SQL, transactions, rows, and driver errors never escape the store boundary. Preserve callback error identity so output preparation errors do not become misleading state failures. Callbacks cannot reenter the same reader.

The application captures its UTC clock once immediately before starting the snapshot and supplies the effective recheck interval. The first database read establishes the snapshot and its highest candidate ID. `as_of` describes the freshness evaluation, not an exact database commit timestamp. Every candidate and all of its summary queries use the same read transaction. Read IDs in batches of 100, with summaries derived through the existing store logic. Stop on exhaustion, the extra match, cancellation, or failure. The entire transaction ends before prepared output is delivered.

Reuse these freshness rules: no definitive result or a last definitive rejection gives `not_applicable`; confirmed evidence after `as_of` gives `unknown` and a clock anomaly; age at or above `max_age` gives `stale`; younger confirmation gives `fresh`. Attempt recency is based on ID, not timestamps. Inconclusive attempts never refresh or erase definitive evidence. The existing six hour default and configuration precedence remain unchanged.

The application owns query validation, filtering, limits, counters, and traversal. CLI rendering callbacks write only to private staging files during traversal. They never write to stdout, network endpoints, or a slow consumer inside the transaction. The store remains a reader and acquires no writer lock. Its existing cancellation and entered operation ownership rules still apply: do not close or delete a staging file while an entered callback can use it, and do not return control that permits resource reuse until that work has finished.

Memory is bounded by one page and encoding buffers rather than the total result count. Temporary disk use is proportional to prepared output. No new arbitrary count cap, disk quota, or operation deadline is introduced; disk exhaustion is an explicit failure and caller cancellation remains supported. A long preparation can delay SQLite journal reclamation, although ordinary WAL writes remain possible. No database transaction stays open while copying prepared output to stdout.

### Preparation and publication

The in memory lifecycle is validate, prepare, release snapshot, deliver, clean up. No lifecycle record or export history is persisted.

For stdout, create a uniquely named private staging directory under the operating system temporary directory. For named output, create it in the resolved destination parent so publication stays on one filesystem. Directory mode is `0700`; the regular staging file is `0600`, with no broader access even under a permissive umask. No key value appears in temporary names. Do not create missing parent directories or tighten their permissions.

Serialize one row at a time. JSON may write `items` before the final count and truncation fields, so one staging file suffices. Flush encoders and check write errors, then complete the read transaction and close the reader successfully before delivery. If preparation or reader closure fails, discard the staged result without publishing it. For named files, sync and close the prepared file before publication. For stdout, seek the prepared file to its start and copy it with cancellation checks.

Resolve symlinks in both the existing destination parent and the existing state directory to obtain their canonical absolute locations. Compare directory components, not string prefixes, and reject a destination parent equal to or contained within the canonical state directory. Use the canonical parent consistently for staging and publication. Leave the destination's final filename unresolved, so an existing symlink remains an existing destination and produces `export_exists`. This also rejects output through a symlinked parent that aliases the state directory, avoiding accidental creation of a managed database, sidecar, or recovery path.

An early destination existence check is only an optimization. Publication must itself refuse replacement atomically. On the supported local filesystems, use a hard link from the complete staging file to the destination, then remove the staging link. If atomic creation is unsupported, return `export_publish_failed`; never fall back to truncation or replacing rename. A racing file, directory, or symlink at the destination causes `export_exists` and remains untouched.

Successful link creation is the publication point. Sync the destination directory after publication. If that sync fails, retain the complete file and return `export_commit_uncertain`, with `published=true` on stderr; do not claim durability or retry publication automatically. If later cleanup fails, retain the destination and return `export_cleanup_failed` with `published=true`. A process crash around publication may leave a complete destination and a private staging directory; retrying with the same destination refuses replacement. Atomic visibility is promised, not immunity to disk destruction.

Cancellation before publication discards prepared output. Check cancellation immediately before the atomic creation call. If cancellation races with successful publication, retain the destination and report `published=true` with the cancellation result. Once copying to stdout begins, bytes cannot be withdrawn; broken pipes, short writes, or cancellation may leave a partial document or key set. Check cancellation before and after each stdout write, and start no further writes once it is observed. Wait for an entered write to finish before releasing its resources; immediate interruption of an arbitrary blocked stdout writer is not guaranteed. Do not print a success summary after failed delivery.

Cleanup attempts remove only paths created by this invocation, after all users of their handles have stopped. Preserve the primary error if cleanup also fails, and report the remaining temporary directory using safely quoted path text. Cleanup failure after otherwise successful stdout delivery returns failure with `delivered=true`. Abrupt termination may leave private temporary files. There is no startup scan that deletes unrecognized or another invocation's temporary output.

### Diagnostics, security, and compatibility

On successful text or key output, print one completion line to stderr: `returned_count=N limit=none|N truncated=true|false`. For named output, also report the safely quoted absolute destination path and `published=true` on stderr, including for JSON. JSON on stdout already carries completion metadata and needs no additional success line. No summary or diagnostic is inserted into stdout data. Diagnostics and their write errors follow the command's failure behavior; a failure after delivery must not pretend that no bytes or file were delivered.

Use existing argument, configuration, state, cancellation, and `output_error` codes where applicable. New stable codes are `export_destination_invalid`, `export_exists`, `export_key_unrepresentable`, `export_prepare_failed`, `export_publish_failed`, `export_commit_uncertain`, and `export_cleanup_failed`. Error text contains no raw SQL, keys, key fragments, or raw encoder payloads. A key representation failure may identify the candidate ID. Do not log output rows. Caller supplied paths are quoted to prevent terminal control sequences.

Exit 0 means the requested selection and delivery completed, including empty output or an explicitly limited collection. Ordinary errors use exit 1. Existing SIGINT and SIGTERM behavior remains 130 and 143. Missing state is never initialized, and reads never recover pending attempts, contact Telegram, invoke Docker, or validate keys. Private local state permissions remain the authorization boundary; no roles, remote identities, service, telemetry, or credentials are added.

This spec extends the earlier key disclosure rule to `export --show-keys` and `export --format keys`. It deliberately strengthens CLI listing from current details per page to a fixed snapshot. Existing store paging clients, `crawl show`, and `check show` retain their behavior. No stored data migration or backfill is needed; reverting the feature code restores earlier CLI behavior with the same database.

### Value sourcing

| Action | Produced value | Source |
| --- | --- | --- |
| Resolve command | State path and freshness interval | Existing defaults, validated private JSON, explicitly changed common flags |
| Select rows | Filters, limit, format, destination, key disclosure | Parsed invocation flags and defaults specified here |
| Read snapshot | Membership and row order | Candidate IDs visible in the first read transaction, traversed in ascending order |
| Summarize | Candidate ID and optional key | Stored candidate record |
| Summarize | Discovery times | Minimum and maximum saved crawl observation times joined through sightings |
| Summarize | Latest outcome, reason, attempt ID | Highest ID completed attempt; `unchecked` and nulls when absent |
| Summarize | Definitive outcome, evidence time, attempt ID | Highest ID confirmed or invalid attempt and its saved evidence |
| Summarize | Active attempt reference | Unresolved attempt in the snapshot, or null |
| Evaluate | `as_of`, `max_age`, freshness, anomaly | One application UTC clock capture, effective interval, and existing definitive evidence rules |
| Encode collection | Version, normalized filters, limit | Constant version 1 and validated query |
| Complete collection | Returned count and truncation | Successful row emissions and one additional matching row in the same snapshot |
| Deliver | Named output path and publication state | Resolved flag path and actual atomic creation result, never mere file existence |
| Report failure | Codes, optional candidate ID, delivery state | Classified failure and tracked preparation, copy, and publication progress |

### Critical verification scenarios

1. Create 338 controlled unique candidates through existing public store operations, restart, and compare IDs across unfiltered text, JSON, and key export to stdout and files. Confirm exact key bytes, no duplicates, and no hidden 100 row boundary. Covers AC-1, AC-5, AC-11.
2. Build histories for unchecked, confirmed, rejected, inconclusive after confirmation, and unresolved attempts. Check each filter, repeated values, combined filters, exact freshness expiry, future evidence, missing evidence, and a later rejection. Compare equivalent selections across formats. Covers AC-2, AC-5, AC-6.
3. Pause preparation after its first page. In another process insert candidates, complete an attempt for a later candidate, and perform permitted writer recovery. Resume and prove that the entire output retains the initial view, while a new invocation sees commits. Block stdout copying and prove the snapshot has already closed. Covers AC-3, AC-8, AC-11.
4. Exercise limits 1, 100, 338, and 339, plus filters whose first match lies after several pages. Distinguish exactly N matches from N plus one, including no matches. Reject zero, negative, overflow, malformed, and empty inputs before any runtime access. Covers AC-1, AC-2, AC-4, AC-7.
5. Parse JSON from a separate process and assert its field types, nulls, normalized metadata, exact integer IDs, UTC timestamps, and omission of hidden keys. Check that key format authorizes disclosure, and that control characters fail key export without publishing partial results. Covers AC-5, AC-6, AC-7.
6. Inject query, callback, write, flush, seek, sync, and reader close failures, then cancellation during preparation and copy. Confirm no premature delivery, correct exit codes, no leaked live handles, sanitized diagnostics, and truthful partial stdout behavior. Covers AC-8, AC-10, AC-11.
7. Race destination creation against publication with a sentinel file, directory, and symlink. Verify unchanged sentinels and private file modes under permissive umask. Reject a destination parent that aliases the state directory through a symlink, while permitting a sibling directory whose name merely shares its prefix. Inject directory sync and cleanup failures after successful publication and verify the retained complete file and reported publication state. Repeat on both platforms. Covers AC-6, AC-9, AC-10, AC-12.
8. Run help and version with inaccessible configuration and state paths, and test missing, old, new, and damaged state with the read commands. Record runtime verification, then retain meaningful tests and run the repository gate. Covers AC-7, AC-11, AC-12.

## Build plan

Tracer Bullet delivery starts with one complete export path and broadens it through the same layers. The Beta workflow verifies runtime behavior before the separate test workflow.

1. Prove one saved candidate through a new store snapshot traversal, application coordination, version 1 JSON encoding, private staging, reader closure, and stdout delivery after restart. Keep help free of runtime access. Satisfies AC-3, AC-5, AC-8, AC-11.
2. Extend that path to all candidate summaries, filters, bounded internal pages, the extra match probe, empty results, and shared snapshot text listing. Demonstrate all 338 records and exact freshness boundaries. Satisfies AC-1, AC-2, AC-3, AC-4, AC-5, AC-7.
3. Add explicit key disclosure, exact key line output, and named file publication with private permissions and atomic refusal to replace a destination. Satisfies AC-5, AC-6, AC-9.
4. Complete cancellation ownership, staging and delivery failures, completion diagnostics, publication uncertainty, and cleanup behavior. Exercise concurrent writer and destination races. Satisfies AC-3, AC-6, AC-8, AC-9, AC-10, AC-11.
5. Run `/check verify complete results and export` on macOS and Linux arm64 using controlled saved data and actual CLI processes. Record evidence beside this spec, then run `/test complete results and export` and the repository formatting, vet, race, and lint gate. No live source or WARP operation is needed for this read feature. Satisfies AC-1 through AC-12.

## Consequences

You get consistent selection and explicit completeness across terminal and script use, without changing saved evidence. Separate latest and definitive fields prevent a temporary failure from hiding earlier confirmation. Named output cannot quietly destroy an existing file.

Output starts later because preparation completes first. Temporary disk space grows with output, and snapshot preparation can retain SQLite journal pages. Stdout cannot provide atomic delivery. Atomic file publication depends on supported local filesystem operations, and abrupt termination can leave private staging files for manual removal.

## Follow-up

1. After implementation, `/sync` can update the root key disclosure rule and the application and store snapshot guidance to match the verified behavior.
2. Provenance exports, complete history archives, CSV, saved export jobs, source and date filters, overwrite flags, and resumable public cursors remain outside this feature.

## Rationale

Reasoning and alternatives are in [rationale.md](rationale.md).
