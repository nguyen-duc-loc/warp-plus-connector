# Warp Plus Connector

@/Users/nguyenducloc/.codex/RTK.md
Prefix shell commands with `rtk`; use `rtk proxy` when raw output is needed.

## Stack
Go, one module, Cobra CLI, Linux amd64 and arm64 for the first version. macOS is outside the release scope; it may be used as a development host. Tested toolchain: `go1.27.2`. Go modules manage dependencies.
Current dependencies: Cobra `v1.10.2`, Go testing and Testify `v1.11.1`, SQLite via `database/sql` and `modernc.org/sqlite v1.60.1`, and goquery `v1.13.0` with `golang.org/x/net v0.58.0`. The store uses explicit SQL and `encoding/json` for evidence; the driver supplies SQLite without a C toolchain. The CLI exposes crawl, saved reports, complete listing, selected checks, and validator recovery.
Implemented runtime pieces include private JSON configuration, the public Telegram HTTP adapter, `log/slog`, an optional local Docker validator using official WARP `2026.8.2100.0`, and persisted scheduling through foreground `run` or the Linux systemd user service. `service` provides installation, repeatable controls, saved status, bounded logs, and local doctor checks without adding a scheduler or database migration.

## Build approach
Tracer Bullet (prove one real crawl, check, save, and list path, then expand it).
Workflow: Beta, verify behavior after development, then add meaningful tests. Scope and the accepted architecture define each slice.

## Commands
```sh
rtk proxy go mod download
rtk proxy go build -o bin/wpc ./cmd/wpc
rtk proxy goreleaser check
rtk proxy goreleaser build --snapshot --clean
rtk proxy sh scripts/build-validator-image.sh
rtk proxy go run ./cmd/wpc --help
rtk proxy go test ./...
rtk proxy gofmt -l cmd internal scripts
rtk proxy sh scripts/format.sh
rtk proxy sh scripts/check-format.sh
rtk proxy pre-commit install
rtk proxy pre-commit run --all-files
rtk proxy go vet ./...
rtk proxy go test -race ./...
rtk proxy golangci-lint run ./...
```
Formatting passes when `scripts/check-format.sh` reports no gofmt or EditorConfig violations. Use `rtk proxy sh scripts/format.sh` to apply GoLand's `.editorconfig` rules, run gofmt, and check the result. `gofmt -w` alone does not enforce the 120-character limit; split remaining long strings without changing their values. Tests live beside source as `*_test.go`; preferences are in `test-preferences.json`.

## Rules
1. Keep `cmd/wpc` responsible for process exit and output streams, and `internal/cli` for parsing and rendering. Add other packages only when a real path needs them.
2. Prefer concrete functions internally and small interfaces at external boundaries. Domain records stay independent of Cobra, HTTP, and database driver types.
3. Document exported APIs, use idiomatic Go naming and concrete types, wrap errors with context using `%w`, and inspect errors with `errors.Is` or `errors.As`. Propagate cancellation through application work and external calls.
4. Keep results on stdout and diagnostics on stderr. Help and version must not load configuration, create state, open a database, or contact the network. Test behavior through public interfaces; use subprocess tests for exit codes and linker metadata.
5. Use a maintained library when it removes meaningful complexity; record its reason and tested version. Avoid speculative packages or new configuration layers. Later settings follow defaults, JSON, then explicitly supplied flags.
6. Docker validation requires a Linux executable, local Linux daemon, immutable image, and worker with matching amd64 or arm64 architecture, plus a local Unix Docker endpoint and shared private recovery ownership. Never use the host WARP registration as a fallback. Unknown remote cleanup blocks further attachments. Store writes require one lock owner; keep SQL behind the store boundary and transactions outside network requests.
7. `internal/state` holds domain records; `internal/store` owns SQLite persistence, migrations, recovery, and bounded history reads. Store callers supply an absolute state directory, recovery clock, and explicit freshness evaluation. Runtime CLI commands use the store; help and version remain free of state and configuration access.
8. `internal/config` resolves private JSON before state or external work; only explicitly changed flags override it. `internal/app` owns coordination: persist crawl intent before fetching, save parsed coverage before messages, and preserve earlier message commits on failure. Listing and export share one database snapshot and freshness evaluation. CLI diagnostics use stable codes; keys appear only with `list --show-keys`, `export --show-keys`, or `export --format keys`.
9. Source configuration accepts up to 32 normalized channels and page limits from 1 through 100. Defaults are `warpplus`, `warppluscn`, `warpplushome`, and `warp_veyke`, one page each. Explicit source lists replace defaults. Validate disabled entries too; `crawl --source` only selects configured enabled entries, preserving configuration order, and explicit `--page-limit` overrides selected limits. `crawl` and `run` with crawling enabled reject an empty source selection. `sources` reads configuration without opening state, contacting sources, or invoking Docker.
10. `check` resolves and deduplicates the complete selection before saving a batch. `internal/app` owns worker limits, mutation pacing, retries, store writes, and result callbacks under one shared validator owner. Defaults are two workers and three attempts; retries require an explicit permitted classification, proven cleanup, and container removal. `check show` reads the saved policy and complete attempt history without Docker. The contract is [spec 0005](docs/specs/0005-concurrent-checks-bounded-retries/index.md).
11. `list` and `export` validate shared filters and positive optional limits before configuration or staging. Results are unlimited by default; truncation requires another matching row in the same snapshot. `internal/cli` prepares output in private temporary files and closes the reader before delivery. Named exports use atomic creation without replacement outside the canonical state directory. Failures after publication retain the complete file and report `published=true`; failures after stdout delivery report `delivered=true`. JSON uses schema version 1; key lines preserve exact values and reject unsafe line characters. The contract is [spec 0006](docs/specs/0006-complete-results-export/index.md).
12. `run` owns one writer for independent crawl and validation lanes. Defaults are hourly crawling, six hour rechecks, and hourly failure recovery; runner intervals must be from `1m` through `720h`. Enabled validation requires Docker, while `--validation-enabled=false` permits collection alone. `schedule show [candidate-id]` reads saved facts as text or schema version 1 JSON without Docker, migration, or keys. Schema version 5 and shared validator registry version 3 implement [spec 0007](docs/specs/0007-scheduled-crawl-revalidation/index.md); worker protocol remains 2.

13. `internal/service` owns account scoped native controls and diagnostics under [spec 0008](docs/specs/0008-background-operation-diagnostics/index.md). Public service admission supports Linux amd64 and arm64; native state and saved observations remain separate evidence. Status and doctor perform bounded reads without migration, recovery, registration, or attachment. Detailed ownership, shutdown, privacy, and log contracts are in [internal/service/AGENTS.md](internal/service/AGENTS.md).
14. Current logging follows [spec 0009](docs/specs/0009-readable-development-logs/index.md), which replaces the earlier `log/slog` stack choice with zerolog `v1.35.1` and uses go-isatty `v0.0.24` for actual destination detection. `internal/diagnostic` owns safe event presentation and failure capture. `--log-level`, `--log-format`, and `--color` are invocation flags only; results remain on stdout and diagnostics on stderr. `service logs --format` controls its stdout independently, with filtering after archive selection and complete JSON output. Final rendering preserves typed delivery facts and never retries a failed diagnostic stream, including a failed result metadata write.
15. Bare `wpc` opens the read only Ink overview under [spec 0010](docs/specs/0010-terminal-foundation-overview/index.md) only on Linux amd64 or arm64 with actual stdin and stdout terminals and a nonempty `TERM` other than `dumb`. Other root invocations return plain help before configuration, state, service, or UI asset access. `internal/terminal` owns the bundled child and private protocol; `ui` owns rendering. Root profile flags resolve once. Explicit commands keep their existing contracts, and overview reads never start background work or disclose keys.

## Tooling
The local gate in `.pre-commit-config.yaml` runs formatting, `go vet`, race tests, and `golangci-lint` in order and stops at the first failure. All four hooks use installed tools and run even for changes containing only documentation. GitHub Actions now defines the same gate on native Linux amd64 and arm64, offline validator smoke checks, workflow validation, and GoReleaser snapshot archives. Hosted execution is verified only by an actual successful workflow run.
The formatting gate uses `scripts/check-format.sh` to reject gofmt and EditorConfig violations in Go files under `cmd`, `internal`, and `scripts` without changing them. It requires `editorconfig-checker 4.0.2`; only its indentation check is disabled because gofmt owns Go indentation and raw string whitespace is data. `scripts/format.sh` uses GoLand `2026.2.0.1` (macOS app detection or `goland.sh` on PATH; override with `GOLAND_BIN`) before gofmt and the gate. `.golangci.yml` enables `errcheck`, `govet`, `ineffassign`, `staticcheck`, and `unused`. Repeatable hook checks are in [tooling verification](docs/reviews/tooling/verify.md).
Observed tools: `golangci-lint 2.14.0`, `pre-commit 4.6.2`. Formatting, vet, race tests, lint, and configuration validation passed on macOS arm64. Sandbox runs of vet, race tests, and golangci encountered cache access or package loading errors; all passed with approved toolchain and cache access. Check the environment before changing dependencies to address those errors.
GoReleaser `2.18.3` uses `.goreleaser.yaml` for Linux amd64 and arm64 builds with CGO disabled and version metadata. Snapshot builds replace generated `dist/` output. `rtk proxy goreleaser release --snapshot --clean` also creates local archives and SHA256 checksums without publishing; the Docker worker image keeps its separate build script.
GoReleaser's preparation hook now requires both `.ui-build/amd64/lib/wpc-ui` and `.ui-build/arm64/lib/wpc-ui` trees, prepared from the exact pins in `ui/runtime.json` and `ui/package-lock.json`. [Terminal packaging](docs/terminal.md#local-packaging) describes preparation and native archive checks. UI checks use `rtk proxy npm --prefix ui run build` followed by `rtk proxy npm --prefix ui test`; the Go commit hooks do not run them.

## Release automation
The user selected GitHub Actions, GitHub Releases, and public GHCR validator images on 2026-10-13, extending earlier scope and architecture records that deferred publishing. `.github/workflows/ci.yml` is reused by `release.yml`; tagged releases require both native gates, snapshot packaging, image verification, and anonymous GHCR access before GoReleaser publishes. Version tags are `vMAJOR.MINOR.PATCH` or `-alpha.N`, `-beta.N`, `-rc.N` prereleases from main history. First publication requires setting the GHCR package public. The operational contract is [docs/releasing.md](docs/releasing.md); workflow files do not themselves prove hosted execution or live provider behavior.

## Git
- integration: on
- branch prefix: feat/
- commit: per-milestone
Offer milestone commits for approval. Confirm pushes and PRs. Public repository and `origin`: `https://github.com/nguyen-duc-loc/warp-plus-connector`. Go module: `github.com/nguyen-duc-loc/warp-plus-connector`.

## Specs
Architecture: [0001 stack and architecture](docs/specs/0001-stack-architecture/index.md). Feature records live under `docs/specs/NNNN-name/`; scope lives in [docs/scope/scope.md](docs/scope/scope.md).

## Validator package update
Current build and runtime pin: WARP `2026.8.2110.0`. See [validator verification notes](internal/validator/README.md#package-availability-update-2026-10-13) for the package availability fix and verification limits. Older stack and spec references to `2026.8.2100.0` describe the earlier release; live entitlement and remote deletion verification have not yet been repeated for the new pin.

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
- [internal/terminal/AGENTS.md](internal/terminal/AGENTS.md): Bundle admission, private protocol, terminal ownership, and bounded shutdown.
- [ui/AGENTS.md](ui/AGENTS.md): Pinned Ink runtime, screen design, validated replies, refresh behavior, and UI checks.
- [internal/diagnostic/AGENTS.md](internal/diagnostic/AGENTS.md): Safe event fields, destination presentation, serialized writes, and diagnostic failure capture.
- [.github/AGENTS.md](.github/AGENTS.md): CI permissions, native checks, and versioned publication.
- [internal/service/AGENTS.md](internal/service/AGENTS.md): Native service ownership, diagnostic reads, structured logs, and bounded shutdown.
- [internal/app/AGENTS.md](internal/app/AGENTS.md): Crawl coordination, mutation failure boundaries, checks, and saved reads.
- [internal/source/AGENTS.md](internal/source/AGENTS.md): Bounded public retrieval, exact candidate extraction, and source provenance.
- [internal/store/AGENTS.md](internal/store/AGENTS.md): Store ownership, persistence contracts, cancellation, and verification.
- [internal/validator/AGENTS.md](internal/validator/AGENTS.md): Isolated Docker validation, durable remote recovery, and image preparation.

_Drafted by /audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
