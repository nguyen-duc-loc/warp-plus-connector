# Concurrent checks and bounded retries rationale

## Context

The current application checks one explicitly selected candidate at a time. SQLite already preserves every attempt and prevents two unresolved attempts for one candidate. Docker holds a shared owner lock for an entire check and stores a single recovery manifest. Launching several existing checks would contend for that lock instead of producing useful overlap.

Independent candidates need isolated registrations. Provider mutations can succeed even when their acknowledgements are lost, and deleting local state does not establish remote cleanup. The pinned worker reports several broad errors and does not expose a trustworthy server retry delay. Those constraints make admission and recovery part of the concurrency design.

You selected deliberate IDs, two workers by default with a maximum of four, three attempts by default with a maximum of five, and a 15 minute candidate budget. One shared batch owns Docker across profiles. Telegram behavior and future scheduling stay outside this enhancement. There is no numerical throughput claim; verification must first measure overlap and ceilings.

## Options considered

### Extend the current application and worker protocol

Keep one coordinator and evolve the existing worker into separately authorized phases. This reuses private storage, SQL, evidence rules, and cleanup ownership. It requires a careful protocol and journal upgrade, and serial mutation commands limit peak throughput.

### Add a local permit service beside the current workers

Workers could request permission while continuing independently, supporting more overlapping mutation commands. This adds a service lifecycle, authenticated local communication, lost permit handling, and another recovery boundary for a personal CLI. It is the runner up if measured throughput later justifies that cost.

### Directly replace the checking path with a durable job system

A job system could supply queues, delayed work, and future scheduling in one framework. It would replace more code, require a larger migration, and couple this deliberate manual slice to scheduling decisions that are still pending. Existing SQLite transactions already provide the durable history this feature needs.

### Add goroutines and retries around the existing check call

This is a small code change and retains the current worker image. The backend lock still serializes checks, and removing that lock would corrupt shared recovery ownership. Broad retry wrappers also cannot determine whether an uncertain remote mutation is safe to repeat.

## Rationale

The existing boundary is useful: the application owns history, while the validator owns remote cleanup. A single coordinator preserves that boundary and gives mutation admission, cancellation, and uncertain writes one ordering point. A shared owner across profiles avoids a new distributed quota or key locking scheme. Standard Go tools are sufficient for at most four active sessions and a small explicit retry policy; a general retry library would still need this application specific lifecycle.

Pacing from observed command completion is deliberately conservative. It avoids claiming an exact remote HTTP rate or relying on clocks across containers. Separating attachment completion from later observation lets useful work overlap without admitting a second mutation while the first command is still running. The alternate permit service could offer more throughput, but it brings more recovery states than this feature needs.

Saved batch items distinguish work that never started from attempts that failed. Separate attempt details explain retries without rewriting the existing evidence record. Runtime monotonic clocks govern waits, while UTC times remain human history. Automatic resume would need a durable scheduling policy; an interrupted batch instead records the truth and leaves a later explicit selection to you.

Only known temporary failures justify another attempt after cleanup. Free accounts and device capacity can change later, but immediate retries do not establish that they will. Throttling without a trustworthy delay stops the batch, matching your preference over an invented cooldown. The optional server delay contract remains honest about the pinned adapter's limitation and is tested through a controlled backend.

## Inspected implementation

The repository contained 59 Go files at design time. Inspection covered application checking, CLI rendering and exit behavior, configuration, attempt transactions and schema, the Docker manifest and lock, worker phases, subprocess limits, and the existing source and validation specs.

`App.Check` currently creates one run and attempt. `Docker.Check` locks before reconciliation and holds ownership until return. Its worker applies a license once and polls for attribution before cleanup. The store's unresolved attempt index already enforces one active attempt per candidate. These are the concrete extension points; no new provider, database, or runtime framework is proposed.

The working tree was clean before drafting. Git fetch succeeded, but the remote exposed no tracking branches, so a behind count against `origin/main` was unavailable. No source code, real registration, or host WARP state was changed during design.

## Independent design review

You requested a read only cross check by another model, `gpt-6-astra`, on 2026-10-11. It found six substantive gaps. You approved the recommended corrections before they were applied.

| Finding | Applied resolution |
| --- | --- |
| Status and cancellation could contend for the operation lock | Atomic status reads and a separate cancellation marker bypass that lock; a bounded watcher cancels active commands while cleanup keeps its own context |
| Deadline and global stop precedence over evidence was unspecified | The host must receive validated evidence before its deadline and stop boundary; eligible evidence survives later expiry or stop if cleanup is proven |
| Ordinary cleanup and shutdown had no complete budget relationship | Ordinary cleanup has a 60 second allowance; shutdown clips collection without resetting it, including receipts and local container removal |
| Registry replacement failure was described as rollback | Failed replacement is an uncertain acknowledgement; recovery inspects the actual saved version and never restores an assumed old snapshot |
| Creation, identity capture, and disconnect had incomplete recovery phases | Explicit persisted intermediate phases name required identity fields and permitted cleanup actions |
| The finalization budget ignored entered SQLite work | Five seconds bounds submissions and cancellation; safe store close retains ownership until entered work completes |

The review found the selection, ownership order, bounded queues, fairness, retry ranges, and preservation of earlier evidence otherwise concrete. A focused read of the revisions confirmed that all six findings were closed, with no remaining material contradiction within those fixes. You accepted the final spec on 2026-10-11. This was a document review; implementation and runtime verification remain pending.

Document checks passed for required sections, local links, unique numbering, and whitespace. The repository formatting, vet, and race test hooks passed. Lint first encountered the documented sandbox package loading failure, then passed with toolchain and cache access, reporting zero issues. These checks validate the current repository; they do not establish the unbuilt feature's acceptance criteria.
