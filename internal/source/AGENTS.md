# Public source adapters

The governing contracts are [the extraction rules in spec 0003](../../docs/specs/0003-source-crawl-validation/0001-source-crawl.md) and [configurable coverage in spec 0004](../../docs/specs/0004-configurable-source-coverage/index.md). [telegram/telegram.go](telegram/telegram.go) implements bounded public preview pages for normalized Telegram channels.

## Retrieval

`FetchPage` makes one GET to `https://t.me/s/<channel>`, optionally with the sole query `before=<positive-id>`, and uses `wpc/<version>` as its user agent. Its dedicated HTTP client has a 20 second deadline, rejects redirects, and disables connection reuse to prevent automatic request replay. Only status 200 and `text/html` are accepted. Read at most 2 MiB plus one byte after decompression, reject overflow, and close the response body.

The adapter never follows pagination or retries by itself. `RequestStartedAt` exposes the last monotonic HTTP start for sequential application pacing. HTTP 429 returns `source_rate_limited`; the application stops later requests.

Use goquery for scoped page traversal and `x/net/html` for visible text extraction. The caller supplies the clock and may replace the HTTP transport for tests. The adapter returns typed messages and coverage facts, without opening state or assessing entitlement. Raw HTML and upstream error text stay out of persistence and diagnostics.

## Parsing and provenance

Require the expected channel identity and one recognized feed. An empty recognized feed is a valid zero post result; unrecognized content is an error. Source posts are direct message children or messages in direct wrappers. Nested widgets do not supply source provenance. Positive `int64` message IDs must be unique across the whole parsed page; malformed or duplicate IDs reject the page before any message is returned.

Candidate tokens match `[A-Za-z0-9]{8}-[A-Za-z0-9]{8}-[A-Za-z0-9]{8}` with exact case. Adjacent Unicode letters, digits, underscores, and hyphens exclude a match. Hidden content and link attributes contribute no text. Inline elements preserve text continuity; block elements and line breaks add whitespace. Deduplicate exact tokens within each message.

Channel identity comparisons ignore ASCII case and saved names use lowercase. Canonical message IDs omit leading zeros and produce URLs under `https://t.me/<channel>/`. Invalid or absent publication dates remain absent. Capture one UTC timestamp after reading the body, truncate it to microseconds, and reuse it for page and message observations. Return messages in numeric ID order. Parsed coverage includes posts without keys; message bounds never claim complete channel history.

Only direct `.js-messages_more_wrap` children of the recognized feed and their dedicated `a.tme_messages_more.js-messages_more` controls supply pagination. Newer controls with only `data-after` are ignored. Older links must resolve to HTTPS `t.me`, the selected channel path, and exactly one positive `before` ID, with no credentials, explicit port, or fragment. Matching controls and `data-before` attributes must agree. The next cursor cannot exceed the minimum parsed ID and must decrease from a requested cursor. Empty feeds with older controls, conflicting controls, and malformed navigation fail the entire page before any facts or messages return.

## Verification

You can run `rtk proxy go test ./internal/source/telegram ./internal/app` from the repository root. Synthetic HTTP responses and sanitized latest and older page fixtures test parsing, navigation, and request limits; application tests exercise durable intent and message persistence through the real store. Live DOM compatibility remains covered by [the coverage verification record](../../docs/specs/0004-configurable-source-coverage/verify.md).

_Drafted by /sync from the introducing change, worth a quick human pass._
