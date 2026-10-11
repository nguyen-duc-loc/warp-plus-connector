# Complete results and export rationale

## Context

Scope feature 7 requires access to the complete matching collection, useful script output, consistent status and freshness filters, and explicit limits. The existing CLI already traverses every candidate in pages of 100, but provides only text and no filters or export destination. Its store reads current details separately for each page, so a changing database can yield mixed evidence across one invocation.

The application already distinguishes the latest completed outcome, last definitive evidence, unresolved attempt, and derived freshness. These meanings must survive export. The product remains a personal Go CLI on macOS and Linux arm64, with private local SQLite state and concurrent readers. This feature does not need new providers, dependencies, or database records.

## Options considered

| Option | Benefit | Cost |
| --- | --- | --- |
| Extend the existing page loop directly | Smallest change; reuse current callbacks and stream immediately | Different pages can reflect different commits; query failure can leave partial output |
| Add snapshot traversal beside existing reads, then share it between list and export | Fixed results and filters; reuse domain semantics; existing history APIs remain compatible | New transaction lifetime and temporary output ownership need explicit failure handling |
| Replace the read layer with a general query and archive system | Could support future history archives and broader searches | Expands beyond candidate summaries, duplicates established behavior, and increases migration and review risk |

## Rationale

You chose candidate summaries, JSON and key lines, stdout and named files, all candidates by default, three independent result filters, an optional limit, and fixed snapshots for both commands. A new snapshot operation meets those choices without changing internal paging contracts used elsewhere. The CLI adopts the shared path once the first export thread works; no feature flag or database migration is needed for this small local command surface.

You also chose private staging before delivery. Streaming directly from a snapshot avoids temporary disk use but allows slow consumers to prolong database reads and exposes partial data on query failure. Staging bounds memory, isolates database work from stdout speed, and lets named files appear complete. It cannot make stdout copying atomic, and its disk and cleanup costs remain explicit.

Application filtering reuses the exact candidate summary rules rather than introducing parallel SQL freshness expressions. A future measured query bottleneck can justify SQL pushdown with equivalence tests; the controlled 338 candidate criterion does not justify a new query framework today. The limit probe finds only one further match, avoiding an unnecessary exact total count.

JSON hides keys by default, while choosing key format explicitly asks for them. This respects your disclosure choices and keeps a useful one flag key export. Failing on unrepresentable key lines preserves exact candidate identity; escaping or skipping could silently change the selected collection. Existing local file permissions remain the protection boundary.

Atomic creation without replacement is necessary because a preliminary existence check can race. Linking a completed private file within the destination filesystem supplies that property on the supported platform filesystems. Replacing rename and direct destination writes were rejected because they can destroy an existing path or expose incomplete content. Publication uncertainty is reported explicitly because a later sync or cleanup failure cannot undo an already visible complete file safely.

No web research or References section was requested. The design uses inspected project contracts and the confirmed conversation choices.

## Independent review

On 2026-10-11, a separate model reviewed decision completeness and soundness without modifying files. It found the value sources complete and one material ambiguity in destination path resolution. You approved resolving parent and state directory symlinks before containment checks, retaining the final filename for refusal of existing paths, and using the canonical parent for staging and publication. You also approved wording that cancellation checks stop further writes but do not promise immediate interruption of an entered stdout write. These fixes are included in the build spec and its verification scenarios.
