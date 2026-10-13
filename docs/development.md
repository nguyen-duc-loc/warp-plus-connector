# Local development

[Back to the README](../README.md)

Release users can download a binary and a validator image. This guide is for changing WPC or building from source.

## Prerequisites

| Tool | Version used by this repository | Needed for |
| --- | --- | --- |
| Go | `1.27.2`, as recorded in `go.mod` | Builds and tests |
| Git | A current supported release | Source checkout and release metadata |
| C compiler | Platform compiler | Go race tests; release binaries use `CGO_ENABLED=0` |
| editorconfig-checker | `4.0.2` | Formatting checks |
| golangci-lint | `2.14.0` | Static checks |
| pre-commit | `4.6.2` | Optional local hook runner |
| GoReleaser | `2.18.3` | Linux archives and release validation |
| actionlint | `1.7.7` | GitHub Actions workflow validation |
| Docker Engine, CLI, Buildx | CI installs Buildx `0.38.0` | Validator image builds and offline smoke checks |

Linux amd64 and arm64 are release targets. macOS can be a development host for builds, collection, saved reads, and tests. Validation and native service commands require Linux. A macOS test pass does not establish Linux runtime behavior.

Install the Go based check tools with exact versions:

```sh
go install github.com/editorconfig-checker/editorconfig-checker/v4/cmd/editorconfig-checker@v4.0.2
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
export PATH="$(go env GOPATH)/bin:$PATH"
```

Install GoReleaser using its [official installation instructions](https://goreleaser.com/install/), selecting `2.18.3`. `pipx install pre-commit==4.6.2` installs the optional hook runner when pipx is available. ShellCheck can be installed through your system package manager; actionlint uses it when present. GitHub's Ubuntu runners include it.

## Build and run

```sh
git clone https://github.com/nguyen-duc-loc/warp-plus-connector.git
cd warp-plus-connector
go mod download
go build -o bin/wpc ./cmd/wpc
./bin/wpc version
./bin/wpc --help
```

A local build reports `wpc dev`. The normal user commands in the README work with `./bin/wpc` in place of `wpc`. Collection needs access to public Telegram previews:

```sh
./bin/wpc sources
./bin/wpc crawl
./bin/wpc list
```

Use a separate absolute `--state-dir` for experiments instead of your normal profile. Tests create their own temporary state and use synthetic keys and controlled external boundaries.

The agent workspace uses RTK as a command wrapper, for example `rtk proxy go test ./...`. RTK is not a build or runtime dependency; ordinary terminals and CI use the underlying commands directly.

## Formatting and checks

```sh
pre-commit install
pre-commit run --all-files
```

The gate runs these checks in order and stops at the first failure, including for documentation changes:

```sh
sh scripts/check-format.sh
go vet ./...
go test -race ./...
golangci-lint run ./...
```

For workflow or release changes, also run:

```sh
actionlint
goreleaser check
goreleaser release --snapshot --clean
```

`scripts/check-format.sh` checks gofmt and EditorConfig rules in Go files under `cmd`, `internal`, and `scripts`. It does not edit files. The 120 character limit still applies after gofmt; split long string literals without changing their values.

To apply the repository's GoLand formatting rules:

```sh
sh scripts/format.sh
```

The formatter uses GoLand `2026.2.0.1`, detected through macOS app locations or `goland.sh` on `PATH`. `GOLAND_BIN` selects another installation. Close other GoLand instances before using its command line formatter, or format in the open IDE. CI only checks formatting and does not require GoLand.

Tests live beside source as `*_test.go`; [test preferences](../test-preferences.json) describe the project's conventions. [Tooling verification](reviews/tooling/verify.md) records repeatable gate checks.

## Build the validator image

Use a local Linux Docker daemon with the same amd64 or arm64 architecture as the target. The build helper detects the daemon architecture and compiles the worker for it. A macOS host may prepare an image for its local Linux VM, but the WPC validator itself still runs on Linux.

```sh
docker version
docker buildx version
docker info
validator_image_id=$(sh scripts/build-validator-image.sh)
sh scripts/smoke-validator-image.sh "$validator_image_id"
```

The builder downloads the signed, pinned WARP package, compiles `wpc-worker`, loads the image into the local daemon, and resolves the loaded tag to its immutable local ID. It then checks OS, architecture, protocol labels, installed package version, and the exact worker bytes. Stdout contains only the final image ID; build diagnostics go to stderr.

`--iidfile` output is not used as a portable Docker lookup key. Earlier verification found an unusable digest with Ubuntu Buildx `0.30.1`; Buildx `0.38.0` worked in that environment. Docker major version alone does not identify every builder and storage compatibility issue. The helper now resolves the loaded image through its tag before using its immutable ID.

The smoke script disables networking and uses synthetic input. It checks worker readiness, initialization, cancellation, and recovery from a locally prepared session. It removes its temporary container. It does **not** register a Cloudflare device, attach a key, prove Unlimited entitlement, or verify remote deletion.

For a deliberate live check on Linux, select candidate IDs from your own collection and use the resulting image ID:

```sh
./bin/wpc check 1 --validator docker --validator-image "$validator_image_id"
```

Current pin and evidence limits are recorded in [validator notes](../internal/validator/README.md). If the upstream APT repository stops serving the pin, downloading a previously published release image avoids rebuilding it. A vendor upgrade needs coordinated image and runtime version changes and renewed provider verification, not just a Dockerfile edit.

## Build release archives locally

```sh
goreleaser check
goreleaser release --snapshot --clean
```

This creates Linux amd64 and arm64 archives plus `checksums.txt` under `dist/`, without publishing or needing a GitHub token. `--clean` replaces generated `dist/` output. The snapshot version includes the source commit. Snapshots are development artifacts, not tagged releases.

The release binary embeds `main.version` and `main.commit`, is built with `-trimpath`, and has CGO disabled. Only the CLI is in the public archive; the worker belongs inside its validator image. See [CI and releases](releasing.md) for version tags and publication.

## Repository map

```text
cmd/wpc/                 CLI process entry point
cmd/wpc-worker/          Container worker entry point
internal/cli/            Cobra commands and rendering
internal/app/            Crawl, checking, schedules, shutdown
internal/config/         Private JSON and explicit flag overrides
internal/state/          Domain records
internal/store/          SQLite and recovery
internal/source/         Public Telegram adapter
internal/validator/      Docker ownership, worker, image, cleanup
internal/service/        systemd user controls and diagnostics
scripts/                 Formatting, image preparation, smoke checks
.github/workflows/       CI and tagged release automation
.goreleaser.yaml         Binary, archive, checksum, release metadata
docs/                    User guides, scope, specs, verification history
```

Respect the root and package `AGENTS.md` contracts when changing behavior. Keep SQL behind the store boundary, propagate cancellation, keep keys out of diagnostics, and preserve the distinction between saved observations and current native or provider state.
