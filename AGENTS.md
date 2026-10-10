# Warp Plus Connector

@/Users/nguyenducloc/.codex/RTK.md
Prefix shell commands with `rtk`; use `rtk proxy` when raw output is needed.

## Stack
Go, one module, Cobra CLI, macOS and Linux arm64 for the first version; amd64 is deferred. Tested toolchain: `go1.27.2`. Go modules manage dependencies.
Current dependencies: Cobra `v1.10.2`, Go testing and Testify `v1.11.1`, and SQLite via `database/sql` and `modernc.org/sqlite v1.60.1`. The store uses explicit SQL and `encoding/json` for evidence; the driver supplies SQLite without a C toolchain. The CLI still exposes help and version only.
Accepted later stack: JSON configuration via `encoding/json`, HTTP source adapters, `log/slog`, persisted scheduling, and user `launchd` or `systemd` supervision. These application features are not implemented yet.

## Build approach
Tracer Bullet (prove one real crawl, check, save, and list path, then expand it).
Workflow: Beta, verify behavior after development, then add meaningful tests. Scope and the accepted architecture define each slice.

## Commands
```sh
rtk proxy go mod download
rtk proxy go build -o bin/wpc ./cmd/wpc
rtk proxy go run ./cmd/wpc --help
rtk proxy go test ./...
rtk proxy gofmt -l cmd internal scripts
rtk proxy sh scripts/check-format.sh
rtk proxy pre-commit install
rtk proxy pre-commit run --all-files
rtk proxy go vet ./...
rtk proxy go test -race ./...
rtk proxy golangci-lint run ./...
```
Formatting passes when `gofmt -l` prints nothing. To format changed Go files, use `rtk proxy gofmt -w <files>`. Tests live beside source as `*_test.go`; preferences are in `test-preferences.json`.

## Rules
1. Keep `cmd/wpc` responsible for process exit and output streams, and `internal/cli` for parsing and rendering. Add other packages only when a real path needs them.
2. Prefer concrete functions internally and small interfaces at external boundaries. Domain records stay independent of Cobra, HTTP, and database driver types.
3. Document exported APIs, use idiomatic Go naming and concrete types, wrap errors with context using `%w`, and inspect errors with `errors.Is` or `errors.As`. Propagate cancellation through application work and external calls.
4. Keep results on stdout and diagnostics on stderr. Help and version must not load configuration, create state, open a database, or contact the network. Test behavior through public interfaces; use subprocess tests for exit codes and linker metadata.
5. Use a maintained library when it removes meaningful complexity; record its reason and tested version. Avoid speculative packages or new configuration layers. Later settings follow defaults, JSON, then explicitly supplied flags.
6. Real validation remains unsupported until its separate Linux feasibility gate proves entitlement, isolated registration, and cleanup. Never use the host WARP registration as a fallback. Store writes require one lock owner; keep SQL behind the store boundary and transactions outside network requests.
7. `internal/state` holds domain records; `internal/store` owns SQLite persistence, migrations, recovery, and bounded history reads. Store callers supply an absolute state directory, recovery clock, and explicit freshness evaluation. The CLI does not open the store yet.

## Tooling
The local gate in `.pre-commit-config.yaml` runs formatting, `go vet`, race tests, and `golangci-lint` in order and stops at the first failure. All four hooks use installed tools and run even for changes containing only documentation. CI remains pending provider selection; its planned checks include the same gate and builds for macOS and Linux arm64.
The formatting gate uses `scripts/check-format.sh` to reject unformatted Go files under `cmd` and `internal` without changing them. Go tests under `scripts` also need the broader formatting command above. `.golangci.yml` enables `errcheck`, `govet`, `ineffassign`, `staticcheck`, and `unused`. Repeatable hook checks are in [tooling verification](docs/reviews/tooling/verify.md).
Observed tools: `golangci-lint 2.14.0`, `pre-commit 4.6.2`. Formatting, vet, race tests, lint, and configuration validation passed on macOS arm64. Sandbox runs of vet, race tests, and golangci encountered cache access or package loading errors; all passed with approved toolchain and cache access. Check the environment before changing dependencies to address those errors.

## Git
- integration: on
- branch prefix: feat/
- commit: per-milestone
Offer milestone commits for approval. Confirm pushes and PRs. Public repository and `origin`: `https://github.com/nguyen-duc-loc/warp-plus-connector`. Go module: `github.com/nguyen-duc-loc/warp-plus-connector`.

## Specs
Architecture: [0001 stack and architecture](docs/specs/0001-stack-architecture/index.md). Feature records live under `docs/specs/NNNN-name/`; scope lives in [docs/scope/scope.md](docs/scope/scope.md).

## Agent skills
- [architect](.agents/skills/architect/): `JavaScript-Mastery-Pro/skills`, decisions and specs.
- [audit](.agents/skills/audit/): `JavaScript-Mastery-Pro/skills`, initial project context.
- [check](.agents/skills/check/): `JavaScript-Mastery-Pro/skills`, runtime verification and code review.
- [debug](.agents/skills/debug/): `JavaScript-Mastery-Pro/skills`, bug diagnosis and fixes.
- [develop](.agents/skills/develop/): `JavaScript-Mastery-Pro/skills`, implementation from accepted decisions.
- [document](.agents/skills/document/): `JavaScript-Mastery-Pro/skills`, change descriptions.
- [scope](.agents/skills/scope/): `JavaScript-Mastery-Pro/skills`, feature scope and status.
- [sync](.agents/skills/sync/): `JavaScript-Mastery-Pro/skills`, durable context upkeep.
- [test](.agents/skills/test/): `JavaScript-Mastery-Pro/skills`, tests and test preferences.
Declined: additional community skills and MCP servers for this stack. Do not offer discovery again without a new request.

## Context files
- [internal/store/AGENTS.md](internal/store/AGENTS.md): Store ownership, persistence contracts, cancellation, and verification.

_Drafted by /audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
