# Public source adapters

The governing contract is [the source crawl child of spec 0003](../../docs/specs/0003-source-crawl-validation/0001-source-crawl.md). [telegram/telegram.go](telegram/telegram.go) implements the fixed `telegram:warpplus` source. Multiple channels and historical coverage belong to later scope work.

## Retrieval

The adapter makes one GET to `https://t.me/s/warpplus` with `wpc/<version>` as its user agent. Its dedicated HTTP client has a 20 second deadline, rejects redirects, and disables connection reuse to prevent automatic request replay. Only status 200 and `text/html` are accepted. Read at most 2 MiB plus one byte after decompression, reject overflow, and close the response body.

Use goquery for scoped page traversal and `x/net/html` for visible text extraction. The caller supplies the clock and may replace the HTTP transport for tests. The adapter returns typed messages and coverage facts, without opening state or assessing entitlement. Raw HTML and upstream error text stay out of persistence and diagnostics.

## Parsing and provenance

Require the expected channel identity and one recognized feed. An empty recognized feed is a valid zero post result; unrecognized content is an error. Source posts are direct message children or messages in direct wrappers. Nested widgets do not supply source provenance. Positive `int64` message IDs must be unique across the whole parsed page; malformed or duplicate IDs reject the page before any message is returned.

Candidate tokens match `[A-Za-z0-9]{8}-[A-Za-z0-9]{8}-[A-Za-z0-9]{8}` with exact case. Adjacent Unicode letters, digits, underscores, and hyphens exclude a match. Hidden content and link attributes contribute no text. Inline elements preserve text continuity; block elements and line breaks add whitespace. Deduplicate exact tokens within each message.

Canonical message IDs omit leading zeros and produce URLs under `https://t.me/warpplus/`. Invalid or absent publication dates remain absent. Capture one UTC timestamp after reading the body, truncate it to microseconds, and reuse it for page and message observations. Return messages in numeric ID order. Parsed coverage includes posts without keys; message bounds never claim complete channel history.

## Verification

You can run `rtk proxy go test ./internal/source/telegram ./internal/app` from the repository root. Synthetic HTTP responses test parsing and request limits; application tests exercise durable intent before fetching and message persistence through the real store. Live DOM compatibility remains covered by [the feature verification record](../../docs/specs/0003-source-crawl-validation/verify.md).

_Drafted by /sync from the introducing change, worth a quick human pass._
