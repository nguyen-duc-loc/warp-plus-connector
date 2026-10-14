# 0009. Readable development logs

**Date**: 2026-10-14
**Status**: Accepted
**Content confirmed**: 2026-10-14, after independent review and application of the four recommended fixes. Implementation remains pending.

## Summary

You can follow foreground work and saved service activity through compact local times, clear level labels, readable messages, and quieter supporting fields. Zerolog replaces application use of slog. Command flags select verbosity and presentation, while existing service records, private data rules, and command results retain their contracts.

## Requirements

**User story**: As the developer and local operator, you can see what work started, what finished, and what needs attention, then enable detail to investigate concurrent work.

1. **AC-1**: Foreground `crawl`, `check`, and `run` show major starts, completions, retries, warnings, and failures at the default `info` level. Individual page, attempt, and mutation details require `debug`, except warnings and failures. Successful saved read commands do not gain activity chatter.
2. **AC-2**: Automatic presentation uses readable text on the actual destination terminal and existing structured text when redirected. Explicit pretty, text, and JSON modes work independently of result formatting. Redirected output and saved records contain no terminal styling.
3. **AC-3**: Readable events show local `HH:mm:ss.SSS`, full level labels, and messages. A context line before the first displayed event and whenever its local date or UTC offset changes makes time interpretation explicit. Clock regression never reorders events.
4. **AC-4**: Relevant full run, candidate, and attempt IDs remain visible in readable foreground events at every level where those events appear. Supporting fields are visually quieter when color is available. Warnings and failures remain understandable without color.
5. **AC-5**: `--log-level debug|info|warn|error`, `--log-format auto|pretty|text|json`, and `--color auto|never` are command flags only. Invalid presentation options fail before configuration, state, or external work. Help and version keep their existing isolation.
6. **AC-6**: Final command failures and cancellation use the selected diagnostic presentation, preserve safe diagnostic details and existing exit statuses, and never emit raw errors or supplied invalid values. An unusable diagnostic stream is not written again.
7. **AC-7**: `service logs` supports `--format auto|pretty|text|json`, defaults to `auto`, and renders saved records using their original timestamps and identities. Its snapshot, ordering, follow, gaps, retention bounds, and JSON schema remain those of spec 0008.
8. **AC-8**: Service text presentation applies the selected minimum level after selecting the latest `--lines N` records. It reports selected, displayed, and filtered counts without concealing continuity or retention notices. JSON remains complete and unfiltered. Debug never invents missing historical attempt events.
9. **AC-9**: Results stay on stdout and foreground diagnostics on stderr. Keys, credentials, raw source or vendor content, and arbitrary error chains remain absent at every verbosity. Saved service access retains account ownership and private file checks.
10. **AC-10**: Concurrent writes preserve complete event lines. A short write or formatting failure is latched, cancels active foreground work, and follows existing cleanup and finalization budgets. A failed log reader consumer stops only that reader. Managed diagnostic failure retains spec 0008 behavior.
11. **AC-11**: Application and managed logging use zerolog with no remaining production slog path. Managed capture produces the same permitted service event shapes without persisting formatting, verbosity, or new fields. Existing archives remain readable without migration.
12. **AC-12**: The real CLI demonstrates terminal and redirected behavior, privacy, failure delivery, and archive compatibility on Linux amd64 and arm64. Development on macOS does not expand release support.

## Decision

**Chosen option**: Replace slog throughout the application with zerolog, and share presentation through a small internal diagnostics package.

The implementation target is `github.com/rs/zerolog v1.35.1`, whose console and logger APIs were inspected during design. This is a proposed dependency pin, not a tested repository version or a claim that it is the latest release. Reuse the existing `github.com/mattn/go-isatty v0.0.24` pin for destination detection, promoting it to a direct dependency when imported. No additional logging adapter, terminal UI framework, telemetry service, or background log queue is needed.

This spec belongs to scope feature 11. It extends spec 0008 presentation and replaces the slog implementation choice in spec 0001 for this feature. Spec 0008 continues to govern saved records, file ownership, bounded reads, rotation, follow continuity, and managed shutdown. Its statement that foreground presentation is unchanged is amended only by the presentation contract here. The older specs keep their lifecycle status.

## Feature design

### Ownership and data boundaries

`internal/diagnostic` owns presentation options, terminal detection inputs, level and message mapping, zerolog construction, safe field encoding, serialized output, and failure state. It accepts caller supplied streams, a display location, and a clock. It imports neither Cobra nor `internal/service`, avoiding a dependency cycle.

`internal/cli` owns flag parsing, service entry adaptation, and public diagnostic rendering. `internal/app` retains lifecycle decisions, acknowledgment boundaries, and logging call sites, with an optional `*zerolog.Logger` replacing `*slog.Logger`. A nil application logger disables activity output for embedded callers. `internal/service` retains its archive encoder and supplies a zerolog writer for managed capture. `cmd/wpc` supplies process streams and remains the sole owner of process exit.

Use `zerolog.ConsoleWriter` with explicit formatting callbacks for pretty output. Wrap it with the invocation's date context, sanitization, serialization, and failure handling. Use zerolog's JSON event encoding directly for JSON diagnostics and the compatibility writer for structured text. Decoding at any adapter preserves integer precision, including IDs above 2^53; float conversion of identifiers is forbidden.

There is no database change, new saved entity, foreign key, retention policy, or configuration file field. Transient event fields are typed scalar values and bounded count objects already available at the emitting boundary. They are discarded after delivery. This feature does not turn logs into an alternative source of saved results.

### Command surface

| Surface | Inputs and defaults | Output and failure behavior |
| --- | --- | --- |
| Public command diagnostics | Persistent `--log-level info`, `--log-format auto`, `--color auto` | stderr only; presentation validation precedes application work |
| `crawl`, `check`, `run` | The same flags | Activity and final errors through one invocation diagnostic sink |
| `service logs` | Existing `--lines 100` and `--follow false`; extend `--format` to `auto`, `pretty`, `text`, `json`, default `auto` | Saved entries on stdout; command failures on stderr |
| Other result commands | Existing result `--format` flags | Existing result content and defaults remain unchanged |
| Managed `service exec` | Installed identity and existing bound configuration | Fixed typed archive capture; public presentation flags never alter retention or saved capture |
| Help and version | Existing command behavior | No configuration, state, logger output, account resolution, or network access |

`--log-level` is a minimum event level in this order: debug, info, warn, error. There is no trace or off mode in this slice. Final command failures always reach the error level. Cancellation uses a warning event but bypasses the activity filter so a nonzero signal exit remains explained. No shorthand aliases or application environment settings are introduced.

`--log-format` controls stderr, including errors from `service logs`. `service logs --format` controls stdout, including its service entries. `--color` applies to either stream independently, and `--log-level` additionally filters saved service text. `service logs --format json` accepts the global level flag but does not filter its schema version 1 output. Help explicitly explains that distinction.

Parse presentation values from Cobra flags without reading configuration. An absent flag uses its default; no private JSON setting participates. If parsing stops early, use defaults plus only already parsed valid presentation values for the safe final error. An invalid presentation value uses automatic formatting at error severity without echoing that value. Help and version do not evaluate unused runtime settings.

### Destination and format selection

Resolve each stream separately before wrapping it. Detect a terminal from that stream's original file descriptor through go-isatty. An arbitrary `io.Writer` without a descriptor is nonterminal unless a test supplies explicit capabilities. A redirected stdout must not change diagnostics sent to a terminal stderr, and the reverse also holds.

`auto` chooses `pretty` when the destination is a terminal and `TERM` is not `dumb`; otherwise it chooses `text`. Explicit `pretty` remains readable without color when redirected or under `TERM=dumb`. Explicit `text` and `json` never contain styling or context headers. A nonempty `NO_COLOR`, `--color never`, a nonterminal destination, or `TERM=dumb` disables styling. `--color auto` does not override these conditions. There is no force color escape hatch in this slice.

Foreground `text` retains the established slog text shape even though slog is removed: `time=<RFC3339 with milliseconds and numeric offset> level=<UPPERCASE> msg=<quoted when needed>` followed by structured fields. Use the event time in the process display location. Field quoting follows the former text handler rules for whitespace, equals signs, quotes, backslashes, and control characters. Preserve field names used by current events where their meaning still applies. This is format compatibility, not a promise to keep obsolete messages, severity, or event volume.

Implement that compatibility writer over zerolog records, with a fixed base field order and the supporting field order below. Zerolog's default plain console output is not the compatibility format. No remaining slog handler is used to produce it.

Foreground `json` emits one complete object per line through zerolog: `time` as an RFC3339 UTC string with microsecond precision, `level` as a lowercase name, `message` as safe prose, `event` as a stable internal event name, and applicable typed fields. Run, candidate, and attempt IDs and all counts remain JSON integers. Source, installation, and invocation identities remain strings. Encode timestamps explicitly rather than changing zerolog global time settings. This diagnostic stream has no service archive schema version and is never fed to the service archive reader.

### Readable layout

The readable row order is time, level, message, then fields. Use a six character level column holding `DEBUG`, `INFO`, `WARN`, or `ERROR`. Color only the level label: cyan for debug, green for info, yellow for warn, and red for error. Keep the message in normal terminal text. Use terminal dim styling for the timestamp and supporting fields; reset styling at each segment. Plain mode preserves every word and value.

Supporting fields use this order when present: `run_id`, `candidate_id`, `attempt_id`, `attempt_number`, `source_id`, `page_number`, `channel`, `lane`, `origin`, `status`, `code`, `result`, then remaining approved fields sorted by name. Service provenance follows these fields as full `installation_id`, `invocation_id`, `sequence`, and `stream`. Nested counts render as sorted `counts.<name>=<integer>` fields. No field is shortened or silently removed to fit terminal width. The terminal may wrap long lines; there is no cursor movement, progress animation, truncation, or interactive screen.

Sample layout, using illustrative data:

```text
Date 2026-10-14 | Zone Asia/Ho_Chi_Minh | UTC+07:00
09:41:03.125 INFO   Crawl started run_id=42 origin=manual
09:41:04.216 DEBUG  Source page saved run_id=42 source_id=telegram:warpplus page_number=1 result=completed
09:41:05.700 WARN   Validation retry scheduled run_id=43 candidate_id=81 attempt_id=95 attempt_number=1 code=temporary_error wait=250ms
09:41:08.010 INFO   Crawl completed run_id=42 code=completed saved_candidates=12 saved_sightings=15
```

The header uses the displayed event's local date, the process `time.Local` location name, and that event's numeric UTC offset. Resolve the location once per invocation, allowing the normal Go timezone environment behavior. If its name is only `Local`, the numeric offset remains authoritative. No new timezone flag or external lookup is added. Repeat the header whenever the next displayed event changes date or offset, including backward changes. Use the original service event timestamp, not the time the reader displayed it. Omit a fabricated timestamp on archive metadata or gap notices, whose records carry no event time.

A writer lock covers both any required context header and its event. Do not sort events by wall clock time. Milliseconds are a display choice; full time precision remains in saved records and JSON diagnostics.

### Foreground events and verbosity

Events are emitted where the coordinator already knows the fact. A saved start or completion is announced only after its existing persistence acknowledgment. No new store reads, transactions across network work, retry decisions, or provider calls are introduced for logging.

| Event or existing call site | Level and message rule | Required facts |
| --- | --- | --- |
| Crawl or validation batch admitted | Info, `Crawl started` or `Validation started` | Saved `run_id`; existing origin and selected count when available |
| Saved run completion | Info for completed work without unresolved failure, warn for cancelled, interrupted, or inconclusive work, error for failed work | Saved run ID, status, safe reason and existing summary counts |
| Existing crawl page event | Debug for successful page progress; warn for an individual recoverable source failure; error when it ends the operation | Existing run ID, source ID, page number, result code |
| Attempt admitted or completed | Debug for routine activity, including a definitive invalid candidate; warn for an inconclusive attempt or unsafe cleanup | Existing run, candidate, attempt IDs, attempt number, and safe outcome fields |
| Retry planned | Warn, `Validation retry scheduled` | Existing IDs, typed failure code, and the existing retry delay; never recalculate jitter |
| Mutation admitted or completed | Debug, `Validator mutation started` or `Validator mutation completed` | Existing IDs, operation enum, and safe result fields |
| Scheduler state change | Info for start, stop, and ordinary waiting; warn for clock pause, cooldown, or recovery restriction; error for terminal failure | Existing typed scheduler code; emit once per transition, not every poll |
| Final command error | Error, explanation derived from its safe category | Stable code and permitted delivery or control evidence |
| Signal cancellation | Warn, `Command cancelled` | Existing signal exit status; no invented failed result |

Normal completion messages belong to runs, not every candidate. Promoting a debug event to a warning keeps its IDs visible. The application terminal result on stdout remains authoritative. A run status event and a final command failure may both appear because they describe different facts; the final command error is delivered only once.

The internal discriminator catalogue is `work_diagnostic`, `crawl_started`, `run_completed`, `crawl_page`, `attempt_admitted`, `attempt_completed`, `retry_planned`, `mutation_admitted`, `mutation_completed`, `scheduler`, `command_error`, and `command_cancelled`. Existing generic run diagnostic calls keep the `work_diagnostic` discriminator and their original capture points. Their known `batch_admitted` and terminal crawl codes supply the readable start or completion message. New crawl admission and validation completion messages use `crawl_started` and `run_completed`, respectively, so they do not add archived work diagnostic records. Service entry display is adapted separately and does not pass through managed capture.

Use fixed safe message templates for recognized event types and typed codes. Preserve current foreground attributes such as `active`, `ceiling`, and retry timing at their appropriate event level. Before conversion, accept only explicitly named attributes at each call site. Arbitrary maps, structs, error objects, raw JSON, and formatting of domain records are forbidden. Unknown internal event names or unsupported attribute shapes are formatting failures. Known typed codes without a specialized explanation use a readable category message plus the exact safe code, never upstream error text.

### Foreground field contract

Every event requires the base fields `time`, `level`, `message`, and `event` defined above. The following table is the complete activity field catalogue. Optional values are omitted when unavailable, never replaced with invented zero IDs or empty enum values. Count zero and boolean false remain meaningful values. Decode integers with `json.Number` or an equivalent exact representation, including inside ConsoleWriter callbacks.

| Field | Type and representation | Supplying value |
| --- | --- | --- |
| `run_id` | Positive JSON integer | Acknowledged `report.Run.ID` or the existing `App.log` run argument |
| `candidate_id`, `attempt_id` | Positive JSON integers | `checkJob.candidate.ID` and acknowledged `checkJob.attempt.ID` |
| `attempt_number` | Positive JSON integer | `checkJob.number` |
| `source_id` | String, `telegram:` followed by the normalized channel | Existing `crawlOwned` source ID, also passed to `logPage` |
| `page_number` | Positive JSON integer | Existing crawl page number supplied to `logPage` |
| `origin`, `status` | Strings from the existing run enums | Acknowledged `report.Run.Origin` and `report.Run.Status` |
| `code`, `result` | Strings from typed code catalogues | `App.log` code, `logPage` result, scheduler code, normalized validator `ReasonCode`, or the safe typed error mapper; no arbitrary error text |
| `active`, `ceiling` | Nonnegative and positive JSON integers, respectively | `batchCoordinator.active` and saved batch policy `Workers` |
| `operation` | String, `register` or `attach` | Existing validator admission hook argument; completion need not retain or invent it |
| `elapsed` | Duration string using `time.Duration.String()` in every format, such as `250ms` | Existing coordinator `now().Sub(job.admitted)` or `now().Sub(mutationStarted)` at the completion event |
| `wait` | Positive duration string using `time.Duration.String()` in every format | `BatchAttemptCompletion.RetryWait` after the existing completion write succeeds |
| `outcome`, `failure_class` | Optional strings from `state.Outcome` and `state.FailureClass`; an empty value is omitted | Normalized worker or mutation `validator.Result.Outcome` and `FailureClass` |
| `cleanup_proven`, `removed` | JSON booleans | The same result's `CleanupProven` and `Removed` |
| `saved_candidates`, `saved_sightings` | Nonnegative JSON integers | The acknowledged `CrawlReport` totals |
| Validation count fields | Nonnegative JSON integers, names listed below | Matching `ValidationReport.Totals` members |

Validation count names are exactly `selected`, `pending`, `active`, `completed`, `skipped`, `interrupted`, `confirmed`, `invalid`, `inconclusive`, `unresolved`, and `unstarted`. At run completion, `active` is the saved total; at attempt events it is the coordinator counter. Foreground totals are flat fields. The separate saved service display retains its existing nested `counts` object.

Only these combinations are permitted. Fields outside an event's row fail validation before rendering.

| Event | Required fields beyond the base | Optional fields |
| --- | --- | --- |
| `work_diagnostic` | `run_id`, `code` | `result` as the same code for text compatibility; `origin`, `status`, `selected`, `saved_candidates`, `saved_sightings` when the existing capture point has an acknowledged report |
| `crawl_started` | `run_id`, `origin` | None |
| `run_completed` | `run_id`, `origin`, `status`, all validation count fields | `code` from saved run reason |
| `crawl_page` | `run_id`, `source_id`, `page_number`, `result` | None |
| `attempt_admitted` | `run_id`, `candidate_id`, `attempt_id`, `attempt_number`, `active`, `ceiling` | None |
| `attempt_completed` | The attempt admitted fields plus `elapsed`, `cleanup_proven`, `removed` | `outcome`, `failure_class`, `code` |
| `retry_planned` | `run_id`, `candidate_id`, `attempt_id`, `attempt_number`, `wait` | `code`, `failure_class` from the acknowledged completion |
| `mutation_admitted` | `run_id`, `candidate_id`, `attempt_id`, `operation` | None |
| `mutation_completed` | `run_id`, `candidate_id`, `attempt_id`, `elapsed`, `cleanup_proven`, `removed` | `outcome`, `failure_class`, `code` |
| `scheduler` | `code` | None |
| `command_error` | `code` | Only the final diagnostic fields defined below |
| `command_cancelled` | `exit_code` as integer 130 or 143 | None |

Worker and mutation completion fields describe normalized observations, not acceptance into saved entitlement history. They use the current coordinator event result. Empty outcome with a typed error, inconclusive outcome, or unproven cleanup is a warning; routine confirmed or invalid observations with proven cleanup remain debug. Persisted run completion and retry announcements still require their existing acknowledgment boundaries. Do not log `Evidence`, `ServerDelay`, raw callback errors, or the whole result object.

Omit the existing `session` attribute from all new foreground logs, including debug. Attempt IDs already correlate this activity. Session IDs remain in their existing private recovery records; this change does not delete or alter recovery data. Do not add backend identities, container identifiers, arbitrary paths, or configuration values to activity events.

Final diagnostic fields are a separate closed set: `candidate_id` from `deliveryError.candidateID`; `published`, `delivered`, `output`, and `temporary_directory` from the values already exposed by `deliveryError.diagnostic`; `changed` and `operation_incomplete` from `service.Error`; and `action`, `changed`, `installation_id`, and `state` from `controlDeliveryError.result`. The booleans remain booleans, candidate ID remains a positive integer, and the remaining values remain strings. Preserve the current inclusion conditions, including output paths only after publication. The existing omit output hint remains fixed safe message text. No field is obtained by splitting the rendered diagnostic string.

### Final diagnostic handling

Move presentation out of the direct `fmt.Fprintln` calls in `cmd/wpc`. Main passes the execution error or signal result to a CLI renderer that reuses the invocation sink. Keep `cli.Diagnostic` as the safe compatibility boundary or replace it with an equivalent typed helper while preserving its public behavior.

Translate typed app, configuration, validator, service, and output errors before logging. Preserve `published=true`, `delivered=true`, control action, changed state, and ownership evidence currently permitted by diagnostic types. Do not parse an arbitrary `Error()` string into fields. Parser error categories and unknown errors retain the existing sanitized explanation. Unknown errors use `command_failed` with `Command or output failed`, not an exception dump. Configuration categories lacking a code use `configuration_invalid` or `configuration_access`; malformed CLI options use `arguments_invalid`. Existing typed codes take precedence over these fallback categories.

The public process returns the same statuses: ordinary error 1, SIGINT 130, SIGTERM 143, and the existing managed lifecycle result. Presentation never calls zerolog `Fatal` or `Panic`, and never exits a process. Existing `DiagnosticFailed` suppression, including doctor report delivery, remains effective.

### Service display and filtering

The saved archive remains spec 0008 schema version 1. Read it through `ReadLogs` and `FollowLogs`. CLI rendering adapts accepted entries into the common readable layout without rewriting files. No application configuration, candidate database, or Docker call is needed to render them.

| Saved event | Display message | Derived level |
| --- | --- | --- |
| `runner_phase` | `Service <status>` | Error for failed, warn for clock_paused, otherwise info |
| `lane_phase` | `<Crawl or Validation> <status>` | Warn for `unknown` or `clock_paused`; debug for `idle` or `disabled`; otherwise info; a code present on this same entry can raise the level as specified below |
| `work_diagnostic` | `Work update` with code, or a known code explanation | Exact code classification below |
| `crawl_result` | `Crawl <status>` | Error for `failed`, warn for `cancelled` or `interrupted`, otherwise info |
| `source_result` | `Source <status>` | Error for `failed`, warn for `cancelled`, `interrupted`, or `skipped`, otherwise info |
| `validation_result` | `Validation <status>` | Error for `failed`; warn for `cancelled`, `interrupted`, or positive inconclusive, unresolved, or unstarted totals; otherwise info |

The accepted saved statuses and code vocabulary come from `validateEntry` and `validCode` in `internal/service/logs.go`, together with the state enums they validate. No `aborted` saved status is introduced. A completed validation containing only confirmed or invalid candidates is ordinary info. Result status and totals determine severity directly; do not override a cancelled result with a more severe interpretation of its reason. Corrupt archive data is a read error, not an unknown event to render.

For `work_diagnostic`, classify the exact saved code using these disjoint rules:

| Level | Codes |
| --- | --- |
| Info | `batch_admitted`, `running`, `completed`, `preview_end`, `page_limit` |
| Warn | `cancelled`, `interrupted`, `interrupted_before_start`, `deadline_exceeded`, `clock_paused`, `cleanup_pending`, `attempt_limit`, `candidate_timeout`, `backend_unavailable`, `backend_busy`, `cooldown`, `clock_regressed`, `probe_required`, `rate_limited`, `temporary_error`, `timeout`, `source_rate_limited`, `source_timeout`, `state_busy`, `validation_batch_inconclusive`, `validation_batch_stopped`, `validation_cleanup_pending`, `validation_interrupted`, `validation_observation_timeout`, `validation_rate_limited`, `validation_timeout`, `validator_backend_unavailable`, `validator_busy`, `validator_clock_regressed`, `validator_cooldown`, `validator_probe_required` |
| Error | Every other code accepted by the saved catalogue, including `failed` |

Foreground `work_diagnostic` uses the same classification. Final `command_error` still uses error severity regardless of its code. The foreground scheduler additionally classifies `schedule_starting`, `schedule_resumed`, `schedule_stopping`, `schedule_stopped`, and `schedule_waiting` as info; all other scheduler codes use the code table above. Added lifecycle codes describe the coordinator's actual start, transition into ordinary idle waiting, stop request, and completed shutdown. Emit waiting only on entry to the idle state, not on each scheduling poll.

For `lane_phase`, a nonempty code on that same saved entry raises the baseline to the greater of its baseline and the code table level. Empty code leaves the baseline unchanged. Current lane records may contain only lane and status; show exactly those facts. Never obtain a missing reason from a neighboring event, the current scheduler, or a fresh state read. Runner phases and result events retain their table classification without an inferred reason.

Finite reads select the latest N complete records before filtering. JSON retains all selected entries and the exact existing response fields. Pretty and text retain `requested_lines`, `returned`, `older_available`, `truncated_tail`, and `continuity_unknown`, and add display metadata `displayed` and `filtered`, where `returned = displayed + filtered`. `returned` continues to mean selected archive records. These new display counts do not enter JSON or saved files. Explicit `text` retains flattened field syntax and original selected entry indices so filtering cannot imply a different saved selection.

A pretty snapshot starts with a plain metadata summary, followed by the displayed entries. No files gives `No service log entries` plus zero counts. If selection exists but the filter removes everything, show `No entries at this level` with the selected and filtered counts. Retention and continuity notices remain visible at all levels and precede affected entries. They never claim proven loss when continuity is only unknown.

Follow preserves snapshot, entry, and gap order. Apply the same entry filter after delivery from the existing follower, but always render gap envelopes. Advance the reader position even for hidden entries. Snapshot counts describe the initial finite selection only; do not accumulate an unbounded session history or invent a final follow count. A new invocation retains its original identity and existing continuity notice. JSON follow remains the existing sequence of complete envelopes with no inserted human headers.

### Managed zerolog capture

Replace `managedHandler` with a zerolog writer owned by `internal/service`. Continue emitting the existing logical work diagnostic at exactly the existing capture points. Use a stable internal event discriminator `work_diagnostic` with the acknowledged `run_id` and safe `code`; migrate the old `run` message and `result` attributes together with its receiver. This avoids relying on human message wording to identify archived events.

The capture writer maps only that discriminator into the existing `work_diagnostic` service event. Its fields must be validated through the service catalogue. Other explicitly known foreground events are ignored by managed capture, as current attempt and page activity already is. A malformed work diagnostic or an unknown discriminator fails the diagnostic sink. Runner phases, lane phases, and result summaries still use their existing typed service emitters, once each.

Managed capture has no user level filter, sampler, or pretty writer. It must capture a work diagnostic even if a foreground equivalent would be hidden at the selected level. IDs, sequence allocation, archive timestamps, and allowed codes retain the service writer's authority. Zerolog JSON is a transient transport between the application logger and capture writer; it is never copied directly into the archive. Candidate IDs and new attempt events remain absent from saved records.

### Failure, concurrency, and privacy

Use one serialized sink per output stream. Each sink retains the first formatting or delivery failure and refuses later writes. The lock includes record formatting, date context tracking, and delivery. Bound foreground encoded records to 16 KiB; exceeding the bound fails the diagnostic sink rather than truncating or leaking raw data. Service archives retain their existing independent 16 KiB record bound.

Zerolog event calls do not provide the application's normal error return path. The adapter atomically latches the first writer or formatter error and marks its cancellation notification claimed while holding the output lock. It releases the lock before invoking that callback, exactly once. The callback only triggers cancellation; it does not log, publish observations, or perform cleanup synchronously. Existing application and service control flow performs cleanup after cancellation. This ordering prevents a callback or managed failure path from reentering a locked sink.

The outer zerolog writer acknowledges the encoded input after latching a failure, preventing zerolog's fallback error printer from writing an uncontrolled message to process stderr. Command completion checks the latched state and returns the existing `output_error` category with `DiagnosticFailed` suppression. This intentional acknowledgment is internal to logging and never reports a successful command or a persisted result.

Managed capture uses the same containment principle while invoking the existing managed diagnostic failure path. Service display writes return errors normally to the reader callback. A broken stdout consumer cancels only `service logs`; it never stops the service. Foreground cleanup and managed watchdog windows remain those of the existing coordinator and service contracts. Do not add a queue, goroutine per event, sampling, retries on a broken output stream, or global zerolog state mutations.

Escape control characters in all displayed dynamic strings before rendering. JSON encoding alone does not protect a terminal once a console writer decodes those strings. Foreground values originate only from approved primitive fields; service values have already passed the strict reader. Display quoting escapes newlines, carriage returns, tabs, ESC, and other control bytes, preserving one physical event line. Only the renderer introduces ANSI sequences, and only under the color rules. Never enable stack traces, raw error serialization, credentials, license values, or provider output at debug level.

### Value sourcing

| Value | Source |
| --- | --- |
| Foreground event time | Injected diagnostic clock sampled at the actual event boundary |
| Historical event time | Saved service entry `at`, never reader time |
| Local date, zone, numeric offset | Event time converted through the invocation's resolved `time.Local` |
| Pretty or structured mode | Explicit format flag, otherwise capabilities of the actual destination |
| Styling | Color flag, destination terminal status, nonempty `NO_COLOR`, and `TERM` |
| Severity and message | Event and typed status/code tables in this spec |
| Run, source, candidate, attempt identities | Existing acknowledged coordinator records; absent facts are omitted |
| Retry timing and worker counts | Existing batch policy, saved retry decision, and coordinator counters |
| Completion counts and status | Existing saved report or acknowledged terminal summary |
| Service identity, sequence, stream, origin, lane, counts | Validated saved entry fields under spec 0008 |
| Selection and retention metadata | Existing finite archive view or follow envelope |
| Displayed and filtered counts | Level predicate applied to the already selected snapshot entries |
| Final diagnostic details | Typed safe diagnostic mapping, including publication and delivery evidence |
| Cancellation and exit result | Existing process signal state and command execution result |

### Critical verification scenarios

1. Run a controlled crawl and concurrent validation through the CLI, then compare info and debug output with saved outcomes and unchanged stdout. Confirm page and mutation details are hidden at info and retries remain visible. Covers AC-1, AC-4, AC-9, AC-11.
2. Exercise a real terminal, each independently redirected stream, explicit formats, `NO_COLOR`, `TERM=dumb`, and an arbitrary writer. Check exact safe quoting, full level names, integer run and attempt IDs above 2^53, string source identities, duration strings, and omitted session fields. Covers AC-2, AC-4, AC-5, AC-9.
3. Render injected events across midnight, an offset change, and backward time, including old service records. Check context headers and unchanged append order. Covers AC-3, AC-7.
4. Read and follow existing archives at every level, including empty selection, all filtered entries, gaps, invocation changes, and archive rotation. Exercise every accepted status, every explicit info or warning code, and the error fallback. Check cancelled and interrupted runs and sources, and lane entries with no saved reason. Compare JSON with the unchanged service contract. Covers AC-7, AC-8, AC-11.
5. Exercise invalid flags, unavailable configuration, typed service errors, completed export publication followed by delivery failure, cancellation, and unknown errors. Verify safe diagnostics and exit statuses without changing help or version isolation. Covers AC-5, AC-6, AC-9.
6. Inject concurrent writes, short writes, formatter failure, a broken reader consumer, and managed archive failure. Prove a single cancellation callback after unlocking, cleanup ownership, no fallback stderr, and no second write to a failed stream. Covers AC-6, AC-10, AC-11.
7. Seed synthetic keys, credentials, and control characters into rejected inputs and upstream error fixtures. Inspect all output modes and archive bytes at debug level. Execute appropriate runtime proof and the repository gate on both Linux targets. Covers AC-9, AC-10, AC-12.

## Build plan

The project uses Tracer Bullet delivery and the Beta workflow. Each slice proves a working path before expanding coverage. No schema migration is needed.

1. Prove one crawl end to end through the new zerolog logger, flags, destination selection, safe final error rendering, and unchanged stdout. Include plain redirected compatibility and one broken diagnostic write. Covers AC-1 through AC-6, AC-9, AC-10, AC-11.
2. Extend the same path to concurrent checks and the foreground runner. Migrate the remaining slog callers, establish explicit event and code tables, add verbosity transitions and complete cancellation integration. Covers AC-1, AC-4, AC-6, AC-9 through AC-11.
3. Prove managed zerolog capture through an existing service archive and its public reader, then add readable finite and following display, level filtering, and retained metadata. Covers AC-3, AC-7 through AC-11.
4. Verify real CLI behavior, time transitions, privacy, and failure boundaries on Linux amd64 and arm64, then add meaningful regressions from those observations and run formatting, vet, race tests, and lint. Record the tested dependency version and evidence. Covers AC-1 through AC-12.

## Consequences

Readable terminals gain clear progress and consistent failure messages. Explicit text and JSON options keep scripting practical. The service schema remains independently inspectable after a binary rollback.

The selected full migration costs more than keeping slog behind a formatting adapter. Preserving the old structured text shape requires a compatibility writer, and event severity changes mean scripts must not depend on the old default volume. Selecting N records before filtering can display fewer than N; counts make that visible. Service history cannot expose details it never stored.

This can ship as one code change with a reversible binary rollback and no data conversion. Prove the thin path before replacing all call sites; do not temporarily write duplicate archive events. Removing the new binary also removes its new flags, which should be considered when reverting local command scripts.

## Follow-up

After implementation, durable project context should record the tested zerolog dependency and replace the old slog guidance. Agent context changes belong to the normal sync workflow. Terminal overview and interactive controls remain separate scoped features.

## Rationale

Reasoning and alternatives are in [rationale.md](rationale.md).
