# WPC (Warp Plus Connector)

A personal CLI for publicly shared WARP Plus key candidates. The current scaffold provides help and version output. Real validation remains unsupported until the separate Linux feasibility gate passes.

The first version targets macOS and Linux on arm64. Support for amd64 is deferred. You can build and run it with Go. This scaffold was built with `go1.27.2` and exercised on macOS and Linux arm64. Exact dependency versions are recorded in `go.mod` and `go.sum`.

```sh
go build -o bin/wpc ./cmd/wpc
./bin/wpc
./bin/wpc --help
./bin/wpc version
```

With no subcommand, you see help. A local build reports `wpc dev`. Unknown commands and invalid arguments return a nonzero status and an error on stderr. Help and version output use stdout. These commands do not load configuration, create state, or contact the network.

You can supply release metadata at build time:

```sh
go build -ldflags '-X main.version=0.1.0 -X main.commit=abc1234' -o bin/wpc ./cmd/wpc
./bin/wpc version
```

The values above are examples. You can use your actual release version and commit. If you omit the commit, the output omits it too.

The executable entry point lives in `cmd/wpc`. Command definitions live in `internal/cli`. Cobra `v1.10.2` handles command parsing and help, as selected by the architecture. The scaffold was initialized with Go tooling and Cobra CLI `v1.3.0`, then adapted to this layout. Cobra CLI is only a generator, not an application dependency. See the [official Cobra guide](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md) for the command framework.

The [architecture](docs/specs/0001-stack-architecture/index.md) defines the later configuration, persistence, and validator boundaries. The [scope](docs/scope/scope.md) tracks their implementation. The scaffold uses Cobra at runtime and Go testing with Testify for tests.

## Development checks

The local gate uses Go `1.27.2`, `golangci-lint 2.14.0`, and `pre-commit 4.6.2`, the versions tested for this project. You can make these tools available on your `PATH`, then enable the hook in each checkout:

```sh
pre-commit install
pre-commit run --all-files
```

Each commit runs formatting, `go vet`, race tests, and lint across the module, stopping at the first failure. The formatting check reports files without changing them; you can apply `gofmt -w` to the listed files. The gate also runs when a commit changes only documentation. Local hooks use your installed tools and do not download separate hook environments.

You can run the same checks separately from the repository root:

```sh
sh scripts/check-format.sh
go vet ./...
go test -race ./...
golangci-lint run ./...
```

The [hook configuration](.pre-commit-config.yaml) follows the [pre-commit configuration reference](https://pre-commit.com/#pre-commit-configyaml---hooks). The [lint configuration](.golangci.yml) explicitly enables the five previously validated checks: `errcheck`, `govet`, `ineffassign`, `staticcheck`, and `unused`, using the [golangci-lint v2 configuration format](https://golangci-lint.run/docs/configuration/file/).

CI remains pending until you select a provider. Its planned checks are the same local gate plus builds for macOS and Linux arm64.

You can repeat the gate and its failure cases with the [tooling verification checklist](docs/reviews/tooling/verify.md).
