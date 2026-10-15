# Decision record

## Context

Scope feature 14 adds manual work to an existing terminal that already supports Overview, Results, Details, reveal, and export. The explicit crawl and check commands already own durable intent, retries, isolated validation, and recovery. Background execution holds a collection writer for its lifetime. The UI currently has one preparation job and a five second process shutdown limit intended for reads and export.

The design must let you keep browsing while work runs without adding a second writer or losing cancellation ownership. Selected keys can change between a fixed Results view and a later start. A stopped validator can require remote cleanup much longer than the terminal's current shutdown allowance. Existing report readers expose complete history but do not provide one fixed transaction across every paged call.

You chose a new spec, one manual operation at a time, explicit source and candidate review, configured check policy, advisory writer conflict guidance, immediate cancellation, confirmed quit, fixed reports, and independent export. You chose the current UI as the visual source and declined a References section. No new tool or library decision requires web research.

## Options considered

| Option | Benefit | Cost |
| --- | --- | --- |
| Extend existing app and terminal boundaries in place | Reuses tested coordination and keeps explicit commands working; small typed additions expose progress | Requires careful ownership and observation changes in shared engines and the process watchdog |
| Add service commands and route manual work to the existing runner | Manual work can be admitted without stopping the background service | Adds service communication, authentication of local requests, queue semantics, and deployment compatibility beyond the chosen scope |
| Run explicit CLI child processes beside Ink | Reuses the command entry point and can isolate crashes | Output parsing, inherited configuration, cancellation, terminal ownership, and duplicate start prevention become another protocol |
| Replace coordination with a new unified interactive scheduler | Could centralize every future work source | Replaces working retry and recovery code, expands risk, and duplicates a scheduler before a need is established |

## Rationale

The in place extension fits the existing single process Go coordination model. A service command channel is the strongest alternative if submitting work to an active service becomes a later requirement; it does not belong in this feature after the explicit choice to explain conflicts. One operation slot still supports concurrent checks inside a batch and avoids new crawl versus check policy decisions.

Typed polling is preferred to an event stream because the current protocol already serializes request and reply access. Progress can coalesce intermediate activity while durable reports retain all attempts. The alternative event stream would need ordered buffering and backpressure for observations that do not need replay.

Review handles freeze intent without putting large ID arrays or keys on the wire. Identity comparison under the writer prevents a stale review from checking another key. Reusing writer recovery before per candidate comparison preserves the current recovery contract, while checking the collection binding before recovery protects a replaced database. An admission path that validates every candidate before any recovery is possible but adds a new recovery order the user declined.

Fixed report snapshots extend the existing Results approach. They cost temporary disk and preparation time, but make header and page evidence coherent during active writes. Live page reads are a reasonable lighter alternative when separate timestamps are acceptable; the user chose fixed observations instead.

The 75 second hard limit for owned work preserves the existing 60 second remote and five second local cleanup contract with a bounded allowance for resource closure and final output. The read only five second watchdog cannot remain the operational limit without cutting off valid cleanup. Early terminal restoration keeps this process lifetime extension from trapping the terminal. No dependency, database version, queue, or new persistent preference is needed.

## Independent review

On 2026-10-15, a separate model read all five draft files without inspecting implementation or fetching references. It identified two blocking gaps: long preparation could hide operation cancellation, and a later receipt could obscure an earlier unresolved cleanup origin without a defined recovery clearing rule. You approved both recommended fixes. Global Ctrl+X and Ctrl+G now cancel work and preparation independently. Retained cleanup receipts survive later work until explicit validator reconciliation supplies complete, durably acknowledged recovery proof. The verification plan includes both regressions. This record does not claim implementation or runtime verification.

The same independent reviewer then checked the two applied fixes and found both findings resolved, with no new concrete blocker in that bounded followup. You separately confirmed the spec content on 2026-10-15 and authorized linking its milestones to scope feature 14. Implementation remains pending.

## Implementation inventory

The read only scout and main thread inspected the existing paths before drafting. This inventory records integration facts, not claims of completed implementation.

| Existing area | Relevant fact and required extension |
| --- | --- |
| `internal/app/crawl.go` | Saves complete plans and message commits; needs typed observations and the common manual ownership wrapper |
| `internal/app/batch.go` | Coordinates concurrent attempts and emits committed final items; needs bounded intermediate activity observations and frozen identity admission |
| `internal/app/shutdown.go` | Runner already shares 60 second remote and five second finalization windows; manual terminal operations need the same timing semantics |
| `internal/store/open.go` | Opens the writer, migrates and recovers before caller reads; collection binding needs verification under the lock before those changes |
| `internal/app/batch_read.go` | Complete saved reports use several read transactions; fixed report preparation needs one store owned traversal transaction |
| `internal/store/browse.go` | Existing private temporary SQLite and bounded pages provide the pattern for report snapshots |
| `internal/cli/terminal.go` | Creates a read oriented App; source and configured validator wiring must be added without external work on startup |
| `internal/terminal/results_dispatch.go` | One cancellable preparation job with a five second cancellation watchdog; operational execution must own a separate slot |
| `cmd/wpc/main.go` | Hard terminal timeout is currently five seconds; operation guard must choose the active work deadline atomically with mutation admission |
| `ui/src/screen.tsx`, `ui/src/results-screen.tsx` | Existing screens and request ownership need a shared scheduler, work indicator, forms, report pages and quit modal |

Preflight found 198 source files, a clean worktree, and zero commits behind fetched `origin/main`. The selected approach is Tracer Bullet and Beta, inherited from feature 14 and the project defaults. Existing specs 0010 and 0011 remain separate records.
