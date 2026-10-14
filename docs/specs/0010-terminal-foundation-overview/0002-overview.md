# Overview and saved reads

## Summary

Your home screen starts with fresh confirmed candidates and shows the rest of the collection honestly. Recent work, saved schedules, and service observations explain what happened and what may happen next. Every section can identify unavailable or stale information without losing the useful data beside it.

## Screen and interaction

The design source is your request to suggest a direction, followed by your selection of compact utility. Use terminal monospace text, aligned values, restrained separators, and one cyan accent for the active section. Use explicit words for status; do not depend on red or green, emoji, icons, or custom fonts. Use terminal default foreground and background to support light and dark themes.

Composition is fixed:

1. Header: `WPC`, `Overview`, selected state directory, local date and UTC offset, and refresh state.
2. Collection: prominent fresh confirmed and total counts; stale confirmations and unknown freshness; then latest completed outcomes labeled unchecked, confirmed, invalid, and inconclusive. Explain that fresh evidence and latest outcomes answer different questions and are not additive groups.
3. Recent work: up to ten rows with run ID, kind, origin, saved status, local start time, finish or recovery time when known, and concise counts. A status saved as running is labeled `Saved as running`; it is not a live ownership assertion.
4. Schedule: saved crawl eligibility, candidate active, due and waiting counts, next due time, saved validation wait or recovery obligation, and clock pause when present. Label eligibility as saved facts, not a promise that execution will start then.
5. Service: installation, manager, and runner observations with separate status words and observation ages; selected collection match or difference. No single green health badge substitutes for these facts.
6. Footer: `Tab sections`, `↑↓ scroll`, `r refresh`, `? help`, `q quit`.

At 100 columns or more, collection values can share two aligned groups and recent work uses a table. At 60 through 99 columns, stack collection groups and wrap each run into a compact labeled block. Keep full IDs reachable on a wrapped line. At less than 60 columns or 16 rows, replace the body with a resize notice and the minimal shortcut footer. Refresh and quit still work; `?` displays compact help within available space.

The header and footer stay fixed while the body scrolls. `Tab` and `Shift+Tab` cycle collection, recent work, schedule, and service focus, scrolling the heading into view. Up and down move one body row; Page Up and Page Down move one viewport; Home and End move to the body limits. Focus uses a visible `>` marker plus the optional accent. There is no row selection, drill down action, or disabled navigation for future features.

`?` opens keyboard help, with `Esc` or `?` returning to the previous focus and scroll position. `q` and Ctrl+C apply from every view. Refresh never moves focus or jumps to a newly inserted run. A changed viewport or shorter content clamps scroll position to a valid row.

Use alternate screen entry and exit only through the terminal lifecycle owner. A nonempty `NO_COLOR` or `--color never` removes color and decorative styling. Screen movement escape sequences remain necessary for interaction; no color does not mean plain redirected output. There are no UI assets to fetch.

## Read model and interface

Add `App.ReadOverview(ctx)` as a concrete application read. It returns collection and service components, with no terminal or Cobra types. Add `Reader.GetOverview(ctx, clock, maxAge)` to obtain the collection component in one read transaction. Reuse the existing internal SQL helpers where they fit; do not assemble a collection from separate public reader calls that establish different snapshots.

Add a concrete `service.OverviewStatus(ctx, account)` that reuses the existing installation, native manager, runner, and final installation identity checks but does not read the installed service's candidate store. Refactor shared internal helpers as needed without changing public `service status` output. Account resolution failures are service component failures, independent of the selected collection.

| Record | Identity and relationship | Required fields | Nullable or optional facts |
| --- | --- | --- | --- |
| Collection snapshot | One selected state directory and one evaluation | `as_of`, `max_age`, `total`, latest outcome counts, `fresh_confirmed`, `stale_confirmed`, `unknown_freshness`, recent runs, `older_runs_available`, schedule summary | No partial collection payload on read failure |
| Recent run | Existing `runs.id`, at most ten per snapshot | ID, kind, origin, saved status, start time, clock anomaly flag | Finish time, recovery time, reason code, kind specific counts |
| Crawl counts | One crawl run | Distinct saved candidates and saved sightings from that run | No invented count of newly inserted candidates |
| Validation counts | One validation run with batch policy | Selected, pending, active, completed, skipped, interrupted, confirmed, invalid, inconclusive, unresolved, unstarted | Entire count object null for legacy runs without saved batch policy |
| Schedule summary | Existing selected collection schedule | Snapshot time, checkpoint, clock regression, recovery pending, candidate counts | Crawl plan, candidate next due time, validation wait |
| Service observation | Effective account and observed installation ID | Installation, manager, runner components; comparison `match`, `different`, or `unknown` | Installation ID, bound state directory, individual observed facts when absent or unavailable |
| UI memory | Current process only | Focus section, body scroll row, refresh state | Last successful component, failure code, last successful receive time |

All relationships reuse existing IDs. There are no new primary keys, foreign keys, indexes, retention rules, or persisted preferences. Existing run and candidate history retention remains unchanged. Wire values follow the runtime child spec's exact integer and timestamp encoding.

Each component envelope follows the exact wire records in [the runtime contract](0001-runtime-bridge.md#complete-wire-records), including observation age, clock anomaly, and cache policy. Available data has no error code and uses `cache_policy=replace`. Missing data has a safe code, null data, and `discard`. Unavailable data has a safe code and null data; it uses `retain` only where the identity rules below permit it. A refresh reply itself may succeed while one component is unavailable.

## Collection evaluation

Establish the SQLite snapshot before sampling the application UTC clock. Normalize that time to microsecond precision. Supply the configured `RecheckInterval` as `max_age`, retaining existing validation. Do not substitute a saved scheduling interval for evidence freshness.

For each candidate, find the latest completed attempt and the last definitive attempt using the existing attempt ID order. Latest outcome partitions every candidate into unchecked, confirmed, invalid, or inconclusive. Unresolved attempts do not count as completed outcomes. Their existence remains reflected in saved activity and schedule facts.

Fresh confirmation requires the last definitive attempt to be confirmed, its evidence time not to be in the future, and its age to be strictly less than `max_age`. Age equal to `max_age` is stale. Future confirmation time is unknown freshness and a clock anomaly. A later inconclusive attempt does not replace that definitive evidence. A definitive invalid attempt has no applicable confirmation freshness. Missing required confirmation evidence is a state read failure, not an unknown or zero count.

Aggregate these values within SQL or existing store helpers across the full collection. Do not serialize candidates or keys to JavaScript to count them. The sum of latest outcome partitions equals total. The three confirmation freshness counts partition candidates whose last definitive outcome is confirmed; they are a separate dimension from latest outcomes.

Recent run selection reads the eleven highest run IDs across both kinds in the same snapshot. Return the first ten and set `older_runs_available` when the eleventh exists. Show `Showing latest 10 runs; more saved history exists` when true. This is a deliberately bounded overview, not a pageable history browser. Origin comes only from saved `Run.Origin`; absent legacy origin displays `Unknown`. Times and aggregate counts retain the existing report semantics. A recovered run shows recovery time separately from finish time.

Schedule data reuses `ScheduleSummary` semantics: `CheckpointAt`, `ClockRegressed`, `ValidationRecoveryPending`, nullable `Crawl`, `Candidates`, and nullable `ValidationWait`. For crawl, show saved coverage channel names and page limits, `DueAt`, `Reason`, `BasisAt`, `Interval`, and optional basis or active run references when expanded by wrapping. For validation, show total, active, due, waiting, next due, and the saved wait reason and retry time when present. Do not claim a routine interval that the summary does not supply. No saved crawl plan means `No saved crawl schedule`, not crawling disabled. A saved due time does not prove a runner or Docker is available.

## Service identity and evidence

Reuse spec 0008 account and ownership validation. Never call doctor as a substitute for status because doctor can probe Docker. The overview may inspect the local native manager but never starts, stops, enables, disables, or installs a service.

Retain installation phase and autostart fact, native manager state, and runner ownership and observation facts separately. Reuse the service package's existing definition of observation staleness (older than 60 seconds), identity matching, clock anomaly, and unknown ownership. A stale saved observation is not automatically a failed service. Do not infer current process ownership from a saved running run or a PID alone.

Compare canonical selected state identity with the installation's canonical bound state identity. Resolve existing ancestor aliases when the selected directory is absent, without creating it. A failed canonicalization produces `unknown`, never `different` or `match`. When different, display `Service uses a different collection` and both escaped paths in the service section. Collection totals and schedules continue to describe the selected configuration. Do not read the other collection just to populate this screen.

The service group always exists as metadata plus three separate component envelopes. It has no outer availability or nullable data field. Installation, manager, and runner success are not reduced to one health result. Derive the UI's degraded refresh marker from any unavailable component; missing state is an informative state, not a failed read.

The parent supplies `ServiceIdentity` outside component data from the existing initial and final installation manifest checks:

| Identity state | Source | Cache and display rule |
| --- | --- | --- |
| `verified` | Both valid manifests have the same installation ID | Supply that ID. Use `retain` only if it also equals the last verified ID in this parent session; otherwise discard all previous service values before applying this reply. |
| `missing` | Both reads prove no installation exists | Null ID and `discard`. Show missing service components, and clear previous installation, manager, and runner observations. |
| `changed` | Valid identity or existence differs between the two reads | Null ID and `discard`; all service components are unavailable with `service_changed`. Do not attach partial observations to the new identity. |
| `unknown` | Account resolution or either manifest read cannot establish a valid identity, including corruption, access failure, or timeout | Null ID and `discard`; service data is unavailable with the corresponding existing safe code. Previously cached service observations are cleared. |

Within a verified identity, replace each available installation, manager, or runner component independently. An unavailable nested component can retain only its own previous successful value from that same installation ID, visibly stale. A missing nested component clears its previous value. Thus manager failure does not suppress fresh installation or runner observations. An identity discard is processed first, before any nested replacement or retention. The UI independently checks the ID against its cached ID and discards on mismatch even if a malformed server policy says retain.

Group metadata follows the same identity rule. Any identity other than `verified` forces `collection_match=unknown` and `bound_state_dir=null`, clearing previous comparison and path text along with cached components. Under a verified identity, the bound path comes from that validated installation, and canonical comparison supplies match, different, or unknown. A comparison failure alone does not invalidate an otherwise verified installation.

Keep the last verified service identity only in parent session memory. Do not persist cache generations or identifiers. Corrupt or unreadable identity is deliberately conservative: losing the stale view is preferable to attributing evidence to an installation whose identity is unproven.

## Refresh and failure state

The view lifecycle is `starting`, `loading`, `ready` or `degraded`, then `closing`. A successful refresh can move degraded back to ready. Each component independently tracks `loading`, `current`, `stale`, `missing`, or `unavailable`. These are display states, not changes to saved operational records.

Start one refresh immediately. After it completes, schedule the next for five seconds later using a monotonic timer. `r` when idle starts immediately. While busy, any number of refresh presses coalesce into one pending request, started after the current read closes. Reset the five second timer after that request completes. Resize and keyboard actions remain responsive while reads run.

While refreshing, retain existing data with a visible `Refreshing` marker. After a component failure with `cache_policy=retain`, mark its previous successful data `Stale`, display its original observation time, original age plus elapsed time since successful receipt, and the current safe failure explanation. Never relabel old data with the failed refresh time. Retain at most one previous successful collection, installation, manager, and runner value for this session; do not merge new counts into an old collection snapshot. A `discard` reply removes the corresponding prior value.

If no prior successful value exists, show the component error or missing state without numbers. Successful missing data clears old data rather than leaving it apparently current. A known identity change clears old service data. Collection disappearance clears its previous snapshot and displays absent state. Losing access to an existing collection may retain the already visible snapshot as stale, without performing additional reads outside the established access rules.

Absent collection state displays `No saved collection yet`, explains that the overview does not create it, and suggests the existing `wpc sources` and `wpc crawl` commands with the selected config and state flags where needed. Render command arguments with safe shell quoting as text, never execute them. An initialized collection with no candidates displays total zero and `No candidates saved`; any saved runs and schedules remain visible.

Old or newer incompatible schema displays its stable state error with guidance to use the matching CLI and the existing writer migration path explicitly. Do not trigger migration by opening the screen. Private permission and ownership failures retain the existing safe categories without suggesting an automatic repair. Uninstalled service is `Not installed`; missing manager access or uncertain runner ownership is `Unavailable` or `Unknown`, not stopped.

All dynamic display strings escape ESC, newlines, carriage returns, tabs, and other control characters before layout. Only the renderer introduces terminal controls. Local time labels use the parent supplied `Stamp` fields, so Node and Go timezone databases cannot disagree. UTC timestamps remain the authoritative wire facts. Header time metadata remains available in `OverviewResult.header_at` even when every component fails. Relative observation age uses elapsed monotonic time since receipt plus the original `age_ns`; a null age or clock anomaly displays an explicit warning instead of a negative age.

## Value sourcing

| Display or action | Value | Source |
| --- | --- | --- |
| Header | Selected state path and config flags | Go configuration resolution once at startup |
| Header | Version, local date, UTC offset | Go build metadata and parent local clock; refreshed time metadata in every overview reply |
| Collection | Total and latest outcome counts | Existing candidate and completed attempt rows in the collection snapshot |
| Collection | Fresh, stale, unknown confirmations | Last definitive attempt evidence time, snapshot `as_of`, and effective `RecheckInterval` |
| Recent work | IDs, kind, origin, status, times, reason | Saved `runs` rows, descending ID selection |
| Recent work | Crawl candidate and sighting counts | Existing crawl observation and sighting relationships, distinct candidate IDs |
| Recent work | Validation totals | Existing validation batch and item aggregate helpers; null for legacy policy absence |
| Recent work | Limited history notice | Eleventh matching run in the same snapshot |
| Schedule | Eligibility, due counts, pauses, retry time | Existing saved schedule summary evaluated in the same snapshot |
| Service | Native, installation, runner facts | Existing service read helpers and effective account, with final installation identity recheck |
| Service | Collection match | Canonical selected and installation bound state identities, or unknown on resolution failure |
| Service | Cache invalidation and installation ID | Initial and final validated installation reads plus the parent's last verified ID, held only in session memory |
| Refresh | Current, stale, or unavailable | Component availability, original observation time, successful receive time, and safe current failure |
| Presentation | Focus and body position | Session memory and current terminal dimensions |
| Presentation | No color and local times | Existing color policy, nonempty `NO_COLOR`, and parent supplied local time labels |
| Empty state | Suggested command text | `HelloResult.profile.suggestions`, produced from the existing CLI surface and parent resolved explicit profile arguments, safely quoted |

## Rationale

A single collection snapshot makes totals, recent work, and eligibility internally consistent even while a writer commits. Separate service observations avoid pretending that SQLite and the operating system were read atomically. Compact text and one scroll surface keep the first UI usable without building navigation for later features. (basis: your confirmed read model and visual choices, `GetOperationalWork`, `ScheduleSummary`, and spec 0008 component semantics)
