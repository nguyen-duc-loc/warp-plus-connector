# 0001. Stack and architecture

**Date**: 2026-10-10
**Status**: Accepted
**Platform amendment**: Confirmed on 2026-10-12 after independent review. Implementation and new runtime evidence remain pending under spec 0008. The lifecycle status retains the completed foundation scaffold.

## Summary

Your CLI targets Linux amd64 and arm64, using Go, Cobra, and a local SQLite database. One process owns writes while other processes can list and export saved results. Background operation uses a systemd user service. The existing isolated Docker validator needs matching amd64 support and runtime proof; macOS verification is outside the release requirements.

## Requirements

The approved first version scope includes Linux amd64 and Linux arm64 only. Existing command, storage, isolation, and recovery contracts remain in force. The executable acceptance criteria for this amendment are spec 0008 AC-17 (release artifacts), AC-18 (validator compatibility and proof), and AC-19 (existing behavior on both Linux targets), together with its AC-1 and AC-16 for native service operation. The [platform checklist](verify.md) records the required evidence; architecture writing does not mark any check passed.

## Decision

**Chosen option**: One local Go executable with SQLite, JSON configuration, and a separate validator feasibility gate. This keeps personal operation small while protecting your existing WARP connection. (basis: [project scope](../../scope/scope.md), confirmed design choices, single writer ownership)

This is the foundation decision for scope feature 1. It governs the scaffold and later feature designs, rather than specifying every later feature. You confirmed the original content on 2026-10-10 and limited that scaffold to macOS and Linux arm64. Its completed verification remains historical evidence. Your scope decision on 2026-10-12 replaces those targets with Linux amd64 and Linux arm64 only. This amendment updates the existing design in place; it does not claim the added architecture has shipped. You declined additional community skills and MCP servers for this stack. Use the existing project workflow skills and ordinary Go tooling; do not repeat those offers without a new request.

**Library policy**: Use Go libraries when they make the code simpler or more readable. Standard library choices here are defaults, not a rule against dependencies. Prefer a maintained library over custom parsing, retry, locking, or database helper code when it removes meaningful complexity. Record the reason and tested version when adding one, and preserve the selected configuration and ownership contracts. No additional package is needed just to populate the scaffold. (basis: your explicit library preference)

## Proposed stack

| Layer | Choice | Reason |
| --- | --- | --- |
| Product | Personal CLI for Linux amd64 and arm64 | Matches your scope; no hosted application is needed. |
| Language | Go | Your fixed choice; cancellation and concurrent work can share one runtime. |
| Command framework | Cobra | Your fixed choice; command parsing stays separate from application work. |
| Packaging | One executable named `wpc` | Manual and background entry points reuse the same application code. |
| Persistence | SQLite through `database/sql` and `modernc.org/sqlite` | Relational state without a database service or C compiler requirement. |
| Queries | Explicit SQL behind a store boundary | Keeps transactions and complete export queries visible; no ORM is needed. |
| Configuration | JSON through `encoding/json`, with explicit CLI flags taking precedence | Uses the standard library and avoids another configuration framework. |
| Public source access | Go HTTP client behind a source adapter | Crawling can change without changing stored candidate identity. |
| Scheduling | In process scheduler, with eligibility and history persisted in SQLite | Restart recovery depends on saved facts rather than timers alone. |
| Background operation | Foreground `run` command supervised by user `systemd` | The operating system owns process restart and output capture. |
| Diagnostics | `log/slog` to stderr, with key values removed | Preserves stdout for command results and exports. |
| Access boundary | Your operating system account and private local files | There is no remote API, login service, or tenant model. |
| Validation | Explicit isolated Docker adapter, with `unsupported` as the default | Existing arm64 evidence is retained; amd64 needs matching image, worker, and runtime proof under spec 0003. |

The observed local toolchain is `go1.27.2 darwin/arm64`. This is an environment observation, not a claim about the latest supported Go release. The scaffold records its tested toolchain and exact dependency versions in `go.mod` and `go.sum`. Release targets are exactly `linux/amd64` and `linux/arm64`. The earlier amd64 deferral is withdrawn. macOS artifacts, launchd integration, and native macOS verification are outside the first version. Compilation alone does not prove runtime behavior on a target.

### Linux platform contract

| Surface | Supported target and proof |
| --- | --- |
| Release executable | `linux/amd64` and `linux/arm64`, CGO disabled, unchanged version metadata, archives and checksums |
| Foreground CLI and storage | Run the existing command, persistence, migration, cancellation, privacy, and complete result contracts on each architecture |
| Background service | A real Linux systemd user manager on each architecture, including login activation and failure recovery |
| Docker validation | Linux executable, local Linux daemon, image, and worker use the same supported architecture, with immutable local image IDs |
| Development host | May differ from the release target; local macOS checks are optional evidence and never a release gate |

Keep the existing module, dependencies, command surface, JSON settings, and ownership boundaries. Release packaging sets `GOOS=linux` and `GOARCH` to each supported architecture. It emits no Darwin artifact. The worker is delivered inside its matching validator image, not as another public CLI. No CI provider or automatic publishing is selected here.

Native runtime evidence comes from Linux machines or full Linux virtual machines of each architecture. Record kernel, distribution, architecture, Go toolchain, systemd and Docker versions where used. Building for another architecture and running isolated package tests in containers are useful checks but cannot replace native service proof. Run formatting, vet, race tests, lint, and the release configuration check on Linux; race test prerequisites belong to the test environment and do not change CGO disabled release builds. The existing formatter may use Linux `goland.sh` or `GOLAND_BIN`.

Keep existing macOS code and fixtures where they remain useful for development; removing them is not required to deliver Linux support. Service and worker platform admission explicitly support only Linux amd64 and arm64. Help and version retain their lack of runtime side effects. No broad source rewrite, schema migration, or automatic movement of macOS state is part of the transition.

Private Linux state, shared validator recovery, and service bindings keep their current paths and formats. Stop a managed service before replacing its executable at the bound path. Retained validator obligations must be reconciled on their recorded daemon with their recorded containers before retiring that environment. Copying files to another architecture or changing an image never proves cleanup. Unresolved identity or cleanup remains a blocking error.

The precise image build, architecture normalization, compatibility, and recovery rules are in [the Docker backend](../0003-source-crawl-validation/0003-docker-validator.md#linux-architecture-amendment). Service behavior is in [spec 0008](../0008-background-operation-diagnostics/index.md). That feature owns implementation and verification of this transition, so the completed scaffold is not relabeled as new runtime evidence.

### Application boundaries

Keep a single Go module. The scaffold needs only `cmd/wpc` and `internal/cli`; introduce the following boundaries when their first real path requires them, rather than generating empty packages.

| Boundary | Responsibility | Constraint |
| --- | --- | --- |
| CLI | Parse flags, render output, select exit status | Does not contain crawling, SQL, or registration logic. |
| Application | Coordinate crawl, check, save, and read operations | Accepts a cancellation context; owns process lifecycle. |
| Configuration | Resolve and validate settings | Completes before opening state or contacting sources. |
| Domain records | Candidate identity, provenance, evidence, and run results | Do not depend on Cobra, HTTP responses, or SQLite driver types. |
| Source adapter | Retrieve public messages and extract candidate strings | Reports source failures and coverage; does not assert entitlement. |
| Store | Transactions, deduplication, history, and queries | All SQL and migrations remain behind this boundary. |
| Validator adapter | Report capability and attributable check outcomes | Cannot use the host WARP registration as isolated state. |
| Scheduler | Select eligible work and avoid duplicate active jobs | Calls the same application operations as manual commands. |

Recommend small interfaces only at external boundaries, particularly the source, store, and validator. Concrete functions are the runner up for internal coordination and should remain the default there.

### Process and persistence ownership

One process holds an operating system advisory lock at `<state-dir>/writer.lock` for its entire write session. Nonblocking lock acquisition reports a busy error only when another process holds the lock, without queuing or performing network work. Permission errors and other filesystem or lock errors report a state access failure instead. The lock is held by an open file descriptor, not inferred from a PID file; the operating system releases it when the process exits. A stale filename is not a held lock.

The background process is that writer while `run` is active. Manual crawling or checking requires stopping it first. Reads and exports may run concurrently. No local control server or remote worker protocol is part of this decision.

Use SQLite WAL mode (a separate write journal that lets readers continue), foreign key enforcement on each connection, and short write transactions. Serialize database writes inside the owner process, even when source retrieval or future validation work runs concurrently. Never hold a database transaction across a network request. Reads do not run migrations or modify application records. Only the lock owner can initialize or migrate state. Unsupported newer schema versions produce an explicit error.

Store the database as `<state-dir>/state.db` on a local filesystem. Network shares and concurrent access from different hosts are outside this architecture. State directories use owner access only, and database, journal, configuration, and explicitly created export files receive private permissions. Keys are recoverable for your requested export; filesystem permissions provide protection, not database encryption.

The persistent state feature owns the actual schema and migrations. Its derived model must cover candidates, source provenance, check evidence, and run history. Rediscovery adds provenance without replacing check history. Discovery time, check time, and freshness are separate facts. A temporary failure never turns prior evidence into a definitive rejection.

### Configuration boundary

Resolve the default configuration file as `os.UserConfigDir()/warp-plus-connector/config.json`. Resolve default state on Linux under `$XDG_STATE_HOME/warp-plus-connector`, falling back to `~/.local/state/warp-plus-connector` when that variable is unset. Resolve home paths through the operating system account, not shell expansion. Explicit `--config` and `--state-dir` flags allow separate profiles.

Effective settings follow this order: built in defaults, JSON file, explicitly supplied flags. No custom environment variable configuration layer is introduced. A missing default file uses defaults; a missing explicitly named file is an error. Reject malformed JSON, unknown fields, trailing documents, and invalid values before side effects. Read settings once at startup; applying edits requires a restart.

Configuration owns enabled public channel names, crawl coverage, crawl interval, recheck interval, request limits, retry limits, and validator selection. The existing interval defaults remain `1h` and `6h`. The relevant later feature specs define those field names and numeric limits before implementing them. Validator selection defaults to `unsupported`; explicit `docker` follows spec 0003 and its current batch and recovery extensions. A requested backend or architecture that cannot satisfy the compatibility gate fails explicitly. Configuration stores no harvested keys or Telegram login credentials.

### Validator feasibility gate

The original experiment used a separate Linux environment with its own WARP daemon and registration. Spec 0003 records the implemented Docker backend and arm64 evidence. The same gate applies to the new amd64 runtime. Local isolation alone does not release a subscription device slot. This amendment performs no registration change.

The initial evidence target is the documented Linux `Account type: Unlimited` result after applying the candidate to that isolated registration. This defines the first supported confirmation class. Other possible Plus account forms remain inconclusive until their evidence is separately established. Syntax matches, channel labels, ordinary connectivity, and command success alone are insufficient.

Before enabling a real backend, a focused validator design must record all of the following:

1. The concrete runtime environment, commands or API, and supported versions. Proof on Linux arm64 does not establish Linux amd64 support. If the eventual CLI calls another host, its transport and authentication need an explicit design before implementation.
2. An attributable link from the requested candidate to the isolated registration and returned entitlement evidence, including check time and backend version. Routine diagnostics never reveal the full key or registration credentials.
3. The attachment effects and a tested cleanup procedure, including evidence that the temporary device no longer consumes a slot. Deleting local files or destroying the VM is not proof of remote cleanup.
4. Durable recovery information recorded before remote changes, plus handling for timeout, process termination, and partial cleanup. An uncertain cleanup result halts further attachments until reconciled.
5. Stable classification of confirmation, definitive rejection, and inconclusive results. Device limits, rate limits, transport errors, unknown output, and exhausted retries are inconclusive unless a documented response proves otherwise.
6. The isolation unit, measured resource cost, request limits, and tested concurrency ceiling. Begin the experiment with one candidate at a time; this is a laboratory constraint, not a production concurrency commitment.

If any item fails, the adapter remains unsupported and the complete crawl and check loop remains unfinished. The installed host `warp-cli` is never a fallback validator. Unofficial consumer APIs were not selected by this decision.

### Output, failure, and value sources

The scaffold exposes working root help and a `version` command without opening a database, creating state directories, loading configuration, or contacting the network. Root invocation with no subcommand shows help. Set the executable version from build metadata, with `dev` as the explicit local fallback; include a commit only when available rather than inventing one. Unknown commands return an error. Later runtime commands belong to their own feature specs.

| Produced value | Authoritative source |
| --- | --- |
| Help text | Cobra command and flag definitions present in the executable |
| Version | Build supplied version, or the literal `dev` fallback |
| Configuration and state locations | Explicit flags, otherwise the platform rules above |
| Writer busy result | Nonblocking advisory lock acquisition reports contention for that state directory |
| State access failure | Permission, filesystem, or other lock error distinct from contention |
| Candidate and provenance | Parsed source text and its source message identity |
| Entitlement and check time | A proven validator response and a timestamp from the application host's UTC wall clock |
| Freshness | Persisted check time, the effective recheck interval, and current UTC time from the application host's wall clock |
| Future run eligibility | Persisted work history and effective schedule settings |
| Export collection | The full matching database query, with only explicit user limits |

Check timestamps and freshness comparisons use the application host's UTC wall clock. The persistent state and scheduling specs define handling for backward clock changes and timestamps in the future before those features are implemented.

Use cancellation contexts for requests and jobs. Graceful shutdown stops new work, cancels active work, saves completed results, and releases ownership within a bounded timeout defined by the background operation spec. Interruption recovery relies on saved state, not a claim that every in flight remote operation succeeded.

The first vertical slice proves one source, one candidate, one supported check, persistence, and a read after restart. No fixed result cap, speculative cache, broker, public API, or dashboard is introduced. Telegram public page parsing is an adapter with visible coverage and failures, not an assumed stable API.

## Consequences

**Benefits**: You operate one executable and one local database. The chosen boundaries allow source parsing and validation to evolve without replacing the command interface or persistent record model.

**Costs**: JSON has no comments. Manual writes wait until background operation stops. You maintain one native service integration and verify it on two Linux architectures. Separate Linux validation may consume substantial resources and may prove infeasible; automation is not yet guaranteed.

**Limits**: The scaffold can be built now, but a successful scaffold does not establish validation support or complete the core loop. Detailed schemas, source parsing, job limits, exports, and service commands remain owned by their existing scope features.

## Follow-up

The platform transition is unfinished implementation work tracked by spec 0008 AC-17 through AC-19, alongside its service milestones. Completed foundation evidence below does not close that work.

1. Complete: root `AGENTS.md` records your additional skill and MCP declines and your Go library preference. No additional community skill or MCP server was installed.
2. Complete: the help and version scaffold is implemented, verified, and tested for macOS and Linux arm64. The recorded Tracer Bullet approach and Beta workflow remain in force.
3. Complete: root `AGENTS.md` records conventions from the scaffold, and the tooling feature is marked done in scope.
4. Complete the added amd64 validator proof and remaining Linux service checks under spec 0008. Retain the arm64 evidence and recovery contracts already recorded in specs 0003, 0005, and 0007.
5. Align release configuration, platform guards, image preparation, runtime tests, and user documentation during implementation. After the change, `/sync` updates root and nested `AGENTS.md` to the actual supported and verified targets. This architecture pass changes no code or project context files.

## Rationale

Reasoning, alternatives, and verified sources are in [rationale.md](rationale.md).
