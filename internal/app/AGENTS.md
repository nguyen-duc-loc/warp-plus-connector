# Application coordination

The contracts are [spec 0003](../../docs/specs/0003-source-crawl-validation/index.md) for selected checks and saved reads, and [spec 0004](../../docs/specs/0004-configurable-source-coverage/index.md) for configured crawls. [app.go](app.go) and [crawl.go](crawl.go) coordinate concrete store operations with small source and validator interfaces. Cobra parsing and output rendering stay in `internal/cli`.

[Spec 0005](../../docs/specs/0005-concurrent-checks-bounded-retries/index.md) extends checks through [batch.go](batch.go) and saved reports through [batch_read.go](batch_read.go). `Check` delegates to `CheckBatch` and returns the final saved attempt.

[run.go](run.go) implements [spec 0007](../../docs/specs/0007-scheduled-crawl-revalidation/index.md) with one lifetime writer, at most one crawl and one validation batch, and shared operation cores for manual commands. [mutations.go](mutations.go) serializes SQLite and journal writes across both lanes. Any uncertain SQL mutation stops later SQL in both lanes while remote cleanup remains possible.

Scheduled selection orders due candidates by due time and ID, taking at most twice the worker count per batch. A provider probe uses one candidate, one worker, and one attempt. Mutation pacing persists across batches. Validator ownership is released while idle; saved recovery obligations can require inspection even without candidates. Backward UTC pauses admission, and overdue work is coalesced after restart or clock repair.

## Crawl ordering

`Crawl` resolves source selection before opening the writer and saves the complete source plan before fetching. For each page, save request intent, fetch and validate the whole page, save parsed facts, save each message with candidates, then finish the page. A failed message write prevents page completion and later requests. No database transaction spans HTTP or pacing waits.

Requests run sequentially with at least one second between actual monotonic starts across all channels. The Telegram adapter supplies `RequestStartedAt`; fixture sources may use the call start. Pacing responds to cancellation. The UTC application clock supplies durable timestamps, not pacing intervals.

No older cursor means `preview_end`, even at the page budget. Otherwise reaching the limit means `page_limit`, without an extra probe. Ordinary source failures preserve commits and allow later channels, returning `source_failed` for the invocation. HTTP 429 aborts later work with `source_rate_limited` and skips pending sources with `rate_limit_abort`. Crawling never calls the validator.

## Failure and recovery

Any crawl mutation error may represent a committed write with a lost acknowledgement. Stop all further mutations, including cleanup, and leave unfinished records for writer recovery. A bounded final report read may show committed progress but does not authorize another write.

Cancellation between known commits can abort with one separate context limited to five seconds. If abort persistence fails, stop writing. A new manual crawl creates a fresh request token and starts from current preview pages; it never retries or resumes the previous plan.

`CheckBatch` resolves every distinct candidate in first supplied order, acquires shared validator ownership, and saves the full selection and policy before dispatch. The coordinator alone writes SQLite, commits registry updates, and emits saved terminal items in completion order. Any uncertain store mutation stops all further store mutations in that invocation; remote cleanup remains independent. An inconclusive batch remains a command error even when its run completed.

Worker capacity defaults to two, accepts one through four, and is released during retry waits. Registration and attachment share a gate that waits after the previous command finishes. Retry scheduling uses explicit failure classes and the monotonic clock, with saved jitter, a candidate budget, proven cleanup, and container removal. Wall clock timestamps record history. Exhausted retries preserve earlier definitive evidence and its freshness time.

Cancellation, output errors, unsafe cleanup, and throttling without a trusted delay stop new dispatch and mutation admission. Shutdown shares a 60 second remote cleanup allowance, then a five second local finalization context; entered store work retains ownership until it finishes. Eligible evidence observed before a stop can survive proven cleanup, while late evidence cannot become definitive. Restart interrupts local work and never resumes unused attempts.

[shutdown.go](shutdown.go) shares one 60 second remote cleanup allowance across the runner's lanes. The five second local finalization window starts when remote work finishes, capped at 65 seconds from stop. Entered store work retains ownership until it returns. Emit only acknowledged terminal run summaries; output failure cancels both lanes and prevents further writes to that stream.

`App.Observe` publishes typed runner and lane phases after schedule preparation; `App.Finalize` receives the shared local finalization context. The managed service supplies these hooks while reusing `App.Run`; observation or output failure cancels both lanes. Keep service file formats and native manager calls in `internal/service`.

## Reads and diagnostics

`App.Logger` is an optional `*zerolog.Logger`; nil disables activity output. Use `diagnostic.Emit` with the scalar fields and event catalogue in [internal/diagnostic](../diagnostic/AGENTS.md), following [spec 0009](../../docs/specs/0009-readable-development-logs/index.md). Emit run and attempt identities from acknowledged records, retry delays from committed retry decisions, and elapsed durations from the coordinator clock. Keys, raw errors, vendor content, and validator session identifiers never enter events.

Major work transitions appear at info, successful page and attempt details at debug, and retries or unresolved work at warn. Saved reads remain quiet. Foreground diagnostic failure cancels work through the invocation sink while preserving the existing cleanup and finalization windows.

`ShowCrawl`, `ShowCheck`, `List`, and `QueryCandidates` open readers only. `ShowCheck` streams the saved policy, every selected item, and each attempt independently of current settings or Docker; legacy reports have no invented batch metadata. `List` delegates to the snapshot query without a total cap and maps callback failures to `output_error`. Application errors and logs use stable codes, record IDs, and source identities without raw upstream content or keys.

[query.go](query.go) implements [spec 0006](../../docs/specs/0006-complete-results-export/index.md): normalize and own filters before state access, combine alternatives within each filter and intersect filters, and capture one UTC evaluation at microsecond precision. `QueryCandidates` counts successful callbacks and probes one extra match to determine truncation. Callbacks prepare output synchronously and retain their error identity. The snapshot and reader close before return, including on cancellation; CLI delivery starts only after successful preparation and reader closure.

`ShowSchedule` reads persisted policy and eligibility through `GetSchedule`, without applying current intervals or acquiring validator ownership. The reader closes before CLI output preparation and delivery.

[overview.go](overview.go) implements `ReadOverview` under [spec 0010](../../docs/specs/0010-terminal-foundation-overview/index.md). It reads the selected collection and the injected `OverviewService` boundary independently, with five seconds per section inside a ten second request context. It waits for entered work and reader closure before preparing the header time; the terminal supervisor owns the independent watchdog for reads that resist cancellation. Missing collection state discards cached data, other failures permit clearly stale retention, and service failure never hides available collection data. This path opens no writer and invokes no source or validator.

You can run `rtk proxy go test -race ./internal/app` for focused checks. Tests use synthetic source or validator responses with the real store. Live behavior remains recorded in the feature verification files.

_Drafted by /sync from the introducing change, worth a quick human pass._
