# Terminal foundation and overview rationale

## Context

You selected Ink for a terminal interface while retaining Go and Cobra for the backend and explicit commands. Scope feature 12 is the first usable screen. It needs real collection readiness, recent manual and scheduled work, saved eligibility, and service observations on Linux amd64 and arm64. Results browsing and work controls belong to later slices.

The current release contains one Go executable. Bare root invocation shows help. Existing readers do not initialize, migrate, or recover application state. A background writer can continue while readers use SQLite snapshots. Service status already treats installation, manager, runner, and work as separate observations.

The checkout contained 144 Go files and no UI package when inspected. It was clean and zero commits behind refreshed `origin/main`. No source code was changed for this design. Your selected workflow is Beta and the build approach is Tracer Bullet.

## Options considered

| Option | Benefit | Cost | Basis |
| --- | --- | --- | --- |
| Require host Node and invoke Go per refresh | Small release and simple process boundaries | Extra installation prerequisite, runtime drift, repeated process startup | Existing explicit CLI surface and [Ink runtime documentation](https://github.com/vadimdemedes/ink) |
| Bundle Node and production modules with a private Go channel | Predictable offline installation and direct reuse of Go reads | Larger archive, two runtimes, a versioned protocol and terminal cleanup contract | Your confirmed installation and process choices, Ink documentation |
| Compile an executable with Bun | Bundled runtime with architecture and musl options | Ink compatibility needs separate proof; Node compatibility is not complete | [Bun executable documentation](https://bun.sh/docs/bundler/executables) and [compatibility documentation](https://bun.sh/docs/runtime/nodejs-compat) |
| Package a Node single executable application | Fewer visible runtime files | Embedded assets and module loading require extra packaging and verification | [Node single executable documentation](https://nodejs.org/api/single-executable-applications.html) |

## Rationale

You chose the bundled Node option and accepted glibc 2.28 or newer for the UI. It follows Ink's documented runtime and removes host Node setup from personal installation. Compiled modules plus production dependencies preserve package assets without a bundler configuration or special embedded filesystem. The smaller host runtime option remains a valid alternative, but conflicts with your preferred installation experience. (basis: confirmed choices, Ink documentation, and [Node Linux platform requirements](https://nodejs.org/id/blog/migrations/v22-to-v24))

A Go parent with inherited pipes retains the existing configuration and privacy boundaries. There is no public API, socket lifecycle, second scheduler, or persistent backend process to operate. Repeated command invocation is the runner up; it would be adequate for an isolated static summary but repeats startup and makes future interactive coordination harder. No future mutation method is specified or granted here. (basis: root and nested `AGENTS.md`, application coordination, least privilege)

Collection counts and saved schedules use one transaction, while service state keeps independent observation times. This prevents a fresh looking composite built from unrelated database moments or a green service label inferred from old saved work. The latest outcome and last definitive confirmation remain separate because an inconclusive retry does not erase earlier evidence. (basis: specs 0002, 0006, 0007, and 0008; existing `GetOperationalWork` and candidate freshness code)

Integer strings preserve all Go IDs and counts through JavaScript. A closed schema reduces accidental key disclosure. One in flight refresh bounds work and avoids stale replies replacing newer ones. No new persisted cache is justified by the current scale. (basis: exact integer transport, snapshot consistency, and the confirmed no new persisted data choice)

## Evidence and limits

The code scan found reusable service component readers, `GetOperationalWork`, `GetSchedule`, and run report aggregates. It also found two gaps: no aggregate confirmation freshness counts and no combined ten run overview. Those belong in the store boundary. Existing `service.Status` reads the service bound collection, so the overview needs a service identity read that leaves collection selection to the application.

Official Ink 7.0.0 release notes establish Node 22 or newer and React 19.2 or newer as entry requirements. Later Ink release entries were visible during research, so this spec does not call 7.0.1 the latest release. The researcher observed Node 24.21.0 on the official download page. These observations are not tested dependency pins. The first build slice resolves exact compatible patches, commits them, and proves both packaged targets before the rest of the UI is built.

Ink documents input, raw mode support checks, unmounting, and terminal lifecycle facilities. Its use of Yoga makes preserving dependency assets preferable to assuming every package is JavaScript text. Bun and Node SEA remain alternatives with their own compatibility and asset requirements. None of this research proves the Go launcher, inherited descriptor protocol, no color rendering, signal cleanup, or this project's release archives. Those require local and native verification. (basis: [Ink documentation](https://github.com/vadimdemedes/ink), [Ink releases](https://github.com/vadimdemedes/ink/releases), and [Node downloads](https://nodejs.org/en/download/current))

## References

**Project sources**: [scope](../../scope/scope.md), [root context](../../../AGENTS.md), [application context](../../../internal/app/AGENTS.md), [store context](../../../internal/store/AGENTS.md), [service context](../../../internal/service/AGENTS.md), [diagnostic context](../../../internal/diagnostic/AGENTS.md), and the existing specs named above. Existing source evidence includes `internal/cli/root.go`, `internal/store/operational.go`, `internal/store/reads.go`, `internal/store/schedule_reads.go`, `internal/service/status.go`, and `.goreleaser.yaml`.

**Practices**: least privilege at a process boundary, exact integer transport, snapshot consistency, bounded work, and Tracer Bullet delivery.

**Verified links**: Ink runtime documentation and releases, Node Linux platform requirements and downloads, Node single executable documentation, and Bun executable and compatibility documentation are linked at the decisions they support above. They were checked during the design conversation. Later build and review work can use the recorded findings without fetching these reference pages again.

## Independent review

On 2026-10-14, a separate model reviewed all five draft files without editing them or fetching references. It identified five decision gaps: unbounded native cleanup, missing identity invalidation transport, incomplete presentation fields in the closed protocol, ambiguous nested service retention, and missing proof at the advertised glibc floor. You chose to apply the recommended fixes.

The revised contract separates request, terminal cleanup, and process exit deadlines; transports explicit service identity and cache policy outside nullable data; defines exact wire records and units; updates nested service observations independently under verified identity; and requires each packaged architecture to execute in a pinned glibc 2.28 baseline userland. These are design changes, not implementation or verification results.
