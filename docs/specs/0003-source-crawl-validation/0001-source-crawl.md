# Source crawl contract

## Summary

You collect candidates from one public preview page and retain enough coverage information to explain the result after restart. Parsing is limited to recognized source messages. A page that cannot be recognized fails explicitly instead of appearing to contain no keys.

## Retrieval and extraction

Use `net/http` and goquery. The fixed source ID is `telegram:warpplus`; fetch `https://t.me/s/warpplus` once with method GET. Do not follow pagination, links, media, scripts, or embeds. Send no login, cookie, authorization, or account data. Set the user agent to `wpc/<version>`, using the existing `dev` fallback.

Use normal TLS verification and a client dedicated to this adapter. Set a 20 second request deadline covering DNS, connection, headers, and reading the body. Reject redirects rather than broadening the allowed destination. Accept only status 200 and a `text/html` media type, ignoring its optional charset parameter. Reject other status codes, including 429, without a retry in this slice. No successful outcome can be inferred from a redirect or login page.

Limit the decoded response body to 2 MiB by reading at most that limit plus one byte. This applies after any HTTP content decompression, rather than trusting `Content-Length`. Close the body on every path. Keep HTML in bounded memory only; do not save live bodies or include them in errors. Tests use sanitized synthetic fixtures.

The initial expected grammar is Telegram message wrappers with a `data-post` value of `warpplus/<positive decimal ID>`, scoped visible text, and optional publication metadata. goquery selectors such as `.tgme_widget_message[data-post]` and `.tgme_widget_message_text` are implementation starting points, not a claim that this design verified live DOM compatibility. During implementation, inspect the real response structure without exposing keys, pin a sanitized fixture, and prove the grammar before reporting a successful live adapter. Selector adjustments that preserve this contract are implementation details.

Require a recognizable channel page and message feed, including channel identity matching `warpplus`. A successful zero post parse requires positive evidence of that channel and an empty recognized feed. A shell alone, a nonempty feed of unrecognized elements, an error page, or a challenge page is a parse error. If a reliable empty feed cannot be recognized, report `source_parse_error`; do not guess zero. A recognized message with no text, such as a media only post, counts as one parsed post and contributes no candidates.

Within recognized message text, ignore script, style, hidden content, and link destination attributes. Decode HTML entities. Treat block boundaries and line breaks as whitespace while preserving ordinary inline text, so a token cannot be synthesized across separate blocks. The recognized token is three groups of eight ASCII letters or digits separated by literal hyphens: `[A-Za-z0-9]{8}-[A-Za-z0-9]{8}-[A-Za-z0-9]{8}`. Both adjacent characters, when present, must not be Unicode letters or digits, an underscore, or a hyphen. Implement boundary checks explicitly rather than relying on regex word boundaries. Preserve exact case. A candidate match is not entitlement evidence.

Each top level source post contributes once. Do not treat embedded or quoted widgets, forwarded attribution links, navigation text, quotas, or channel descriptions as separate source messages. Validate IDs as positive `int64` values. Require unique message IDs in the parsed page; duplicate or malformed identity fails the page rather than silently combining ambiguous provenance. Fully parse and validate the page before saving any candidates.

Canonicalize the stored message ID to decimal without leading zeros and construct its URL as `https://t.me/warpplus/<id>`. Parse publication time from that post's timestamp when valid; otherwise leave it absent. Never substitute fetch time for publication time. Capture `fetched_at` from the application UTC clock after the bounded body has been read successfully. The page's observation time equals that timestamp and remains fixed across all writes and retries. Times use the store's UTC microsecond normalization.

After parsing, save its coverage facts, then process messages in numeric ID order. Deduplicate exact candidate matches within each message before `RecordMessage`; that method remains the authority for normalized key identity and atomic provenance. Posts without candidates are counted in parsed coverage but do not create message bodies or empty provenance rows. All distinct matches in this one page are retained; there is no key count cap.

## Coverage storage

Add one versioned migration after the existing schema. The new table is `crawl_coverage`. Existing records are unchanged and legacy runs receive no fabricated rows. All existing store permission, migration, locking, recovery, and cancellation behavior remains in force.

| Field | Storage and nullability | Meaning |
| --- | --- | --- |
| `run_id` | Required `INTEGER`, primary key and foreign key to `runs.id` | One coverage record for a crawl run |
| `source_id` | Required `TEXT` | `telegram:warpplus` |
| `requested_url` | Required `TEXT` | Canonical fixed preview URL, without credentials, query, or fragment |
| `coverage_mode` | Required `TEXT` | `latest_preview` |
| `page_limit` | Required `INTEGER` | `1` |
| `fetched_at` | Nullable UTC microsecond `INTEGER` | Complete bounded response receipt, stored only with successful parsed facts |
| `parsed_post_count` | Nullable nonnegative `INTEGER` | Distinct recognized source posts, including posts without candidates |
| `min_message_id` | Nullable positive `INTEGER` | Lowest recognized message ID |
| `max_message_id` | Nullable positive `INTEGER` | Highest recognized message ID |

All parsed fields begin absent. A successful empty parse fills `fetched_at` and a zero count and leaves both bounds absent. A positive count requires both bounds with minimum no greater than maximum. Request failure or unrecognized HTML leaves all parsed facts absent. Successful parsing followed by a persistence failure retains parsed facts and only the candidate writes that actually committed. A parsed count therefore never means every candidate was saved; run status and saved counts make that distinction.

Enforce foreign keys and field constraints in SQL where practical, and enforce crawl kind and running status in the same transaction as a mutation. Do not index fields beyond the primary key without a query need. Request metadata is immutable. Parsed facts fill once; identical normalized replay succeeds, conflicting replay returns `ErrConflict`. A terminal run accepts only an already committed identical replay, without changing any facts.

Extend domain records with typed coverage inputs and results. SQL and driver types remain inside the store. The operation names below define the proposed public Go contract, using ordinary structs for their fields.

| Operation | Inputs | Result | Important errors |
| --- | --- | --- | --- |
| `StartCrawl` | Context, existing run creation fields, source ID, requested URL, coverage mode, page limit | Crawl run with its requested coverage, created atomically | Invalid input, conflicting request token, state failure |
| `RecordCrawlPage` | Context, run ID, fetched time, parsed post count, optional ID bounds | Persisted coverage | Wrong kind or missing run, closed run, conflicting facts |
| `GetCrawlReport` | Context, run ID | Run, optional coverage, saved distinct candidate count, saved sighting count | Missing run, wrong kind, state failure |

`StartCrawl` forces kind `crawl`. Replay with a matching request token requires all normalized creation and coverage inputs to match and returns the existing record. Reusing a token from legacy `StartRun` without coverage conflicts; it never retrofits a terminal run. Existing `StartRun` remains available for old callers and validation runs.

`GetCrawlReport` is available on reader and writer handles. Read the run, coverage, and counts inside one short read transaction. Count observation links for sightings and distinct linked candidate IDs for candidates. The report can inspect an active run without using `ListObservations`, whose terminal run restriction remains unchanged. Current counts are explicitly provisional until the run finishes. Missing coverage on a legacy run is a successful report with absent coverage, not corruption.

The source adapter accepts a context and returns a bounded parsed page with message inputs and coverage observations, or a classified error. It does not open state, classify entitlement, or return raw upstream bodies in an error. The application supplies the clock and run identity. Its HTTP client is replaceable at the external boundary for controlled fixtures.

## Verification

Use sanitized fixtures for multiple keys, exact duplicates, case variants, Unicode and ASCII token boundaries, split blocks, inline elements, HTML entities, media only posts, invalid optional dates, embedded posts, malformed IDs, duplicate IDs, and wrong channel identity. Test a recognized feed without matching tokens separately from a structural error. Verify the body limit on decoded compressed data and that redirects cause no second request.

Store checks cover atomic run and coverage creation, replay after lost acknowledgement, conflicting page facts, legacy reports, migration rollback, active report consistency, and a later message write failure. A live check demonstrates the current source adapter, while controlled fixtures provide stable assertions. These scenarios cover parent AC-1, AC-2, AC-6, and AC-9.

## Rationale

One preview page provides a finite first crawl while preserving honest coverage. goquery makes scoped message traversal readable; direct `x/net/html` traversal is the viable alternative. Persisted source facts explain empty and partial results, while derived saved counts avoid counters that can disagree with committed observations. (basis: your confirmed model, spec 0002, [goquery project](https://github.com/PuerkitoBio/goquery), [HTML parser documentation](https://pkg.go.dev/golang.org/x/net/html))
