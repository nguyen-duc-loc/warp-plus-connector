# Results and details

## Summary

You browse a fixed collection with exact counts and mark candidates across pages. Opening a candidate gives you a separate fixed view of its sources and attempts. Background changes appear only when you choose to refresh the relevant view.

## Snapshot preparation and storage

`internal/app` adds a concrete Results session backed by a temporary store owned by `internal/store`. `results.open` uses the existing complete `QueryCandidates` traversal with unrestricted filters and no limit. Capture its fixed evaluation, every summary, and each exact key into private temporary records. Keys remain in the parent. Reuse the existing evidence derivation; do not calculate freshness in TypeScript or introduce a competing SQL formula.

The first results evaluation follows the existing query clock contract, captured immediately before the live snapshot read. It is an evidence evaluation time, not a database commit timestamp. Save the effective `RecheckInterval` as `max_age`. Finish the entire traversal and successfully close the live reader before marking the snapshot ready. A failed scan exposes none of the partial replacement. Store callbacks write only to the private snapshot, never to the UI or destination file.

Use one uniquely created `0700` directory below the operating system temporary directory for session storage. Precreate each database file with `0600`; keep journals, indexes, and side files within the private directory with private permissions. Use the existing SQLite driver, explicit SQL, and typed serialization behind `internal/store`. No temporary object lives inside the canonical application state directory. Do not put keys or candidate values in filenames.

The temporary schema is internal and disposable. Its logical tables are:

| Table | Key and relationships | Required values |
| --- | --- | --- |
| `snapshots` | `snapshot_id` primary key | Fixed `as_of`, `max_age`, source database file identity, canonical state directory, complete flag |
| `candidates` | Primary key `(snapshot_id, candidate_id)`, snapshot relationship | Typed summary and exact key, first and last discovery times |
| `views` | `view_id` primary key, one snapshot | Canonical filters, complete match count, marked count and revision, retained flag and safe error code |
| `view_rows` | Primary key `(view_id, ordinal)`, unique `(view_id, candidate_id)` | Candidate identity, zero based position, marked boolean |
| `details` | `detail_id` primary key, current view and candidate | Own evaluation, complete summary, source and attempt counts, complete flag |
| `detail_sources` | Primary key `(detail_id, ordinal)`, unique source sighting ID within detail | Existing sighting fields |
| `detail_attempts` | Primary key `(detail_id, ordinal)`, unique attempt ID within detail | Existing attempt fields plus nullable saved batch detail |

Foreign relationships remain within this temporary database. There are no new keys or relationships in the live schema. Store the existing summary fields losslessly, including nullable evidence times and attempt references. Indexed ordinals support bounded first, last, and arbitrary page reads. Index marks by view and candidate so selected exports and refresh reconciliation never load all IDs into memory. Exact key comparison is parent internal and never a log field.

There is no total row cap or disk quota introduced by this feature. Memory is bounded by one traversal page, one row, and encoding buffers; a single stored record can still be large. Disk exhaustion fails preparation safely. Partial temporary databases are never treated as ready snapshots. Normal cleanup waits for users to release handles, then removes only session owned files and directories. It does not search for or remove other sessions' directories.

A successful swap may temporarily retain the old files until their handles close. If deleting them fails, the job still returns its committed new view as `succeeded`, with `warning_code=ui_snapshot_cleanup_failed` and the escaped remaining path. Install that view before displaying the warning. Cancellation after activation also preserves and returns the new view. The next requested preparation job first retries that owned cleanup in its `cleanup` phase. It may create no new resource until cleanup succeeds; failure ends that job with the same safe code and leaves the active view intact. This prevents repeated refresh from accumulating abandoned snapshots. Existing readable pages and quit remain available. A crash can leave a private directory; report it when known, without adding an automatic startup scavenger.

## Filters, marks, and focus

`Filters` has exactly `status: string[]`, `definitive: string[]`, and `freshness: string[]`. Allowed values and canonical ordering are:

| Field | Ordered values |
| --- | --- |
| `status` | `unchecked`, `confirmed`, `invalid`, `inconclusive` |
| `definitive` | `none`, `confirmed`, `invalid` |
| `freshness` | `fresh`, `stale`, `unknown`, `not_applicable` |

An empty array is unrestricted. Values within a field are alternatives; fields intersect. The wire accepts only unique canonical arrays. UI controls construct them, and Go validates them again before any work. Reuse `CandidateQuery.Normalize` and its matching policy through a shared internal function. Valid contradictory filters produce zero matches.

Filter application scans only the prepared results snapshot. It never reads newer live evidence. Build a replacement ordinal view in a temporary transaction, counting all matches. When the canonical filters actually change, activate the new view with no marks and focus its first candidate. An unchanged filter submission is a no op preserving marks and focus. A failure leaves the old view and marks intact. The screen shows the applied filters and fixed evaluation.

`results.mark` requires a candidate in the current view and explicitly sets its boolean value. Repeating the same set is harmless. Maintain marked count and an increasing mark revision in the same temporary transaction as each effective change; these provide bounded count replies and review invalidation. Space marks one focused row. Clearing marks is a separate explicit action. There is no mark limit and no list of all marked IDs sent through the protocol. Export matching already covers the whole view, so a mark all control is unnecessary.

Refresh builds a replacement collection from current live state using the same configuration, then rebuilds the current filters. Compare old and new candidates by both ID and exact key before retaining a mark or focus. An ID reused for another key is a different identity. Retain only identities still matching. Report the count of removed marks once after success. Never merge new summaries into an old snapshot.

The UI supplies its nullable focused candidate ID on refresh. The parent looks up its old identity and returns the same identity's new ordinal when present. Otherwise use the first matching ID greater than the former focused ID, or the last row if there is no successor. Empty views return null focus and offset. A first open or changed filter focuses the first row. Mark updates and page changes do not move focus. Returning from details restores the previous list position.

The Results view has no automatic refresh. Label its data `As of` even during a healthy session; freshness is not recalculated on a UI timer. After a failed refresh, retain the original view, position, marks, and evaluation and add `Retained after refresh failure` plus the safe explanation. This applies even if the live collection has disappeared. You may review an export of that retained snapshot. An initial missing collection has no snapshot and uses the existing missing state guidance.

## Detail snapshot

Opening details validates the current view and candidate first. The application opens one live read transaction for the candidate summary, every sighting, and every attempt including nullable batch retry metadata. Sample its own evaluation clock after establishing this transaction. Materialize those records into the temporary detail tables in bounded batches, then close the live reader before exposing the detail handle. No read transaction remains open while you inspect a history page.

Reuse the store's candidate, sighting, attempt, and batch detail query logic, but perform them inside this one transaction. Separate calls to the existing public paged APIs do not establish a common snapshot and are not sufficient. Add one concrete store traversal for this path. Order sources by ascending sighting ID and attempts by ascending attempt ID. All retained history is reachable; an unresolved attempt is included and labeled `Unresolved`, not a completed outcome.

The Results snapshot records the live database's validated filesystem identity during preparation. Detail opening checks that identity before opening and after establishing its read, and compares the candidate's exact key inside the transaction with the private Results identity. Missing, changed, or mismatched state returns `ui_collection_changed` or the applicable state error. The same account deliberately swapping files during access is outside the store's supported lifecycle; ordinary replacement and ID reuse must not silently bind details to a different candidate. Refresh Results to bind a new collection.

Show `Results as of` and `Details as of` separately. The detail summary is the newer detail summary and does not overwrite the list snapshot. Export always uses Results. Help and export review explain that boundary. Detail refresh discards the old detail and revealed key first, then creates a new detail snapshot. Leaving details also removes the detail snapshot.

If summary, source, or attempt preparation fails, discard the entire partial detail. Show the already available Results summary and the failed phase from `JobState.phase`. Mark detail sections unavailable or not loaded, never empty. Allow retry, return, and quit. There is no cached history fallback. A reveal needs a successfully opened detail; its key still comes from the matching Results candidate, whose exact identity was checked above.

## Closed detail records

The following types use the umbrella's `Decimal`, `Stamp`, `DisplayText`, and nullable conventions. Required timestamps cannot be invented. Nonsecret `DisplayText` shortening is labeled in the screen. Keep stored fields exact in private snapshots; shorten only their presentation projection.

| Record | Exact fields |
| --- | --- |
| `Summary` | `id: Decimal`, `first_seen_at: Stamp`, `last_seen_at: Stamp`, `latest_outcome: Outcome`, `latest_reason: Code?`, `last_definitive: 'confirmed' or 'invalid' or null`, `evidence_at: Stamp?`, `freshness: Freshness`, `clock_anomaly: boolean`, `latest_attempt_id: Decimal?`, `definitive_attempt_id: Decimal?`, `active_attempt_id: Decimal?` |
| `SourceRow` | `id: Decimal`, `candidate_id: Decimal`, `source_id: DisplayText`, `message_id: DisplayText`, `message_url: DisplayText?`, `published_at: Stamp?`, `first_seen_at: Stamp`, `last_seen_at: Stamp` |
| `AttemptRow` | `id: Decimal`, `candidate_id: Decimal`, `run_id: Decimal`, `backend_id: DisplayText`, `backend_version: DisplayText`, `started_at: Stamp`, `finished_at: Stamp?`, `recovered_at: Stamp?`, `outcome: 'confirmed' or 'invalid' or 'inconclusive' or null`, `reason_code: Code?`, `evidence: Evidence?`, `clock_anomaly: boolean`, `retry: Retry?` |
| `Evidence` | `kind: 'account_type' or 'rejection'`, `observed_at: Stamp`, `account_type: DisplayText?`, `rejection_code: DisplayText?` |
| `Retry` | `attempt_number: Decimal`, `failure_class: 'temporary' or 'throttled' or 'terminal' or 'unsafe' or null`, `wait_ns: Decimal`, `eligible_at: Stamp?` |

`Summary` maps the same fields as the spec 0006 JSON summary, using private protocol encodings instead of public JSON numbers. Its latest reason comes only from the latest completed attempt. Its evidence time comes from the last definitive attempt. Inconclusive attempts cannot erase earlier definitive evidence. Freshness uses the saved evaluation and configured interval; the exact expiry is stale and future confirmation is unknown with a clock anomaly.

Source rows map existing `state.Sighting` fields. URLs are text only, with no automatic opening or terminal hyperlink controls. Attempt fields map `state.Attempt` except `RequestID`, which is excluded. Null outcome means the stored attempt has not completed. Evidence maps normalized stored evidence; the field irrelevant to its kind is null. No raw provider evidence is displayed.

Retry fields come from the optional `validation_attempt_details` record and existing `BatchAttempt` semantics. If there is no such record, the entire retry object is null and the UI says `Retry facts unavailable`. Within a present record an empty saved failure class maps to null, and a zero saved wait remains zero. Do not infer retry policy from today's settings or reconstruct a missing attempt number. Run ID and candidate ID must agree across joined records or the detail read fails.

## Screen design

Use the existing `ui/design.md`: terminal foreground and background, terminal monospace text, one cyan focus accent, visible `>` focus, and text status labels. Add no images, fonts, animation, or custom backgrounds. At fewer than 60 columns or 16 rows, replace content with the resize notice. Keep help, cancellation, quit, and progress observation available; do not begin a new export or reveal from that notice. Clear a revealed key when crossing below this threshold.

Overview gains `b Results` in its footer. Its existing section navigation stays intact. Pause its automatic timer when leaving; wait for any already entered overview request before opening Results. On return, trigger one overview refresh and resume the existing five second completion based schedule. Results, details, and export never run concurrent overview reads.

The Results screen contains a fixed header, filter and snapshot summary, full width candidate body, and fixed shortcuts. The header includes profile and local time context from the parent, `Results`, exact matching and marked counts, evaluation time, and retained or preparation state. At 100 columns show aligned columns for mark, ID, latest outcome, last definitive result, evidence time, and freshness. At 60 through 99 columns use labeled blocks. Never truncate an ID to fit. Reason and discovery times are available in details.

Up and Down move the focused candidate. Page Up and Page Down move by one visible viewport of candidates. Home and End select the first and last matching candidates. The UI maps logical ordinals to bounded page requests and retains only the current page and enough adjacent rows for one viewport. It never accumulates every previously visited page. Show `Rows X to Y of N`, derived from actual displayed ordinals, and a clear empty state for zero rows.

`Enter` opens details, `Space` sets or clears the focused mark, `u` clears marks, `f` opens filters, `e` opens export, `r` refreshes Results, `Esc` returns to Overview, `?` opens help, and `q` quits. While a page request is pending, queue at most the latest desired position. Focus remains on existing content with a loading marker until the reply is validated. Failed page reads leave the prior page visible with a safe error and retry action.

Filters are a separate screen with three labeled groups and checkbox values in canonical order. Tab changes groups, arrows choose values, and Space changes a draft checkbox. Empty groups say `Any`. Enter applies the draft, Esc cancels, and `c` clears the draft filters. Show that applying different filters clears marks. Do not alter the active view while editing.

Details is a separate screen with the candidate ID, both snapshot times, and Summary, Sources, and Attempts sections. Tab cycles sections. Arrows scroll, Page Up and Page Down move a viewport, and Home and End move to section limits through bounded pages. Source and attempt totals are exact within this detail snapshot. Restore each section's position while this detail stays open. `k` reveals or hides this candidate, `r` refreshes details, `Esc` returns to Results, and `?` and `q` retain their meanings. Export is entered from Results so its selection boundary remains clear.

Keep a revealed key only in the active detail component. Clear it when hiding, leaving, refreshing either snapshot, opening help, losing channel ownership, quitting, or crossing the small terminal threshold. A stale reveal reply must not restore it after any of those actions; bind completion to the detail handle and a local reveal generation. Escape control characters in a quoted representation, label it `Key`, and allow scrolling for long values. Do not claim secure erasure of JavaScript memory or terminal capture. Export is the exact byte delivery path.

In text inputs, printable shortcut letters are text, including `q`, `r`, and `e`. Esc cancels the form; Ctrl+C retains the process interruption contract. Enter activates only the focused form action. Focus, help return, resize, and no color behavior share the existing UI primitives.

## Value sourcing

| Display or action | Source |
| --- | --- |
| List rows and match count | Complete private snapshot and canonical filtered ordinal view |
| Marks and removed count | Old view marks joined to matching new candidate ID and exact key |
| Focus after refresh | UI supplied old focused ID, old identity, and new ordered view |
| Latest versus definitive evidence | Existing attempt ID order and summary derivation |
| Discovery and provenance | Saved candidate discovery aggregates and sighting fields |
| Attempt and retry history | Saved attempts and optional batch detail join within one detail transaction |
| Detail unavailable label | Safe job error and phase, never a zero history count |
| Revealed text | Exact private key for the validated current candidate |
| Page ranges and position | Returned offsets, exact total, current viewport dimensions |

## Rationale

Private materialization keeps the collection stable without retaining a live database transaction during human navigation. Reusing SQLite provides indexed pages and marks with the installed driver. Separate detail preparation avoids copying every candidate's complete history just to open Results, at the cost of clearly labeling a second observation time.
