# Operational diagnostics

## Summary

You can inspect the service even when configuration, Docker, or candidate state is unavailable. Status combines separately timed process observations with one consistent view of saved work. Private bounded logs and local diagnostic checks explain failures without changing work or revealing keys.

## Decision

Keep only the latest managed runner observation outside SQLite. Derive collection and run summaries from the existing database when you ask for status. Retain structured diagnostic entries in a fixed size private archive.

## Feature design

### Latest observation

`observation.json` is an atomically replaced private schema version 1 record. The lifetime lock owner is its only writer. It uses a fresh invocation ID each start and the installation ID from the ready manifest. Old records may remain after exit or uninstall, but never describe a later invocation merely because a PID matches.

| Field | Type and rule |
| --- | --- |
| `schema_version` | Integer, exactly 1 |
| `installation_id`, `invocation_id` | Required 32 character lowercase hex identifiers |
| `pid` | Required positive integer, informational only |
| `started_at`, `updated_at` | Required UTC timestamps with microsecond precision |
| `phase` | `starting`, `running`, `clock_paused`, `stopping`, `stopped`, or `failed` |
| `crawl_enabled`, `validation_enabled` | Nullable booleans, null until config resolution succeeds |
| `crawl_phase` | `unknown`, `disabled`, `idle`, `crawling`, `clock_paused`, or `stopping` |
| `validation_phase` | `unknown`, `disabled`, `idle`, `acquiring`, `checking`, `waiting`, `clock_paused`, or `stopping` |
| `policy` | Nullable object with canonical Go duration strings `crawl_interval`, `recheck_interval`, `failure_interval`, and integer `workers`; set after config validation |
| `stopped_at` | Nullable UTC timestamp; set only when this invocation actually reaches its terminal managed path |
| `failure_code` | Nullable stable code; required for `failed`, null for normal operation |
| `app_exit_code` | Nullable integer; records the corresponding foreground outcome, independently of managed native exit 0 |

This is the confirmed latest observation model with explicit serialization fields. Nullable startup fields prevent a broken configuration from appearing as two intentionally disabled lanes. No run counts, candidate values, environment dump, or raw error text belongs here.

Publish `starting` before configuration resolution, `running` after writer acquisition and schedule preparation, `clock_paused` when the scheduler pauses admission, and `stopping` before cancellation cleanup. A completed normal stop becomes `stopped`; a handled fatal result becomes `failed`. Any active phase may end abruptly without a final write. A restart replaces the record with a new invocation and does not invent a stop time for the previous one.

The application exposes a small typed observation callback for managed execution. The coordinator produces lane transitions; worker goroutines never publish directly. The service serializes phase and heartbeat writes through one publisher so an older heartbeat cannot replace a newer phase. Schedule waits and policy values come from the actual runner, not current file contents read by a separate process.

Write on each phase change and every 15 monotonic seconds while alive, including during startup and stopping. Use atomic replacement with file and parent directory synchronization. Each publication has a five second observation deadline; a publication during shutdown uses the earlier of that deadline and the existing local finalization deadline. Reject malformed, oversized (more than 64 KiB), duplicate field, unknown field, version, permission, and identity errors when reading. Any write uncertainty marks that sink failed, blocks later observation writes, and cancels managed work. Other sinks and remote cleanup remain independent.

Use one serial publisher with at most one entered filesystem operation and one coalesced pending observation. A deadline stops waiting and triggers cancellation, but cannot claim that a filesystem syscall was cancelled. The entered operation retains its buffers, descriptors, and lifetime ownership until it actually returns or the managed process terminates. Do not publish a terminal record over an unresolved write, release the lifetime lock early, or let an old completion authorize another write. Ignore its late acknowledgement for admission; its file may have become visible and remains a last observation only. Remote cleanup can proceed independently. A clean process return waits for entered operations, subject to the managed 75 second watchdog defined in the service child. Observation timestamps are sampled before publication, never changed to the time of a late acknowledgement.

Readers read the record before sampling its `observed_at` clock. `stale=true` when `observed_at - updated_at > 60s`. Equality is still fresh. A future `updated_at`, invalid timestamp order, or backward time produces `clock_anomaly=true` and unknown freshness, never fresh. The heartbeat is evidence of a recent loop observation, not proof of useful work or remote readiness.

### Status schema

`service status --format json` returns one object with `schema_version: 1`, `view: "service_status"`, `observed_at`, `complete`, and four components: `installation`, `manager`, `runner`, and `work`. Each component contains `availability` (`available`, `missing`, or `unavailable`), `observed_at`, nullable `error_code`, and nullable `data`. `complete` is false exactly when a component is unavailable. A known absent installation or database is `missing`, not a fabricated empty record. Missing and unavailable components never prevent other components from being inspected safely.

| Component data | Fields and sources |
| --- | --- |
| `installation` | `installation_id`, `manager`, `service_name`, `phase`, `operation`, `autostart_enabled`, and bound executable, config, state, and Docker paths, read from the validated manifest |
| `manager` | `loaded`, `running`, `pid`, `last_exit_code`, `last_signal`, `active_state`, `pending_job`, `autostart_observed`, and `identity` (`verified`, `absent`, or `unverified`), from the service child's native checks; each unsupported value is null |
| `runner` | `lock_held`, `owner_pid`, `installation_matches`, `stale`, `clock_anomaly`, and `observation` containing the latest validated record; `owner_pid` comes from the lifetime lock query, and missing lock or observation has explicit null fields |
| `work` | `as_of`, `schedule`, `outcomes`, `latest_crawl`, `last_terminal_crawl`, `latest_validation`, and `last_terminal_validation`, all from one database snapshot |

Status does not merge these components into a single health promise. Text uses labeled sections and explicit `unknown`, `unavailable`, `stale`, and `none` values. It shows the manager's process state beside the last observed runner phase. A held lock with a stale record is reported as held ownership with stale progress, not a healthy runner. A free lock with a retained failed observation is reported as stopped with the saved failure. Manager and lock disagreement remains visible. If the observation's installation ID differs, show it as retained history and exclude its policy from current interpretation.

The Linux adapter can return incomplete native fields while private observations and saved work remain useful. Managed startup intent remains distinct from observed systemd enablement. If native enablement cannot be observed reliably, `autostart_observed` is null. Doctor reports that limitation without fabricating agreement. macOS manager inspection is outside this contract.

Never read the current configuration file to locate installed work or render saved policy. Status uses the manifest's state directory. It can inspect an incomplete manifest's state binding when identity and path validation succeeded. With no manifest, `work` is unavailable because there is no authoritative installed profile; retained observation and logs remain inspectable. Existing `list` and `schedule show` still inspect arbitrary profiles.

### Saved work snapshot

Add one store aggregate read over a single read transaction. Establish the SQLite snapshot by reading `scheduler_state`, then sample the supplied UTC clock and normalize to microseconds. Reuse existing query logic within that transaction rather than calling independent public readers. Close the reader before prepared output delivery. Reads never acquire writer or validator ownership.

`work.schedule` is the existing saved schedule summary from spec 0007, including `checkpoint_at`, `clock_regressed`, `validation_recovery_pending`, saved crawl plan, candidate counts, and local validation wait. Its `as_of` equals `work.as_of`. It is embedded without a second top level schema or `view`. The saved crawl's due time and candidate `next_due_at` are eligibility, not promises that an enabled lane, available Docker backend, or admitting provider gate will execute then.

`work.outcomes` contains `total`, `unchecked`, `confirmed`, `invalid`, and `inconclusive`. Use each candidate's latest completed attempt under the existing summary rules; a candidate without one is unchecked. An unresolved attempt does not erase a prior completed outcome. Thus the four outcome counts sum to total. Separately expose `definitive_confirmed`, `definitive_invalid`, and `definitive_none` from the last definitive attempt, preserving the distinction after later inconclusive checks. This view adds no freshness categories or new freshness policy.

Queue depth is exactly `schedule.candidates.due`: candidates due at `as_of` excluding pending or active items in a running batch. `active` retains spec 0007's reserved candidate meaning, not the number of Docker workers. `waiting` contains the remaining candidates, and `total = due + active + waiting`. Display these labels explicitly. Keep runtime lane enablement and saved eligibility separate when stopped, disabled, stale, or blocked.

Select `latest_crawl` and `latest_validation` by greatest run ID of their respective kind, including an active run and either origin. Each has `run_id`, `origin`, `status`, `started_at`, nullable `finished_at`, nullable `recovered_at`, nullable `reason_code`, and `clock_anomaly`. Separately select `last_terminal_crawl` and `last_terminal_validation` by greatest ID of each kind whose status is not running. Return null when no qualifying run exists. Each terminal value has the complete corresponding aggregate described below, including sources or validation failures, not only a header. When latest and last terminal are the same run, both values contain the same aggregate. A new active run therefore does not hide the previous run's failure details. All four selections use the same database snapshot.

The crawl adds `saved_candidates`, `saved_sightings`, and `sources` in saved configuration order. Each source contains `channel`, `status`, nullable `reason_code` (saved `stop_reason`), `saved_candidates`, and `saved_sightings`. Return every saved source, up to the configured maximum 32. Legacy history without a source plan has `sources: null`, not an invented list.

The validation adds nullable `totals`, with the existing `ValidationTotals` fields encoded in snake case, and `failures`, sorted by code. Each failure object has `code` and `count`. Count at most one reason per selected item: its terminal stop reason when present, otherwise its latest completed attempt's reason. Do not count a healthy completed item as a failure merely because an earlier retry failed. Pending and active items are not failures. Legacy history without a batch policy has null batch totals and failures, with its saved run header still shown. No candidate IDs or keys are needed in aggregate status.

A database that has not been initialized is `missing`. A version mismatch, invalid permissions, corruption, or read error is `unavailable` with the existing stable code. No empty totals are substituted. Bound the entire work read to five seconds; a timeout reports unavailable, with no partial database aggregate. Use indexed aggregates and bounded source arrays rather than collecting candidate records in memory. Full collection semantics remain unlimited.

### Structured log archive

Store one newline delimited JSON stream in `logs/current.jsonl`, with four archives `logs/1.jsonl` through `logs/4.jsonl`, newest archive first. Each file is at most 4 MiB. Before a complete record would exceed the current file's limit, discard the oldest archive, shift the remaining archives, and rename current to archive 1. Open a fresh current file privately. Never split a record. No compression, extra archive, unbounded temporary copy, or database logging is introduced.

The exact rotation order is remove archive 4 if present, rename 3 to 4, 2 to 3, 1 to 2, and current to 1, then exclusively create current. Skip absent source slots. Flush and close current before rotation. Synchronize the directory after each successful removal or rename; synchronize the new current file and directory before appending. A failure stops further archive mutations in that invocation. Crash created holes are accepted in their numeric order, never filled by inventing records or reordering by timestamps. If current is absent on recovery, create it privately after inspecting the remaining files. Do not replay a partially completed rotation from an assumed step.

Each record contains `schema_version: 1`, `installation_id`, `invocation_id`, a positive increasing `sequence` within that invocation, `at` (UTC microsecond timestamp), `stream` (`result` or `diagnostic`), and `event`. Event fields are closed by the catalogue below. Unknown fields, formatted error chains, arbitrary messages, paths from upstream output, and raw stream contents are not accepted by the log encoder. Encode known typed runner summaries and slog attributes through this boundary. Routine text stdout and stderr behavior stays unchanged for public foreground commands.

| Event | Stream and required event fields | Optional fields | Source |
| --- | --- | --- | --- |
| `runner_phase` | `diagnostic`, `status` from the observation phase enum | `code`, required for failed phase | Acknowledged phase transition; ordinary heartbeat writes produce no log |
| `lane_phase` | `diagnostic`, `lane` (`crawl` or `validation`), `status` from that lane's phase enum | `code` for a typed pause or wait | Coordinator transition and its typed reason |
| `work_diagnostic` | `diagnostic`, `run_id`, `code` | None | Existing application run diagnostic after translating its typed result; no arbitrary slog message is accepted |
| `crawl_result` | `result`, `run_id`, `origin`, `status`, `counts` | `code` from the saved run reason | Acknowledged saved terminal crawl report |
| `source_result` | `result`, `run_id`, `channel`, `status`, `counts` | `code` from saved source stop reason | Each saved source in that terminal crawl, in saved order |
| `validation_result` | `result`, `run_id`, `origin`, `status`, `counts` | `code` from the saved run reason | Acknowledged saved terminal batch report |

`crawl_result` and `source_result` counts contain exactly `saved_candidates` and `saved_sightings`. `validation_result` counts contain exactly `selected`, `pending`, `active`, `completed`, `skipped`, `interrupted`, `confirmed`, `invalid`, `inconclusive`, `unresolved`, and `unstarted`, derived from `ValidationTotals`. All counts and IDs are JSON integers; counts are nonnegative and IDs positive. Only result events carry counts. Origin uses the existing `manual` or `scheduled` enum; result status uses the corresponding saved run or source status. Codes come only from the existing typed reason enumerations or the new explicit service codes in this spec. No per candidate log event is added, and candidate IDs are not part of this log schema. A new event or field requires an explicit schema contract update before emission.

Maximum encoded record size is 16 KiB. An oversized or unsupported record is a diagnostic failure, not a reason to silently truncate data. Assign sequence numbers before serialization. Flush complete records synchronously; disk synchronization on every ordinary log entry is unnecessary, so a crash may lose the last buffered filesystem data. These logs are diagnostic evidence, not a durability substitute for SQLite or the validator registry. The managed runner remains the sole archive writer under its lifetime lock. Native manager startup or crash diagnostics remain in the host's native facilities and are outside this application's archive budget.

After a crash during rotation, inspect only the known five file names. Missing slots are allowed; malformed regular files, incorrect permissions, symlinks, or duplicated sequence identities are errors. A trailing incomplete current record can be ignored by readers, with an explicit truncated tail indication. Before appending after restart, the new owner truncates only that incomplete tail to the last newline. Complete malformed entries are never silently discarded or repaired. The archive budget is measured over these five files and never exceeds 20 MiB, including during rotation. File errors stop the runner through normal cleanup.

`service logs` reads at most the five bounded files. It can work after uninstall. No files means a successful empty result. The finite JSON form has `schema_version: 1`, `view: "service_logs"`, `requested_lines`, `returned`, `older_available`, `truncated_tail`, `continuity_unknown`, and `entries`. Return the latest N complete entries in chronological append order, oldest first, using file order and sequence rather than sorting wall clock timestamps. `continuity_unknown` is true when the returned range crosses archive holes, a sequence discontinuity, or an invocation boundary. It does not assert that records were certainly lost. Text exposes equivalent retention metadata and entries. Prepared output closes source files before delivery.

For a finite read, capture file identities and byte lengths, open matching descriptors, and read only those byte ranges. Recheck archive identities before accepting the result; growth of the same current file after its captured boundary is harmless. A rotation during discovery or collection retries the whole read, at most three times within a five second total budget. Exhaustion returns `service_log_busy` without partial output. Detect duplicate invocation and sequence pairs rather than emitting duplicates. `older_available` means additional complete entries exist in that accepted archive view, not that discarded history can be recovered.

With `--follow --format json`, emit newline delimited envelopes, never a single unclosed JSON array. Each has `schema_version: 1` and `type`: `snapshot` with the finite response fields, `entry` with one record, or `gap` with `reason` (`retention` or `continuity_unknown`). Poll at most once per second; identify open files by file identity and retain invocation and sequence positions. Reopen after rotation or a new invocation. Do not hold the writer's locks. Proven disappearance of unread bytes or skipped sequence numbers emits a retention gap. An invocation change, archive hole, or unreconciled identity emits a continuity unknown gap before later entries, even if no loss is proven. This deliberately covers an entire intervening invocation that has disappeared before the reader sees it; per invocation sequence numbers cannot prove continuity across starts.

After a gap, resume from the earliest available unread complete entry. Deduplicate by installation ID, invocation ID, and sequence, retaining only keys from the current five file view plus the last delivered position so a long follow does not grow an unbounded history set. The snapshot's continuity flag covers gaps already inside its selected range; later gap envelopes cover transitions after that snapshot. Cancelled following closes its own readers and uses existing signal exit codes. It does not stop the service. A broken consumer ends that log command only.

### Doctor

The report has `schema_version: 1`, `view: "service_doctor"`, `observed_at`, `ok`, and `checks`. Each check has `name`, `status` (`pass`, `warn`, `fail`, or `skip`), `code`, `observed_at`, and a fixed safe `suggested_action`. `ok` is false if any check failed. All applicable checks run within a shared 20 second budget; individual native or Docker calls have five second deadlines and 1 MiB output limits. Dependent checks become `skip` after missing prerequisites; budget exhaustion is a failed timeout check. No raw command output enters the report.

| Check | Read and result |
| --- | --- |
| `installation` | Parse manifest, account identity, version, phase, and bound paths; an incomplete operation fails with the action to repeat |
| `definition` | Verify private native file bytes and SHA256 against the manifest; drift is a failure, never repaired |
| `executable` | Check the bound executable is still a regular executable and not writable by other accounts |
| `configuration` | Resolve the chosen file under the saved context, validate run settings and compare bound paths; never apply changed settings to work |
| `manager` | Query the current native user manager; unavailable is a failure; unverifiable enablement is a warning |
| `runner` | Report lock and observation agreement, stale or anomalous observations, and failed phase; intentionally stopped is informational and passes |
| `state` | Open a read only current schema reader; never migrate or recover; missing state before the first run is a warning, incompatible or damaged state is a failure |
| `logs` | Read permissions, known file identities, record framing, and aggregate size; perform no write probe or rotation |
| `docker` | When effective validation is enabled and bound paths are valid, call the existing local daemon and immutable image inspection checks through the saved Docker executable and socket; no registration or container commands |

Use the following condition table for findings. A check may produce several entries with the same `name`; sort findings in the table's order. Emit one `pass` with `code=ok` and `suggested_action=none` only when a check has no warning, failure, or skip. `suggested_action` is one of the literal values in the table, rendered as a fixed explanation in text. A matching row is not hidden by a later passing probe.

| Condition | Check and status | Code | Suggested action |
| --- | --- | --- | --- |
| Effective account cannot be resolved | `installation`, fail | `service_account_unavailable` | `repair_account_lookup` |
| No installation manifest and no unsafe fixed native conflict | `installation`, warn | `service_not_installed` | `install_service` |
| Invalid permissions, identity, version, or record encoding | Affected check, fail | Existing stable access or format code | `inspect_private_files` |
| Manifest operation is incomplete | `installation`, fail | `service_operation_incomplete` | `repeat_pending_operation` |
| Definition bytes or effective Linux settings differ | `definition`, fail | `service_definition_changed` | `inspect_native_definition` |
| Registered native identity cannot be verified | `definition`, fail | `service_identity_unverified` | `inspect_native_registration` |
| Missing, nonexecutable, or unsafe executable | `executable`, fail | `service_executable_invalid` | `restore_executable` |
| Invalid runtime configuration | `configuration`, fail | `service_config_invalid` | `fix_configuration` |
| Changed bound paths or newly enabled unbound Docker | `configuration`, fail | `service_binding_changed` | `reinstall_service` |
| Manager unavailable | `manager`, fail | `service_manager_unavailable` | `open_supported_user_session` |
| Native enablement cannot be observed | `manager`, warn | `service_enablement_unknown` | `inspect_native_registration` |
| Native enablement disagrees with acknowledged intent | `manager`, fail | `service_enablement_changed` | `repeat_startup_intent` |
| Lock owner and native PID disagree, or active owner has a different installation observation | `runner`, fail | `service_owner_mismatch` | `inspect_native_registration` |
| Held lifetime lock has missing or terminal observation | `runner`, fail | `service_observation_unavailable` | `inspect_service_logs` |
| Matching active observation is older than 60 seconds | `runner`, fail | `service_observation_stale` | `inspect_service_logs` |
| Matching observation has anomalous clock order | `runner`, fail | `service_clock_anomaly` | `check_system_clock` |
| Latest matching invocation ended in failed phase and no newer owner is ready | `runner`, fail | `service_runner_failed` | `inspect_service_logs` |
| Released lifetime lock retains an active phase without a pending native start | `runner`, fail | `service_interrupted` | `inspect_service_logs` |
| Manager accepted start, no published owner yet, no conflicting record | `runner`, warn | `service_starting` | `check_status_later` |
| Valid observation belongs to an earlier installation | `runner`, warn | `service_historical_observation` | `none` |
| Stopped service with no failed current invocation; missing observation before first start | `runner`, pass | `service_stopped` | `none` |
| Missing database | `state`, warn | `state_not_initialized` | `start_service` |
| Damaged or incompatible database | `state`, fail | Existing stable store code | `inspect_saved_state` |
| No log files, including after a fresh install | `logs`, warn | `service_logs_empty` | `none` |
| Truncated tail, archive holes, or unknown continuity | `logs`, warn | `service_log_gap` | `none` |
| Malformed complete record, duplicate identity, or exceeded budget | `logs`, fail | `service_log_invalid` | `inspect_service_logs` |
| Docker readiness probe fails | `docker`, fail | Existing typed local validator code | `check_local_docker` |
| Validation is disabled | `docker`, skip | `validation_disabled` | `none` |
| A required earlier check failed or installation is absent | Dependent check, skip | `prerequisite_unavailable` | `resolve_prerequisite` |
| Shared probe budget exhausted | Affected check, fail | `service_diagnostic_timeout` | `retry_doctor` |

An unreadable native PID or lock owner is unavailable, not a mismatch: report `runner`, warn, `service_owner_unknown`, action `open_supported_user_session`, unless another independently proven failure applies. Age alone does not fail a terminal stopped observation, since it no longer receives heartbeats. A prior failed invocation replaced by a newer matching ready observation is historical and cannot keep doctor failing. Clock checks apply only to the current installation's record. With no installation, skip profile dependent checks but still inspect retained logs and the fixed native service for conflicts; a registered service without a manifest fails definition identity. No missing installation outcome authorizes repair or file creation.

Docker checks reuse the validator's local runtime, platform, image, and worker compatibility inspection without acquiring its owner or reading credentials. They do not reconcile sessions, enumerate account data, probe WARP entitlement, or imply that pending remote cleanup is complete. Validation disabled means Docker is skipped. A stale effective config is not replaced by the observation policy for this check; configuration failure causes dependent Docker checks to skip.

Doctor always delivers the prepared complete report before returning 0 or 1 for its check outcome. Output errors take precedence over the health result. It is an inspection tool, never a repair command.

### Consistency, privacy, and value sources

There is no transaction across manifest, native manager, runner observation, logs, and SQLite. Each component carries its own observation time. Status rereads manifest identity after component collection. If the installation ID changed, appeared, or disappeared, set all four installation bound components to `availability=unavailable`, `error_code=service_changed`, and `data=null`. This makes `complete=false` under its existing exact rule and avoids combining two installations. Return the report without retrying or choosing one installation silently. No read requires mutable configuration to succeed first.

Stable codes originate in typed configuration, service, application, store, and validator results. Lane phases and policy originate in the actual coordinator. Candidate outcome counts, source failures, and run totals originate in the single status transaction. Log gaps originate in file identity and sequence discontinuities. Doctor outcomes originate in the named bounded read probes. None is inferred from raw vendor messages or a PID alone.

All service data uses owner private files and the same account boundary as installation. Never echo a rejected config value, native command output, SQLite error text, key, or credential. The absence of a database does not authorize creating one during diagnosis. Earlier observation or log evidence cannot override a newer installation identity or a released owner lock.

### Critical scenarios

Compare text and JSON while a writer commits new candidates, while configuration is invalid, before first startup, after uninstall, and during clock regression. Verify all 338 outcomes and queue counts without truncation. Rotate logs with a slow follower, interrupt rotation, fail observation synchronization, and break diagnostic output. Run doctor with Docker absent, stopped, wrong platform, wrong image, and disabled validation, proving zero mutations. These scenarios cover AC-8 through AC-16.

## Rationale

Separate component observations avoid claiming atomic process and database state. A single saved work snapshot keeps its counts coherent. Latest observations and fixed size logs answer the selected operational questions without a new history store or telemetry service.
