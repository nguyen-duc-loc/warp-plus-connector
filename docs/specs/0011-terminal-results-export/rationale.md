# Decision record

## Context

Scope feature 13 follows the completed terminal overview. The existing CLI already reads and exports complete candidate summaries, while Ink currently shows aggregate collection information. You need a keyboard path from those totals to every candidate, its evidence, and a usable export.

The backend uses Go, Cobra, SQLite, and existing private output staging. The packaged UI uses pinned Node, Ink, React, and TypeScript on Linux amd64 and arm64. Background writers can commit while you inspect data. Ordinary store cursors bound candidate IDs but read newer details on later pages, so they cannot alone provide the fixed filtered view you selected.

You chose a new spec 0011, fixed results until manual refresh, all candidates initially, all three existing filters, source and attempt detail, and both matching and marked exports. You chose private temporary disk snapshots, session only preferences, a separate detail snapshot, a full width list, the existing visual design, and file only export. You also chose retained results after refresh failure, marks retained across matching refresh results, and cancellation on quit. The data model was confirmed during design. No new compliance system or external integration is involved.

## Options considered

| Option | Benefit | Cost |
| --- | --- | --- |
| Extend existing Go and Ink paths with private snapshots | Preserves CLI behavior and evidence rules; gives stable complete browsing with bounded memory | Adds temporary storage, snapshot lifecycle, and protocol methods |
| Build a parallel UI service and migrate callers gradually | Separates a future richer interface and could serve other clients | Adds an unnecessary service, authentication boundary, and duplicated operational concerns for this local tool |
| Replace the command and terminal paths with one new interface | Could unify all interaction code | Risks established scripts, publication guarantees, and process behavior without a demonstrated need |

The first option is selected. Within it, holding a live transaction would avoid a copy but keep database history pinned during human navigation. Keeping the full collection and marks in memory would be simpler but violate the chosen bounded memory behavior. Private temporary SQLite tables reuse the installed driver and provide ordered pages, exact counts, and selected set queries.

## Rationale

The important boundary is a fixed dataset that survives ordinary navigation and feeds export. Materializing it once costs disk space and initial work, but it makes filter membership, evidence time, counts, and export deterministic while releasing the live reader. The temporary keys stay under the existing account boundary and reach the child only through explicit reveal.

Capturing every candidate's full history during initial load would make basic browsing proportional to all historical activity. A separate detail snapshot captures only the candidate you inspect. Two clear times are the honest cost of that choice. Exact candidate identity and database identity checks guard ordinary collection replacement and ID reuse.

Parent jobs preserve responsive cancellation and the existing short protocol watchdog without imposing a total collection limit. A single job slot keeps ownership understandable. The UI review binds the export to known data and options; the existing encoder and atomic publication implementation remain the authority for what happens on disk.

The enhancement ships through the existing archive as one matching executable and UI. No live data migration or persistent preference migration is required. Rolling back means restoring the earlier complete archive; files already exported remain ordinary user files. A feature flag would add configuration without providing a useful rollout boundary for this personal local interface.

## Implementation evidence

The read scan found 179 source files excluding generated runtime trees. `App.QueryCandidates` already provides complete callback traversal, canonical filters, fixed evaluation, and reader closure. `Reader.VisitCandidates` supplies the actual transaction. Existing `ListSightings`, `ListAttempts`, and batch detail queries supply the detail facts, but require a shared transaction wrapper for a fixed detail snapshot.

`candidateEncoder` and `outputStage` currently live in `internal/cli`. Their private visibility calls for a local reusable operation and injection into terminal startup, not a copied export engine. The existing terminal has only hello and overview, strict frame validation, one outstanding request, and independent watchdogs. Protocol 2 explicitly extends those restrictions while preserving them for ordinary replies.

Repository freshness was checked against the fetched `origin/main`, with zero missing commits. The working tree was clean before this design. No implementation, runtime verification, or new hosted execution is claimed by this record.

## Independent design review

A separate model reviewed all five draft files on 2026-10-15 without implementation inspection or web access. It found two gaps: committed session changes could lose their result when later cleanup failed, and polling was not explicitly continued during cancellation. You approved both recommended fixes. The contract now distinguishes activation from cleanup warnings, preserves committed results after cancellation, continues polling in both active states, and fixes the cancellation deadline on the first request. Verification includes those boundaries. This is design review evidence, not runtime acceptance.
