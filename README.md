# WPC (Warp Plus Connector)

A personal CLI for publicly shared WARP Plus key candidates. You can crawl bounded public Telegram previews, inspect saved coverage, list all saved candidates, and check a selected authorized key in an isolated Linux container.

The first version targets macOS and Linux on arm64. Support for amd64 is deferred. You can build and run it with Go. The tested toolchain is `go1.27.2`. Exact dependency versions are recorded in `go.mod` and `go.sum`.

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

For both supported platforms, you can use GoReleaser (tested with `2.18.3`):

```sh
rtk proxy goreleaser check
rtk proxy goreleaser build --snapshot --clean
```

[The build configuration](.goreleaser.yaml) produces macOS and Linux arm64 binaries in `dist/`, with version and commit metadata. It disables CGO so the builds need no C compiler. To also create local archives and SHA256 checksums, you can use `rtk proxy goreleaser release --snapshot --clean`. Snapshot mode does not publish. These commands replace the generated `dist/` directory. The Docker validator image keeps its separate build script.

The configuration follows the official [Go build options](https://goreleaser.com/customization/builds/builders/go/) and [snapshot workflow](https://goreleaser.com/getting-started/quick-start/).

The executable entry point lives in `cmd/wpc`. Command definitions live in `internal/cli`. Cobra `v1.10.2` handles command parsing and help, as selected by the architecture. The scaffold was initialized with Go tooling and Cobra CLI `v1.3.0`, then adapted to this layout. Cobra CLI is only a generator, not an application dependency. See the [official Cobra guide](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md) for the command framework.

The [architecture](docs/specs/0001-stack-architecture/index.md) defines the configuration, persistence, and validator boundaries. The [scope](docs/scope/scope.md) tracks their implementation. The public preview parser uses goquery `v1.13.0` for scoped HTML traversal, with cascadia `v1.3.4` and `golang.org/x/net v0.58.0`. These versions were exercised with live markup and synthetic fixtures on macOS and Linux arm64.

## Crawl and inspect

```sh
./bin/wpc sources
./bin/wpc crawl
./bin/wpc crawl --source warpplus --page-limit 2
./bin/wpc crawl show 1
./bin/wpc list
./bin/wpc check 1
./bin/wpc list --show-keys
```

You can replace the example IDs with a saved run ID or candidate ID. Crawling defaults to one page each from `warpplus`, `warppluscn`, `warpplushome`, and `warp_veyke`. Requests run sequentially with at least one second between starts, a 20 second deadline, and a 2 MiB decoded body limit. Higher page limits follow validated older navigation without an extra probe or retry. Repeated crawls preserve exact candidate identity and source message provenance. Reports distinguish a page budget stop, the end of the visible preview, and failures. They always report `history_complete=false`.

`sources` shows configured channels, including disabled entries, without opening state. Only `crawl` accepts repeatable `--source` selection and `--page-limit`. Selection preserves configuration order and cannot enable a disabled channel. One source failure allows later channels to run; HTTP 429 stops further requests.

`crawl show` and `list` use saved state without contacting Telegram or a validator. Listing follows every bounded store page, with no total limit. Keys stay hidden unless you supply `--show-keys`. Missing state is an error; an initialized empty collection reports `No candidates saved.`

The default validator is `unsupported`. It saves an inconclusive attempt with reason `validation_unsupported` and exits 1 without registration work. The optional Docker backend uses the official WARP client, confirms only matching Unlimited evidence, and deletes its temporary registration before returning a confirmation. Source claims and matching key formats do not establish entitlement.

## Isolated validation

You need a local Docker engine running Linux arm64 containers. You can build the tested image once, then select its immutable ID:

```sh
validator_image_id=$(rtk proxy sh scripts/build-validator-image.sh)
rtk proxy ./bin/wpc check 1 --validator docker --validator-image "$validator_image_id"
rtk proxy ./bin/wpc list
```

Replace `1` with a saved candidate ID you are authorized to activate. The build downloads the signed official WARP package; a check uses only the prepared local image. The backend does not change your host registration or connect a host tunnel. Remote Docker endpoints, privileged containers, host networking, and host mounts are unsupported.

A confirmed result includes `account_type=Unlimited`, its observation time, and the tested backend version. Matching Free accounts remain inconclusive with `not_unlimited`. Full accounts report `device_limit`. Throttling reports `rate_limited` when the tested client provides sufficient evidence. Results are observations, not reserved device slots.

The backend keeps one shared recovery record across profiles. A new check first reconciles interrupted work. You can also request cleanup alone:

```sh
rtk proxy ./bin/wpc validator recover --validator docker --validator-image "$validator_image_id"
```

Unknown remote outcomes retain the container and block further attachments with `validation_cleanup_pending`. Deleting that container or its journal does not prove remote cleanup. The [validator notes](internal/validator/README.md) describe the retained state and recovery boundary.

## Private configuration

Runtime commands accept `--config`, `--state-dir`, and `--recheck-interval`. `check` and `validator recover` also accept `--validator`, `--validator-image`, and `--validator-socket`. Explicit flags override JSON settings. Validator choices are `unsupported` and `docker`.

The optional default file is `os.UserConfigDir()/warp-plus-connector/config.json`. An explicit missing file fails. Existing configuration must be a regular file owned by your account, without group or other access or a symlink. For example, you can save this JSON with mode `0600`:

```json
{
  "state_dir": "/absolute/private/path/wpc",
  "recheck_interval": "6h",
  "validator": "unsupported",
  "crawl_page_limit": 1,
  "sources": [
    {"channel": "warpplus", "page_limit": 2},
    {"channel": "warppluscn", "enabled": false}
  ]
}
```

Omitting `sources` uses the four defaults. An explicit list replaces them, including an empty list. Channels use bare ASCII letters, digits, or underscores and normalize to lowercase. Duplicate channels are invalid. Up to 32 entries and page limits from 1 through 100 are accepted. Per channel limits override `crawl_page_limit`; an explicit `--page-limit` overrides selected limits. An empty enabled selection fails only for `crawl`, before opening state.

Relative state paths resolve from your invocation directory. Shell expansions such as `~` are not performed. State defaults to your `Library/Application Support/warp-plus-connector` on macOS. Linux uses an absolute `XDG_STATE_HOME`, or your `.local/state`, followed by `warp-plus-connector`. State is private to your account. Opening a writer applies schema migrations and recovers interrupted local work; reading does neither.

To select Docker in JSON, set `validator` to `docker` and add `validator_image` with the complete `sha256:` ID printed by the build script. Optional `validator_socket` is an absolute local Unix socket path. Otherwise the backend inspects your selected local Docker context. The recovery directory is `validator` beneath the platform default state directory, independent of a profile's `state_dir` override.

The interval controls freshness evaluation only. Scheduling is not implemented. Configuration rejects unknown or duplicate fields, nulls, invalid values, extra JSON documents, and files larger than 64 KiB. Root invocation, help, and version bypass configuration and state.

Results use stdout; fixed diagnostic reasons and structured run logs use stderr. Ordinary failures and unsupported checks exit 1. SIGINT and SIGTERM request cancellation and exit 130 and 143, with a bounded attempt to persist terminal state. A later writer recovers any unresolved local work.

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
