# File export

## Summary

You choose matching or marked candidates, a format, and a destination. A review binds those choices to the fixed Results snapshot before you create the file. Go prepares all bytes privately and publishes a complete file without replacing an existing path.

## Selection and review

Open export from Results. Default to `matching`, JSON, and keys hidden. Keep the path input empty on first entry. A failed attempt may preserve the typed draft for editing, but never automatically submit it again. There is no clipboard, stdout destination, history archive, CSV, source filter, or UI row limit in this feature.

The selector is one of two sets: every row in the current filtered view, or every marked row in that view. Both are ascending candidate ID order and contain no duplicates. Matching zero rows is a valid export. Marked zero rows returns `ui_export_selection_empty` and asks you to mark candidates or choose matching. Marks outside current filters cannot exist.

Use JSON or key lines. JSON has an independent `Include keys` checkbox, initially off for every newly opened export form. Key lines visibly state that they contain complete keys and set `include_keys=true`; the checkbox is not separately offered for this format. Revealing a key in details never enables export disclosure. Changing back to JSON resets disclosure to false.

After the draft is submitted, `export.review` runs destination validation through the parent job slot. The parent derives the selected count from the fixed view, validates the combination, and resolves the destination. It does not read newer live candidate data, create an output stage, or publish anything. The review contains the canonical absolute destination and the original Results evaluation, including a retained snapshot warning when applicable.

The review shows selection mode, selected and matching counts, filters, evaluation time and freshness interval, JSON or key lines, whether keys are included, and the resolved destination. State that the file is created privately and never replaces an existing path. State that interruption during publication can leave a complete file even when the UI cannot report completion. Provide `Back` and `Create file`, with Back initially focused. Only activating Create file sends `export.start`.

Store the review in parent memory, bound to snapshot ID, view ID, the marked set revision, selector, format, disclosure, resolved path, and selected count. The child receives only its review handle and display facts. Any mark, filter, refresh, selection, format, disclosure, or path change invalidates the review. Back discards it. The parent verifies the binding again before accepting start. Consume it at admission so repeated start cannot publish twice. Repeating a consumed start returns `ui_handle_stale`, with no new work.

## Destination rules

Retain spec 0006 policy. The path is required; empty, `-`, and a trailing path separator are invalid. Relative paths resolve from the parent's invocation directory. There is no shell expansion, tilde expansion, environment interpolation, automatic directory creation, or automatic filename suffix. Preserve spaces as entered. Display the resolved path before creation.

The existing destination parent must resolve to a canonical directory outside the canonical state directory. Reject state aliases through parent symlinks. Do not resolve the final filename, since an existing symlink is an existing destination and must not be replaced. Preserve the existing file, directory, and symlink refusal. Review checks are advisory about availability; publication itself performs atomic refusal.

Retained snapshots remain exportable when the live state directory has disappeared. For the UI adapter, exclude both the canonical state directory captured during successful snapshot preparation and the current canonical location of the configured state path. Resolve the latter through its existing ancestors without creating anything when absent. If it cannot be resolved safely, fail destination validation. This check reads path metadata only and never requires the live database to reopen. Pass those resolved exclusions into the shared destination helper; keep the explicit CLI's existing requirement and error behavior unchanged. Compare directory components for each exclusion.

Store the canonical parent and final name from review. At start, repeat existing parent and state safety checks and ensure the resolved parent and its filesystem identity still match the reviewed directory. A changed parent fails with `export_destination_invalid` and requires another review. Use the canonical parent for staging and publication. As with existing export, an authorized local account actively replacing ancestor directories during individual filesystem calls is outside the path ownership guarantee; do not expand this feature into a new filesystem security model.

The request remains limited to 4 KiB including JSON escaping. If the entered path cannot fit, show `Path is too long for this interface` before sending. Do not shorten or silently rewrite it. Existing explicit CLI export remains available for paths this UI cannot represent.

## Preparation and publication

The injected CLI export operation accepts a typed review and an immutable row source owned by the Results session. It reuses the current candidate encoder and `outputStage` implementation. Extract their control flow where necessary so both the CLI and UI receive the same typed delivery facts without printing over Ink. CLI parsing, public flags, stdout behavior, and final diagnostics retain their existing contracts.

Iterate the reviewed fixed view with bounded pages, intersecting marks only for marked selection. Read exact saved summary values and keys from the private snapshot. There is no live collection query during export. The row source holds its snapshot reference until entered iteration returns; no cancellation handler closes it underneath the encoder. Application selection and error identity remain outside serialization.

Create the existing uniquely named `0700` staging directory and `0600` prepared file in the resolved destination parent. Serialize all rows, final metadata, and the terminating JSON newline where applicable. Flush the encoder, close the temporary snapshot reader, and check every error before delivery. Sync and close the prepared output file before publishing.

Use the existing atomic hard link creation with no replacement. The successful link is the publication point. Capture `published=true` in the parent receipt immediately. Sync the destination directory, remove the staging link and directory, and report completion only after cleanup succeeds. A link failure never falls back to truncation or replacing rename. Unsupported atomic creation returns the existing publication failure code.

Errors before publication delete only owned staging paths after their users stop. Errors after publication keep the complete destination. Directory sync failure returns `export_commit_uncertain`; cleanup failure returns `export_cleanup_failed`. Both keep `published=true`. Preserve the primary error if cleanup also fails and include the remaining owned directory when known. A file is either fully prepared before publication or absent; there is no partial destination copy.

Keep file preparation counts truthful. `completed_count` counts fully encoded rows, and `expected_count` is the exact reviewed selection size. During sync, publication, and cleanup display the phase rather than claiming row progress means success. No progress event contains a key or arbitrary error. Normal browsing is suspended during review preparation and export; help, cancel, resize, and quit remain available.

## Public export format

JSON retains schema version 1, exact integer JSON IDs, UTC timestamps, nullable fields, summary field names, and key omission from spec 0006. JavaScript never constructs exported JSON or converts exported IDs through `Number`. The Go encoder reads exact typed values from the private snapshot. The UI private protocol continues using decimal strings independently.

Top level `as_of`, `max_age`, and `filters` describe the Results snapshot and current filtered view. `limit` is null. `returned_count` is the selected set size and `truncated` is false after complete preparation. The explicit marked selector narrows the query; unmarked rows are not truncation. Add one optional schema version 1 field on UI exports:

```json
"selection": {
  "mode": "marked",
  "matching_count": 338
}
```

`mode` is `matching` or `marked`; `matching_count` is the complete current filtered view count as an exact integer. This avoids describing a marked subset as all matches. The items themselves identify the exported candidates; no duplicate array of selected IDs is added. Existing CLI exports omit `selection` and retain their existing byte structure apart from unrelated established nondeterministic times. Existing consumers may ignore this optional field under spec 0006's compatibility rule.

Empty matching JSON has an empty items array and zero counts; empty matching key output is zero bytes. Key lines use the existing exact string plus LF encoding and existing rejection of invalid UTF-8, Unicode Cc controls, U+2028, and U+2029. Check only emitted keys. Never render escaped keys into key line output or silently omit an unrepresentable key. An unsafe key fails all preparation with `export_key_unrepresentable` and no destination.

## Export wire records

Records use the umbrella's `Decimal`, `Stamp`, and explicit nullable fields.

| Record | Exact fields |
| --- | --- |
| `ExportReview` | `review_id`, `snapshot_id`, `view_id`, `selection: 'matching' or 'marked'`, `selected_count: Decimal`, `matching_count: Decimal`, `filters: Filters`, `as_of: Stamp`, `max_age_ns: Decimal`, `retained: boolean`, `format: 'json' or 'keys'`, `include_keys: boolean`, `destination: string` |
| `ExportReceipt` | `destination: string`, `selection`, `format`, `include_keys: boolean`, `selected_count: Decimal`, `prepared_count: Decimal`, `published: boolean?`, `durability: 'not_published' or 'confirmed' or 'uncertain'`, `error_code: Code?`, `remaining_path: string?` |

`published=false` is a known absence of publication by this operation, not proof that no other file exists at that name. True means this operation created the complete destination. Null means publication was entered but its result could not be observed before forced shutdown. Before entering publication the receipt starts false. Durability becomes confirmed only after successful destination directory sync; true publication with failed or incomplete sync is uncertain. A later cleanup error does not undo a confirmed sync.

Regular terminal export completion is returned as a JobResult export receipt, including failed and cancelled operations once cleanup completes. A forced shutdown may prevent a reply, so the parent final diagnostic path uses the same receipt. Do not put a raw key, summary item, or request token in any review, progress, receipt, or error field.

## Cancellation and failures

Check cancellation before preparation, between records, before publication, and after entered filesystem operations. The Cancel action signals the current job and shows `Cancelling`; it is not immediate evidence that no file was created. Wait for entered operations before releasing their handles. The umbrella cancellation watchdog bounds an unresponsive job by ending the session, not by abandoning its goroutine and continuing work.

Cancellation before publication removes prepared output. Cancellation racing with a successful link returns a cancelled receipt with `published=true` and preserves the file. A disconnect after publication cannot erase the parent fact. A crash during the publication syscall has unknown outcome to the user; check the named destination before retrying. Repeating a new export at the same path still refuses replacement.

An expected export failure returns to an outcome screen with the safe code, known destination, count, publication fact, and cleanup guidance where relevant. A known published file is labeled `File created` even when durability or cleanup failed; it must not be labeled `Nothing exported`. A failed export with `published=false` says that this operation did not create a file. Unknown outcome says `File may have been created` and offers no automatic retry.

Success shows the destination, returned count, format, and whether keys were included, then permits return to Results. Do not open the destination automatically. Before a new export, run a new review; the old receipt remains available until the next operation or exit. Cancellation returns to Results only after its terminal receipt has been displayed and acknowledged.

## Value sourcing

| Review or outcome value | Source |
| --- | --- |
| Mode, format, disclosure, path | Explicit export form inputs validated in Go |
| Reviewed candidate set | Fixed view and its mark revision at review |
| Selection and matching counts | Indexed snapshot queries, not the number of rows loaded by Ink |
| Exported fields and keys | Exact private Results snapshot records |
| Destination and parent identity | Existing canonical destination resolution plus reviewed directory identity |
| Publication, durability, cleanup | Actual link, directory sync, and owned path removal results |
| Unknown outcome | Entered publication without an observed result before forced shutdown |

## Rationale

A review bound to immutable data makes the file match the collection you chose, even while background work continues. Keeping encoding and publication in the Go parent preserves existing file guarantees and exact integers. File only output avoids mixing result bytes with an active terminal or adding a clipboard integration.
