# WPC (Warp Plus Connector)

A personal CLI for publicly shared WARP Plus key candidates. You can crawl bounded public Telegram previews, inspect saved coverage, list all saved candidates, and check selected authorized keys in an isolated Linux container.

The release targets are Linux amd64 and arm64. Native amd64 runtime and provider verification remain pending. You can build and run it with Go. The tested toolchain is `go1.27.2`. Exact dependency versions are recorded in `go.mod` and `go.sum`.

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

For both Linux build targets, you can use GoReleaser (tested with `2.18.3`):

```sh
rtk proxy goreleaser check
rtk proxy goreleaser build --snapshot --clean
```

[The build configuration](.goreleaser.yaml) produces Linux amd64 and arm64 binaries in `dist/`, with version and commit metadata. It disables CGO so the builds need no C compiler. To also create local archives and SHA256 checksums, you can use `rtk proxy goreleaser release --snapshot --clean`. Snapshot mode does not publish. These commands replace the generated `dist/` directory. The Docker validator image keeps its separate build script.

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
./bin/wpc check 1 2 3
./bin/wpc check show 2
./bin/wpc list --show-keys
```

You can replace the example IDs with a saved run ID or candidate ID. Crawling defaults to one page each from `warpplus`, `warppluscn`, `warpplushome`, and `warp_veyke`. Requests run sequentially with at least one second between starts, a 20 second deadline, and a 2 MiB decoded body limit. Higher page limits follow validated older navigation without an extra probe or retry. Repeated crawls preserve exact candidate identity and source message provenance. Reports distinguish a page budget stop, the end of the visible preview, and failures. They always report `history_complete=false`.

`sources` shows configured channels, including disabled entries, without opening state. Only `crawl` accepts repeatable `--source` selection and `--page-limit`. Selection preserves configuration order and cannot enable a disabled channel. One source failure allows later channels to run; HTTP 429 stops further requests.

`crawl show`, `check show`, and `list` use saved state without contacting Telegram or a validator. Listing follows every bounded store page, with no total limit. Keys stay hidden unless you supply `--show-keys`. Missing state is an error; an initialized empty collection reports `No candidates saved.`

The default validator is `unsupported`. It saves an inconclusive attempt with reason `validation_unsupported` and exits 1 without registration work. The optional Docker backend uses the official WARP client, confirms only matching Unlimited evidence, and deletes its temporary registration before returning a confirmation. Source claims and matching key formats do not establish entitlement.

## Isolated validation

Validation requires a Linux executable and local Docker daemon with the same amd64 or arm64 architecture. You can prepare an image from a development host, then select its immutable ID on the matching Linux runtime:

```sh
validator_image_id=$(rtk proxy sh scripts/build-validator-image.sh)
rtk proxy ./bin/wpc check 1 --validator docker --validator-image "$validator_image_id"
rtk proxy ./bin/wpc list
```

The preparation script detects the local Linux daemon architecture. You can also pass `amd64` or `arm64`; a mismatch is refused. It checks the compiled worker, image platform, labels, signed package version, and installed worker bytes before printing the image ID. Inspection uses a temporary container with networking disabled and does not start WARP.

Replace `1` with a saved candidate ID you are authorized to activate. The build downloads the signed official WARP package; a check uses only the prepared local image. The backend does not change your host registration or connect a host tunnel. Remote Docker endpoints, privileged containers, host networking, and host mounts are unsupported.

A confirmed result includes `account_type=Unlimited`, its observation time, and the tested backend version. Matching Free accounts remain inconclusive with `not_unlimited`. Full accounts report `device_limit`. Throttling reports `validation_rate_limited` when the tested client provides sufficient evidence. Results are observations, not reserved device slots.

The backend keeps one shared recovery registry across profiles, with up to four isolated sessions. A new check first reconciles interrupted work. You can also request cleanup alone:

```sh
rtk proxy ./bin/wpc validator recover --validator docker --validator-image "$validator_image_id"
```

Unknown remote outcomes retain the container and block further attachments with `validation_cleanup_pending`. Deleting that container or its journal does not prove remote cleanup. The [validator notes](internal/validator/README.md) describe the retained state and recovery boundary.

`check` removes duplicate IDs in your supplied order and resolves the whole selection before saving a batch. Each final item prints when its result is committed, followed by saved totals. `check show <run-id>` reads the policy, selection, every attempt, retry waits, and totals after restart. Reading a report exits 0 even when that historical batch failed.

Checks default to two workers, three attempts per candidate, one second between completed registration or license commands, and a 15 minute candidate budget. You can set `--workers` from 1 through 4, `--max-attempts` from 1 through 5, `--mutation-interval` in whole seconds from 1s through 60s, and `--candidate-timeout` from 1m through 60m. The budget begins at the first admitted attempt and includes later waits.

Only classified temporary failures retry after proven cleanup. Retry waits grow with jitter and release capacity. Free accounts, device capacity, unsupported validation, and unclassified errors do not retry. The pinned Docker adapter cannot supply a trusted server delay, so throttling stops the batch. Inconclusive attempts preserve earlier definitive evidence and cause exit 1.

The new image uses worker protocol 2. You can rebuild it with the existing build script before selecting its new immutable ID. Recovery reconciles a retained protocol 1 session with its original image before converting the journal. Older binaries cannot use the new registry. Cancellation allows up to 60 seconds for remote collection, then five seconds for local finalization submissions. Entered store work retains ownership until it finishes, so these are not hard process exit deadlines.

## Private configuration

Runtime commands accept `--config`, `--state-dir`, and `--recheck-interval`. `check`, `run`, and `validator recover` also accept `--validator`, `--validator-image`, and `--validator-socket`. Explicit flags override JSON settings. Validator choices are `unsupported` and `docker`.

The optional default file is `os.UserConfigDir()/warp-plus-connector/config.json`. An explicit missing file fails. Existing configuration must be a regular file owned by your account, without group or other access or a symlink. For example, you can save this JSON with mode `0600`:

```json
{
  "state_dir": "/absolute/private/path/wpc",
  "recheck_interval": "6h",
  "validator": "unsupported",
  "check_workers": 2,
  "check_max_attempts": 3,
  "check_mutation_interval": "1s",
  "check_candidate_timeout": "15m",
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

`recheck_interval` controls evidence freshness and the routine validation schedule. `crawl_interval` and `failure_interval` default to `1h`. The routine interval defaults to `6h`. `run` requires all three intervals to be between `1m` and `720h`, including intervals for disabled lanes. Other commands retain positive freshness intervals outside that range. Configuration rejects unknown or duplicate fields, nulls, invalid values, extra JSON documents, and files larger than 64 KiB. Root invocation, help, and version bypass configuration and state.

Results use stdout; fixed diagnostic reasons and structured run logs use stderr. Ordinary failures and unsupported checks exit 1. SIGINT and SIGTERM request cancellation and exit 130 and 143, with a bounded attempt to persist terminal state. A later writer recovers any unresolved local work.

You can keep collection and validation running in the foreground:

```sh
bin/wpc run --validator docker --validator-image sha256:<image-id>
bin/wpc run --validation-enabled=false
bin/wpc schedule show
bin/wpc schedule show 1 --format json
```

Both lanes are enabled by default. `--crawl-enabled=false` runs validation alone. The JSON fields `crawl_enabled` and `validation_enabled` accept the same controls. Automatic crawling uses all enabled configured sources and their page limits. Validation selects at most twice the worker count per batch, then reads the next due candidates from saved state. Ordinary failures receive a future attempt; restarts keep timing bases and coalesce missed intervals.

`run` owns the profile writer until it stops. Saved reports, listing, export, and schedule inspection remain available, while other writers report busy. `schedule show` reads saved facts without applying current intervals or contacting Docker. A backward clock pauses automatic admission until the saved checkpoint is reached. Shared Docker throttling survives changes of profile and requires one successful, cleaned, durably saved probe before normal capacity resumes.

The first writer upgrades SQLite to schema 5. Docker acquisition converts clean recovery state to registry 3; both versions reject older executables. Existing history is retained.

## Background operation and diagnostics

The service commands manage one systemd login service for your account on Linux amd64 or arm64. You can select an existing private configuration file at installation, then start work separately:

```sh
bin/wpc service install --config /absolute/path/config.json
bin/wpc service start
bin/wpc service status --format json
bin/wpc service logs --lines 100 --follow
bin/wpc service doctor --format json
bin/wpc service stop
```

Installation enables startup at the next login. Stop preserves that setting. `service disable` changes future startup without stopping current work, and `service enable` restores it. `service restart` confirms stop before starting again. `service uninstall` requires a stopped service and retains your configuration, saved work, logs, and validator recovery state.

The installation binds executable, configuration, state, and validator paths. A validation service also binds the local Docker executable and socket. Settings are reread on start; changing bound paths requires a stopped uninstall and new installation. For collection alone, your file can set `validation_enabled` to `false`.

Status combines separately timed manager and runner observations with one snapshot of saved work. Due times describe eligibility. They do not promise execution while a lane is disabled or blocked. Status exits 0 when its report is delivered, including when the service is unhealthy. Doctor performs local inspection without repairs or container creation and exits 1 after delivering failed checks. Both support text and JSON. Logs retain at most 20 MiB across five private files and report gaps when following crosses lost or uncertain history.

Linux arm64 native lifecycle evidence is retained in the scope. Native Linux amd64 lifecycle and provider proof, plus the remaining recovery and fault checks, are still pending. Docker tests cannot establish native user manager behavior. The full procedure is in [spec 0008 verification](docs/specs/0008-background-operation-diagnostics/verify.md).

## Development checks

The local gate uses Go `1.27.2`, `editorconfig-checker 4.0.2`, `golangci-lint 2.14.0`, and `pre-commit 4.6.2`, the versions tested for this project. You can make these tools available on your `PATH`, then enable the hook in each checkout:

```sh
pre-commit install
pre-commit run --all-files
```

Each commit checks formatting, then runs `go vet`, race tests, and lint across the module, stopping at the first failure. The formatting check runs `gofmt` and `editorconfig-checker` on every Go file under `cmd`, `internal`, and `scripts`, including new files. It reads the 120-character limit from `.editorconfig` and reports violations without changing files. Go indentation is checked by `gofmt`, since a plain text indentation check would also flag whitespace inside raw string data. The gate also runs when a commit changes only documentation. Local hooks use your installed tools and do not download separate hook environments.

To apply the GoLand rules in `.editorconfig`, then run `gofmt` and the formatting check:

```sh
rtk proxy sh scripts/format.sh
```

This command uses GoLand's synchronous command line formatter (tested with GoLand `2026.2.0.1`). It finds the standard macOS app locations or `goland.sh` on `PATH`. For another installation, set `GOLAND_BIN` to the native GoLand executable or Linux `bin/goland.sh`. Close other GoLand instances before using the command line formatter, or run Reformat Code from the open IDE. Long string literals and conditions that the formatter cannot wrap still fail the final check; split strings with concatenation to preserve their contents.

GoLand applies the IDE-specific `ij_*` settings; the portable checker enforces the supported EditorConfig properties. See the [GoLand formatter documentation](https://www.jetbrains.com/help/go/command-line-formatter.html) and [editorconfig-checker documentation](https://github.com/editorconfig-checker/editorconfig-checker).

You can run the same checks separately from the repository root:

```sh
sh scripts/check-format.sh
go vet ./...
go test -race ./...
golangci-lint run ./...
```

The [hook configuration](.pre-commit-config.yaml) follows the [pre-commit configuration reference](https://pre-commit.com/#pre-commit-configyaml---hooks). The [lint configuration](.golangci.yml) explicitly enables the five previously validated checks: `errcheck`, `govet`, `ineffassign`, `staticcheck`, and `unused`, using the [golangci-lint v2 configuration format](https://golangci-lint.run/docs/configuration/file/).

CI remains pending until you select a provider. Its planned checks are the same local gate plus builds for Linux amd64 and arm64.

You can repeat the gate and its failure cases with the [tooling verification checklist](docs/reviews/tooling/verify.md).
