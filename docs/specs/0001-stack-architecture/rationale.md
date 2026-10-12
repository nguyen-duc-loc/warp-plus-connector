# Stack and architecture rationale

## Context

> Premise note: A publicly posted key and a working WARP connection do not prove Plus entitlement. Temporary registration can affect a subscription device slot. Host isolation and remote cleanup are separate requirements, and neither can be inferred from the presence of `warp-cli`.

Your [scope](../../scope/scope.md) describes a personal Linux amd64 and arm64 CLI with fixed Go and Cobra choices. It collects candidates from configurable public Telegram channels without login, preserves every distinct record within declared crawl coverage, checks entitlement, and runs on independent crawl and recheck schedules. Automatic key application to your normal connection is outside scope. (basis: project scope)

At the initial design review, there was no application source, manifest, existing spec, or root project context file. The directory was not yet a Git repository, so remote freshness could not be checked then. Scope feature 1 combined this architecture decision with a runnable scaffold, which is now implemented, verified, and tested. Root `AGENTS.md` now records project conventions, and Git has an `origin` remote. The project uses Tracer Bullet delivery and the Beta verification workflow. No throughput target, deadline, or managed hosting requirement was supplied.

## Options considered

| Option | Strength | Cost or constraint | Outcome |
| --- | --- | --- | --- |
| One Go executable, Cobra, SQLite through modernc, JSON config, native service supervision | Few operational dependencies; relational persistence; no C compiler needed for the SQLite driver | Platform service integration and explicit writer ownership are still required | Selected. (basis: project scope, confirmed choices, modernc documentation) |
| The same CLI with SQLite through mattn and TOML or YAML configuration | Mature C SQLite integration and configuration comments | Requires a C compiler and another parser; cross compilation needs more setup | Valid alternative, not selected. (basis: mattn documentation, explicit build dependency management) |
| One CLI with JSON state files and a detached process it manages itself | Easy initial inspection and no database driver | Custom work for transactional updates, queries, history, recovery, and process management | Rejected for this persistent background workload. (basis: project scope, transactional persistence) |
| Separate CLI and worker with a local control protocol | Manual commands can submit work to an active background process | Additional protocol, lifecycle, packaging, and failure handling | Defer until busy errors become an actual usability problem. (basis: confirmed single writer choice, simplest adequate architecture) |

## Rationale

The collection relates candidates to multiple sources and repeated checks. A transactional relational store fits those facts, while a database service would add operation work for one user. The modernc driver avoids a C compiler requirement; the mattn driver remains a reasonable alternative if CGo becomes acceptable. (basis: [modernc SQLite](https://pkg.go.dev/modernc.org/sqlite), [mattn SQLite](https://pkg.go.dev/github.com/mattn/go-sqlite3))

One writer process keeps background and manual operations from independently changing the same work state. Parallel work remains possible inside that owner once isolated validation is proven. SQLite protects transactions, while the process lock protects application work ownership; they solve different problems. JSON and standard Go packages keep the initial dependency surface small. (basis: single writer ownership, project scope, confirmed configuration choice)

The validator is the feasibility risk. Cloudflare documents applying a subscription key on Linux and then reading the account type, and documents a five device limit. Those facts support an isolated experiment, not a promise that automated temporary attachments can always be cleaned up. A Linux VM is the chosen test environment because it separates the daemon from your normal client. An unofficial consumer API was an alternative, but it was not selected and its semantics were not researched as a substitute. (basis: [Cloudflare Linux instructions](https://developers.cloudflare.com/warp-client/get-started/linux/), [WARP modes and subscriptions](https://developers.cloudflare.com/warp-client/warp-modes/))

The research did not find a documented consumer endpoint that checks entitlement without attachment. This is a bounded research finding, not proof that no such method exists. The reviewed macOS documentation did not establish the Linux CLI licensing flow there. Enterprise device registration APIs concern a different account model and do not prove cleanup for consumer subscriptions. (basis: [macOS instructions](https://developers.cloudflare.com/warp-client/get-started/macos/), [enterprise device registrations](https://developers.cloudflare.com/cloudflare-one/team-and-resources/devices/device-registration/))

Telegram documents public widgets, but that does not establish a stable contract for crawling public channel preview HTML. Keeping retrieval behind an adapter permits explicit parser failures and later changes without conflating source availability with entitlement. (basis: [Telegram widgets](https://core.telegram.org/widgets))

## Evidence and decision record

On 2026-10-10, local help reported `warp-cli 2026.8.2100.0`, described it as a CLI to the service daemon, and described `registration license` as attaching the current registration to another account. Top level and registration help exposed no separate state, profile, or socket argument. Only version and help commands were run; no registration, connection, settings, or key data was inspected or changed.

The local Go command reported `go1.27.2 darwin/arm64`. These local outputs establish the environment observed during design, not cross platform runtime compatibility.

You explicitly welcome Go libraries when they make implementation easier or more readable. The design therefore keeps the selected standard packages without imposing a dependency minimization rule on later features. A helper library can be preferable to custom retry, locking, parsing, or query code. (basis: your explicit library preference)

You accepted verified references, temporary registration subject to reliable cleanup, a separate Linux feasibility test, SQLite through modernc, one executable under native service supervision, JSON configuration, and a busy result for competing writers. You authorized optional development tool discovery, then declined additional skills and MCP servers. No tool was installed or connected.

Discovery found the Cobra and Viper skill in `spf13/go-skills`, general Go guidance in `mythicalhacker/go-mastery`, style guidance in `madflojo/go-style-agent-skill`, and modular guidance in `samber/cc-skills-golang`. No dedicated modernc skill was confirmed. You accepted the recommendation to skip additional skills for this stack. (basis: verified skill repositories listed below, your explicit decline)

Optional MCP candidates were official experimental gopls support, `hloiseau/mcp-gopls`, `helmi75/mcp-sqlite-server`, and `rusq/mcp-sqlite`. They are not runtime dependencies. You accepted the recommendation to skip them: no live database inspection or semantic tooling integration is needed to build help and version output. (basis: verified MCP sources listed below, your explicit decline)

## Independent cross check

On 2026-10-10, a separate model reviewed this decision and its scope without fetching references or changing files. It found no blocking decision gap for the help and version scaffold. It identified two gaps for later implementation: freshness needed an explicit current time source, and the writer busy result needed to distinguish contention from state access failures.

You approved both recommended fixes. The decision now names the application host's UTC wall clock for check timestamps and freshness, and reserves busy errors for lock contention. Clock anomaly handling remains assigned to the persistent state and scheduling specs. You accepted the revised spec on 2026-10-10. Its lifecycle status is now `Accepted`: the linked foundation scaffold is implemented, verified, and tested for macOS and Linux arm64, and scope feature 1 is marked done. Real validation remains unsupported until its separate feasibility gate passes.

## Linux scope amendment, 2026-10-12

You replaced macOS and Linux arm64 support with Linux amd64 and arm64 so macOS GUI service verification no longer blocks delivery. The current implementation already has the Linux systemd lifecycle, but release configuration, Docker admission, container creation, image preparation, worker admission, and service admission still contain arm64 assumptions. Adding a build target alone would leave amd64 unable to validate or run the managed service.

| Option | Benefit | Cost | Recommendation |
| --- | --- | --- | --- |
| Update the existing Linux path and build one immutable validator image per architecture | Reuses tested service behavior, command contracts, recovery, and dependencies | Needs real amd64 provider and service evidence | Selected for this amendment |
| Keep macOS and add amd64 alongside it | Retains broader compatibility | Keeps the macOS verification requirement you explicitly removed and adds a third target | Outside your approved scope |
| Replace service and validator layers with a new abstraction | Could make later operating system additions easier | Discards tested recovery boundaries and introduces unnecessary migration work | Not selected |

Matching the executable, daemon, image, and worker architecture avoids making emulation a supported deployment mode. Existing image IDs remain the configuration boundary, so no registry publication, manifest selection, or new configuration field is needed. Existing journals stay shared and unchanged; this prevents a platform switch from bypassing unresolved cleanup.

Cloudflare's [consumer client prerequisites](https://developers.cloudflare.com/warp-client/get-started/) list AMD64 and ARM64. Its [stable release page](https://developers.cloudflare.com/cloudflare-one/team-and-resources/devices/cloudflare-one-client/download/) lists Linux version `2026.8.2100.0`. These support investigating the existing pinned version on amd64, but do not prove Debian package availability at build time, image compatibility, license attribution, or cleanup parity. Those remain measured implementation requirements.

The transition leaves historical verification records intact and assigns the new proof to spec 0008 AC-17 through AC-19. This keeps the completed scaffold distinct from pending platform work. No implementation, live registration, key attachment, or service mutation occurred during this architecture pass. Git fetch completed, but the repository supplied no remote branch refs, so a remote base comparison was unavailable.

## Independent review of the Linux amendment

On 2026-10-12, a separate model reviewed the draft architecture, validator amendment, service contract, diagnostics, and verification procedures without editing files or fetching references. It found no blocking decision gap or contradictory completion tracking and recommended no corrective change. Architecture value sources, immutable image selection, shared recovery, systemd admission, and AC-17 through AC-19 ownership were explicit. Pinned amd64 package availability, real entitlement and cleanup parity, and native Linux service proof remain implementation requirements. You accepted the technical amendment on 2026-10-12. The scope now points to implementation under feature 9, with no new runtime result claimed.

## References

**Project sources**: [Scope and delivery approach](../../scope/scope.md), confirmed design conversation, local version and help output summarized above. No root `AGENTS.md` was available.

**Practices**: Single writer ownership, transactional persistence, explicit build dependencies, and the simplest adequate architecture.

**Verified documentation**:

1. [Cloudflare Linux WARP instructions](https://developers.cloudflare.com/warp-client/get-started/linux/): documented licensing and account inspection.
2. [Cloudflare WARP modes](https://developers.cloudflare.com/warp-client/warp-modes/): subscription and device limit.
3. [Cloudflare macOS instructions](https://developers.cloudflare.com/warp-client/get-started/macos/): platform documentation boundary.
4. [Cloudflare enterprise device registrations](https://developers.cloudflare.com/cloudflare-one/team-and-resources/devices/device-registration/): separate account model, not consumer cleanup evidence.
5. [modernc SQLite](https://pkg.go.dev/modernc.org/sqlite): selected driver.
6. [mattn SQLite](https://pkg.go.dev/github.com/mattn/go-sqlite3): alternative with a C compiler requirement.
7. [Telegram widgets](https://core.telegram.org/widgets): documented public integration surface, not a crawler contract.

**Verified optional tool candidates**:

1. [Cobra and Viper skill](https://github.com/spf13/go-skills/blob/main/cobra-viper/SKILL.md).
2. [Go mastery](https://github.com/mythicalhacker/go-mastery).
3. [Go style guide](https://github.com/madflojo/go-style-agent-skill).
4. [Modular Go skills](https://github.com/samber/cc-skills-golang).
5. [Official gopls MCP documentation](https://github.com/golang/tools/blob/master/gopls/doc/features/mcp.md).
6. [Third party gopls MCP](https://github.com/hloiseau/mcp-gopls).
7. [Read only SQLite MCP candidate](https://github.com/helmi75/mcp-sqlite-server).
8. [SQLite MCP with write operations](https://github.com/rusq/mcp-sqlite).
