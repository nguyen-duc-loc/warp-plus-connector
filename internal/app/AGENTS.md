# Application coordination

The contracts are [spec 0003](../../docs/specs/0003-source-crawl-validation/index.md) for selected checks and saved reads, and [spec 0004](../../docs/specs/0004-configurable-source-coverage/index.md) for configured crawls. [app.go](app.go) and [crawl.go](crawl.go) coordinate concrete store operations with small source and validator interfaces. Cobra parsing and output rendering stay in `internal/cli`.

## Crawl ordering

`Crawl` resolves source selection before opening the writer and saves the complete source plan before fetching. For each page, save request intent, fetch and validate the whole page, save parsed facts, save each message with candidates, then finish the page. A failed message write prevents page completion and later requests. No database transaction spans HTTP or pacing waits.

Requests run sequentially with at least one second between actual monotonic starts across all channels. The Telegram adapter supplies `RequestStartedAt`; fixture sources may use the call start. Pacing responds to cancellation. The UTC application clock supplies durable timestamps, not pacing intervals.

No older cursor means `preview_end`, even at the page budget. Otherwise reaching the limit means `page_limit`, without an extra probe. Ordinary source failures preserve commits and allow later channels, returning `source_failed` for the invocation. HTTP 429 aborts later work with `source_rate_limited` and skips pending sources with `rate_limit_abort`. Crawling never calls the validator.

## Failure and recovery

Any crawl mutation error may represent a committed write with a lost acknowledgement. Stop all further mutations, including cleanup, and leave unfinished records for writer recovery. A bounded final report read may show committed progress but does not authorize another write.

Cancellation between known commits can abort with one separate context limited to five seconds. If abort persistence fails, stop writing. A new manual crawl creates a fresh request token and starts from current preview pages; it never retries or resumes the previous plan.

`Check` resolves the saved candidate before creating a validation run and attempt. The validator owns remote registration recovery; the store owns local run and attempt recovery. Before completion begins, cleanup may retain an inconclusive attempt. Once completion starts, leave an uncertain commit for recovery rather than replacing its payload. An inconclusive check remains a command error even when its run finished normally.

## Reads and diagnostics

`ShowCrawl` and `List` open readers only. Listing fixes one freshness evaluation for every page and has no total result cap. An output callback failure stops traversal and returns `output_error`. Application errors and logs use stable codes, record IDs, and source identities without raw upstream content or keys.

You can run `rtk proxy go test -race ./internal/app` for focused checks. Tests use synthetic source or validator responses with the real store. Live behavior remains recorded in the feature verification files.

_Drafted by /sync from the introducing change, worth a quick human pass._
