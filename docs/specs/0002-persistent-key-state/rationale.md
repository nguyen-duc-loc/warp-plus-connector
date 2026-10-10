# Persistent key state reasoning

## Context

The current five Go source files implement help, version, and their verification and tooling tests. There is no database or runtime configuration layer. Scope item 3 needs durable candidate identity, message provenance, check evidence, and run history before the first crawl and validation path can use them.

The accepted foundation fixes Go, Cobra, SQLite through `database/sql` and `modernc.org/sqlite`, explicit SQL, local account access, WAL, and one operating system lock owner for writes. Real validation remains unsupported. The design needs to preserve uncertainty and separate source visibility from entitlement without anticipating a backend that has not passed its feasibility gate.

You selected a store foundation with no new CLI commands, exact key identity after trimming outer whitespace, every distinct source message, optional publication time, and separate crawl and validation runs. You chose to retain all attempts and observations, keep structured evidence without message text or raw validator output, preserve definitive evidence after temporary failures, and recover interrupted work automatically.

The local branch was clean at the start. Fetch completed, but no `origin/main` reference was available, so comparison with the remote base could not establish freshness. This spec is based on the local accepted architecture and scope. You chose no references section and no additional tool discovery.

## Options considered

### Option 1: Current state and channel summaries

Keep one row per candidate with its latest outcome and channels. This is the smallest implementation and makes listing cheap. It loses the message and attempt history needed to explain rediscovery, transient failures, and prior evidence, so it does not meet your selected requirements.

### Option 2: Relational history with derived summaries

Keep candidates, sightings, run observations, runs, and attempts. Compute the latest outcome, definitive result, discovery bounds, and freshness from retained facts. This preserves the selected history and supports ordinary SQL transactions, at the cost of growing history and more involved read queries. This is the chosen option.

### Option 3: Append every state event

An event log could reconstruct every change and support replay into different views. It provides strong audit detail, but requires event versioning, replay semantics, and projection maintenance. Those mechanisms add operational work without a requirement beyond the relational history already selected.

## Rationale

Source visibility and entitlement answer different questions. A message sighting explains where a candidate came from; an attempt records what a validator could establish at a specific time. Keeping both the latest completed attempt and the latest definitive result prevents a transport failure from erasing prior evidence or making it appear recent.

Message sized transactions preserve useful progress without holding a database transaction across source retrieval. The runner up, committing an entire crawl, simplifies whole run atomicity but discards earlier discoveries when a later message fails. Request tokens make retries of run and attempt creation safe after an uncertain acknowledgement. They are internal bookkeeping, not new product identity.

The signed off first and latest discovery fields are derived from observations rather than copied into multiple mutable columns. Attempt IDs order results because only one attempt per candidate can remain unresolved, so clock movement cannot reorder its evidence. A recovery timestamp records when interruption was detected without pretending to know when a process died.

One initial SQL migration keeps the confirmed model coherent while the first implementation task proves a complete save and read thread. A migration framework is the runner up if version management later becomes complex. The narrow macOS and Linux scope permits the existing Go syscall boundary for advisory locking; a locking library is the runner up if supported platforms expand or implementation evidence shows it materially reduces complexity.

Cursor pages bound memory and avoid keeping a database snapshot open while callers work. Fixing the starting maximum ID makes candidate traversal finite while the writer adds records. A persistent snapshot is the runner up when an export must freeze every detail; that decision belongs to the results feature. Run observations use their existing composite identity and are traversed only after the run is terminal, avoiding an extra identity column just for that list.

Private filesystem permissions match personal local operation and the requirement to recover keys for later export. Automatic permission repair is the runner up for convenience, but you selected explicit refusal. There is no claim of encryption or protection from other programs running under your own account. Refusing damaged or unknown state avoids disguising data loss as successful initialization.

All history remains because its real volume is unknown and you explicitly chose traceability over early pruning. There is no throughput promise or arbitrary total collection cap. The 338 record check proves traversal completeness, not a claimed live yield.

### Independent review

A read only review by a different model found no major blocker in recovery, evidence, writer ownership, or traversal. You approved its two recommended clarifications: initialization eligibility depends on a readable version zero database with no application schema objects, including a prior failed initialization; undefined run aggregate totals are deferred while per message insert counts remain. The verification plan also now reads both the candidate summary and message provenance in its first process restart check. You then confirmed the complete revised design on 2026-10-10.
