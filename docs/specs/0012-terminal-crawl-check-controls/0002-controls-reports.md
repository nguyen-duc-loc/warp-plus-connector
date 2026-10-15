# Controls and saved reports

## Summary

You use the current terminal design to choose work, review its exact scope, and follow progress. You can open a saved run from recent work or by its ID and read every detail in a fixed snapshot. Work completion leaves your current screen in place.

## Screen composition

Use `ui/design.md` and the current screens as the design source. Keep the terminal font and background, cyan focus accent, explicit focus marker, text status labels, fixed header and footer, and one scrolling body. New shared components are a source checklist, a selection choice, a review action row, an active work indicator, a progress summary, a saved run chooser, and a report pager. Reuse existing input, help, scrolling, escaped text, and async job patterns. No assets, animations, downloaded fonts, or new design system are needed.

Overview retains Collection, Recent work, Schedule, and Service. Add `c` for Crawl and `w` for Work. Enter on the focused Recent work section opens a chooser containing the same saved recent runs and an Open run ID action. The chooser can also open when recent runs are absent, so older IDs remain accessible. It does not imply a complete searchable run archive or change the existing ten run overview bound.

Results adds `c` for Check and `w` for Work. Check opens a choice of Focused, Marked, or Matching, initially Focused. Show the respective counts from the current view; disable focused when no row exists and marked or matching when their count is zero. No action implicitly chooses the marked subset merely because marks exist. Opening Check hides a revealed key before any request. Results content, order, evaluation, and marks remain fixed while operations commit new evidence until the user refreshes.

The global indicator shows Idle, the active operation kind and phase, or Outcome available. It appears in Overview, Results, Details, reports, help, and resize notices. `w` opens the current work surface when not in a text field or another modal action. A work completion changes the indicator, not the screen or keyboard focus. Independently append Cleanup unresolved with the originating run ID when known, or Run unknown, whenever the retained `unresolved_cleanup` value is nonnull. A successful later crawl cannot hide this warning. Show the saved report action when the retained run ID is known; acknowledgement alone does not dismiss the warning.

At every size and on every surface, including preparation, help, a quit dialog, and text entry, Ctrl+X requests Cancel work and Ctrl+G requests Cancel preparation. Handle these control combinations before text input and local shortcuts. Show both labeled actions while both owners exist, using a separate wrapped footer line when needed; help explains their separate effects. Disabled actions have no side effect. These shortcuts never wait for a preparation to finish, change focus, close the current form, or implicitly cancel the other owner. They enter the shared scheduler's cancellation priority immediately, after any already entered request completes. Existing `c` shortcuts remain local: Cancel work on Work, Cancel preparation on preparation progress. No printable shortcut is captured from a text field.

### Crawl setup and review

Crawl setup lists sources in configuration order with enabled state and configured page limit. All enabled entries begin selected. Disabled entries remain visible but cannot be selected. Space toggles the focused enabled source; arrows move focus. Provide an optional common page limit field, initially empty. Empty means use configured limits, and a nonempty value accepts only decimal digits denoting 1 through 100. The draft is discarded when this flow is left.

Review and Back are explicit focusable actions. Enter activates only the focused action; typing never starts review or work. A zero enabled selection gives an inline explanation and disables review. Review preparation uses the job contract and may be cancelled independently of other work.

Crawl review shows collection, ordered source names, each effective page limit, maximum page budget, and the fact that no validation will start. The budget is an upper bound, since a source can end earlier. Note collection initialization when state was absent. Explain recovery of prior interrupted records on writer admission. Initial action focus is Back, with a separate Start crawl action. Only that activation sends `work.start`.

### Check setup and review

Review shows collection, Focused/Marked/Matching, the focused candidate ID when applicable, exact candidate count, Results snapshot time and filters, configured image identity, workers, maximum attempts, mutation interval, and candidate timeout. It explains that membership is frozen and identities are checked again before a new batch is saved. A policy details section exposes every `CheckPolicyWire` field with human duration labels, so fixed limits are inspectable without crowding the main review.

Label configured Docker as not yet contacted. Unsupported validation shows setup guidance and has no Start action. No settings are editable here. Review explains that existing writer recovery and validator reconciliation may run on Start. Initial focus is Back; Start check is separate. Starting a large matching selection still requires this explicit review, but no extra arbitrary count threshold is introduced.

Any Back, Escape, selector edit, changed source override, view replacement, filter application, or applicable mark change invalidates the review and its local request generation. It cannot be reused after returning. A late preparation reply cannot display a review for an abandoned flow; release any committed but abandoned handle through the serialized scheduler. Once Start is sent, navigation intent waits for the acceptance reply or session failure; it never cancels by pretending the operation was not accepted. Pending q intent opens the normal quit confirmation. Only an accepted quit cancels the accepted operation immediately after its handle arrives, or cancels the session on channel failure. Ctrl+C still cancels the session immediately.

### Work and outcomes

The Work screen contains a header with kind, acknowledged run ID, elapsed time, and observation age. Without an ID, show Not yet saved only for `run_created=false`, and Save outcome unknown for `unknown`. Its body presents summary counts, current activity, waits, and then recovery or failure guidance. Crawl shows sources completed, failed and skipped, pages requested and completed against the budget, committed candidate and sighting counts, and current source/page. Check shows selected, pending, active, completed, skipped and interrupted items, evidence outcome counts, attempts started/completed, workers, and retry waiting count and earliest wait.

Counts label their unit. Selected items and attempts are different quantities. A saved candidate count is not automatically a count of newly discovered identities. Confirmed, invalid, inconclusive, unresolved, and unstarted retain the existing definitions. Cleanup is an explicit phase, not a frozen percentage at 100. Do not invent a completion percentage or ETA from page budgets or retry ceilings.

While running, `c` on Work or global Ctrl+X requests operation cancellation immediately, `Escape` returns to the previous screen, `w` returns to Work, `?` opens help, and `q` opens quit confirmation. Escape from Work never cancels it. The cancellation action becomes a disabled Cancelling label after the request; repeated presses do not change the deadline. Report details become available once a run ID is acknowledged. Opening them takes a separate fixed report snapshot, with its own timestamp, and does not stop work polling.

Terminal outcomes show execution result, saved run status, counts, whether persistence is uncertain, cleanup status, and guidance. An outcome does not hide committed partial work. Provide View saved report when an acknowledged run ID exists and an Acknowledge action. Acknowledge releases the operation slot only after resources close. It does not clear a recovery block. Start remains unavailable until acknowledgement. If no run ID is known after an uncertain creation, explain that saved work may exist and recommend inspecting history after restart, without presenting a guessed ID.

Quitting while work is active shows Stay and Cancel work and quit, initially Stay. If an export is also preparing, describe that both will be cancelled and that file publication may already have happened. If work completes while this dialog is open, retain the dialog and update the explanation; the quit action still means quit, never another start. Ctrl+C and external signals bypass this modal and preserve existing signal exit codes. In a text field, q is text; leaving it restores the usual quit action.

### Layout, small terminals and accessibility

At 100 columns or wider, align activity and report rows as tables. From 60 through 99 columns use labeled blocks with full IDs reachable by scrolling. Keep action rows outside the body scroll region and reserve enough rows for wrapped shortcuts. At 60 by 16 every primary action remains visible. Do not truncate Start, Back, Cancel, Stay, or Acknowledge into indistinguishable labels.

Below 60 columns or 16 rows, hide setup and report bodies, clear disclosure, and retain the operation indicator, help, cancellation, and quit controls. Block new starts and review activation until usable size returns. Ctrl+X and Ctrl+G keep their independent meanings and labels in the compact notice, including while both owners are active. The small terminal quit confirmation uses a minimal text prompt with Stay as default and Tab/Enter actions; Ctrl+C always remains available. Restore previous focus and scroll when size returns.

Color is decoration only. Escape dynamic text before width calculation, including configured channels, image identity, paths already allowed by export, and saved display fields. Printable characters belong to the active text input; Tab changes field, Escape leaves the form, and Enter activates only an explicit action. No report or work control reveals keys or prints raw errors.

## Saved report snapshots

Recent run selection supplies its existing run ID. Manual entry accepts a canonical positive decimal ID within the Go int64 domain, with explicit Open and Back actions. It does not accept a key, arbitrary SQL, negative number, zero, exponent, whitespace, or a command. Go reads the saved kind by ID; the client never declares the kind to choose a reader.

Add a concrete application report session and store snapshot traversal. Establish one read transaction and collection identity before sampling the UTC observation clock. Stream header, complete sources/pages or selected items/attempts into private indexed report storage while the transaction is held. Finalize counts from the same transaction, then close the live reader before committing the report handle. Current `ShowCheck` calls use separate page transactions and cannot alone establish this snapshot contract; reuse their SQL and mapping behind the new store traversal, without exporting SQL into app or terminal.

Keep one active report snapshot. Opening another or refreshing prepares a replacement and swaps it only through the existing cancellation commit gate. Failed preparation retains the prior report, its timestamp and position with a visible error. Successful replacement restores section and focused source/candidate/attempt identity when present, otherwise clamps to the nearest valid position. Failed cleanup after committed replacement activates the new report and surfaces `ui_snapshot_cleanup_failed`; it blocks further preparation until cleanup succeeds. Report closure waits for entered reads and removes only its owned temporary storage.

A missing run is `state_record_not_found`. Missing state, incompatible schema, unreadable private state, and a replaced collection retain their separate safe codes. Collection replacement discards handles; it does not retain apparently usable old data for a new database. Repeated refresh never recovers unfinished runs or changes schedules. An active saved run can be viewed; its report remains fixed until explicit refresh and does not become a live activity feed.

### Report contents and ordering

Each report has a Summary tab and kind specific detail tabs. Summary shows run ID, kind, origin, saved status, start, finish/recovery times, safe reason, observation time, counts, and known policy/backend metadata. No live service observation proves that a saved run is still executing.

Crawl reports have Sources and Pages. Sources follow saved plan order. Pages belong to the focused source and follow page number. Show effective limit, status, stop reason, request time, parse time, optional boundary IDs, parsed post count, and saved candidate/sighting counts. Keep unknown or legacy coverage explicitly unavailable; older single source coverage uses its saved `CrawlCoverage` rather than an invented modern plan.

Check reports have Items and Attempts. Items follow saved selection ordinal; attempts for the focused item follow attempt number, then attempt ID. Show item disposition, attempts, times, reason, and latest outcome. Attempts show saved outcome, normalized evidence, backend identity, times, classification and retry facts. Legacy checks without a batch show policy and selection as unavailable, with an Attempts tab ordered by attempt ID. Never manufacture selected items or retry metadata for them.

All report sections use fixed ordinal offsets into the temporary snapshot. One page has at most 50 records and 768 KiB of encoded item data, advancing by the returned next offset rather than a guessed page size. Offsets equal to count return an empty page; larger offsets fail. Source and item changes reset their subordinate page position. Header and section counts remain exact decimal strings, and the UI retains only a bounded page plus current focus. No limit on total reachable history is introduced.

### Report methods and closed records

Use protocol 3 and the shared preparation job from the operation child. All methods have the inherited account and session access boundary. Bound handle reads use five second contexts and twelve second watchdogs. Preparation has no total duration limit; cancellation uses the existing five second preparation watchdog and entered resource ownership rules.

| Method | Required params | Result | Main errors |
| --- | --- | --- | --- |
| `report.open` | `run_id: Decimal` | `JobStarted` | `state_record_not_found`, `ui_busy`, state or snapshot code |
| `report.refresh` | `report_id: Decimal`, `focus: ReportFocus` | `JobStarted` | `ui_handle_stale`, `ui_collection_changed`, `ui_busy`, state or snapshot code |
| `report.state` | `report_id: Decimal` | `ReportView` | `ui_handle_stale`, `ui_collection_changed` |
| `report.page` | `report_id`, `section: sources or pages or items or attempts`, `source_id: string?`, `candidate_id: Decimal?`, `offset: Decimal` | `ReportPage` | `ui_handle_stale`, `ui_page_invalid`, `state_invalid_input` |
| `report.close` | `report_id: Decimal` | `{closed: true}` | `ui_handle_stale`, `ui_snapshot_cleanup_failed` |

Sources and items use null source and candidate IDs. Pages requires a source from this report and null candidate. Modern attempts requires a candidate from this report and null source. Legacy attempts uses both null. Wrong section for run kind is invalid input. These methods never reuse a live Results candidate handle as proof of report membership.

After a failed refresh, `report.state` returns the retained header and failure code without another snapshot preparation. State, page, and refresh requests validate the collection's canonical directory and database file identity; missing or changed identity discards the report with `ui_collection_changed`. Ordinary report reads perform no implicit refresh. Report close remains possible after invalidation so owned temporary resources can be released.

Every field below is required; a question mark means explicit null. `RecentRun`, `CrawlCounts`, `ValidationCounts`, `DisplayText`, `Stamp`, and `AttemptRow` reuse their exact safe shapes from specs 0010 and 0011. Reused records carry no key. Enums and codes are restricted to the existing applicable saved domains.

| Record | Exact fields |
| --- | --- |
| `ReportView` | `report_id`, `as_of: Stamp`, `run: RecentRun`, `legacy: boolean`, `source_count`, `page_count`, `item_count: Decimal?`, `attempt_count`, `crawl_counts: CrawlCounts?`, `validation_counts: ValidationCounts?`, `policy: CheckPolicyWire?`, `backend_id: DisplayText?`, `backend_version: DisplayText?`, `legacy_coverage: LegacyCoverage?`, `focus: ReportFocus`, `retained: boolean`, `error_code: Code?` |
| `ReportFocus` | `section: summary or sources or pages or items or attempts`, `source_id: string?`, `candidate_id: Decimal?`, `page_number: Decimal?`, `attempt_id: Decimal?`, `offset: Decimal` |
| `ReportPage` | `report_id`, `section`, `source_id: string?`, `candidate_id: Decimal?`, `offset`, `total`, `items: ReportSource[] or ReportCrawlPage[] or ReportItem[] or ReportAttempt[]`, `next_offset: Decimal?` |
| `ReportSource` | `source_id: string`, `channel: DisplayText`, `position`, `page_limit`, `status: string`, `started_at: Stamp?`, `finished_at: Stamp?`, `recovered_at: Stamp?`, `stop_reason: Code?`, `page_count`, `saved_candidates`, `saved_sightings` |
| `ReportCrawlPage` | `source_id: string`, `page_number`, `requested_url: DisplayText`, `status: string`, `requested_at: Stamp`, `before_id: Decimal?`, `fetched_at: Stamp?`, `finished_at: Stamp?`, `recovered_at: Stamp?`, `parsed_post_count: Decimal?`, `min_message_id: Decimal?`, `max_message_id: Decimal?`, `next_before_id: Decimal?`, `reason_code: Code?`, `saved_candidates`, `saved_sightings` |
| `ReportItem` | `candidate_id`, `ordinal`, `status: string`, `stop_reason: Code?`, `started_at: Stamp?`, `finished_at: Stamp?`, `recovered_at: Stamp?`, `attempt_count`, `latest_outcome: Outcome?` |
| `ReportAttempt` | `candidate_id`, `attempt: AttemptRow` |
| `LegacyCoverage` | `source_id: DisplayText`, `requested_url: DisplayText`, `coverage_mode: DisplayText`, `page_limit`, `fetched_at: Stamp?`, `parsed_post_count: Decimal?`, `min_message_id: Decimal?`, `max_message_id: Decimal?` |

Fields without an explicit type are `Decimal`, except inherited `section`. `legacy` means crawl without modern saved source plan or check without saved batch metadata. `item_count` is null for legacy check selection and zero for crawl; source/page counts are zero for checks. Crawl attempts count is zero. Only the matching count variant is nonnull. Modern checks require policy and backend metadata; legacy missing fields remain null. `legacy_coverage` is nonnull only when an older crawl actually saved it. A nonretained report has null error; a retained report has its failed refresh code and original observation time.

Initial open returns Summary focus with null identities and offset zero. Refresh focus comes from the UI's current row and section: source for Sources, source and page number for Pages, candidate for Items, candidate and attempt ID for modern Attempts, or attempt ID alone for legacy Attempts. Other identities are null. The parent verifies these against the old report and locates them in the new indexed snapshot, returning its new ordinal. If a row is gone, clamp the old offset in that section; if its parent is gone, return the parent selection section at its first row. Empty sections return offset zero and null row identity. Summary always has zero offset and null identities. Failed refresh preserves local position; `report.state` and ordinary page replies never reset locally moved focus.

### Field sourcing and bounded details

Report summary is the existing run plus counts derived in the same report transaction. Source and page rows map directly from `CrawlSource` and `CrawlPageRecord`; per source page count counts its saved page rows. Item rows map from `ValidationItem`; `latest_outcome` is nullable when there is no attempt. Attempt rows reuse the safe normalized detail mapping already used by candidate Details, with the wrapper supplying `Attempt.CandidateID`; the wrapper and nested candidate IDs must match. Policy is the saved batch policy, not current configuration. Legacy coverage maps directly from `CrawlCoverage`.

Limit dynamic nonsecret strings to the existing 4096 UTF-8 byte display bound with a visible shortened flag. Never shorten a structural identity used in a subsequent request; source IDs use their validated saved identity, with the corresponding display field separately bounded. A legacy source without a modern plan is display only. Safe normalized evidence retains its existing typed bounds. Report data cannot introduce raw source message text, key material, or validator output.

## Rationale

The existing screen language keeps controls familiar. Fixed report snapshots provide consistent counts and complete pages even when the active operation commits more work. Separate live progress and saved observations keep execution activity distinct from durable evidence.
