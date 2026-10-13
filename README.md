# Warp Plus Connector (WPC)

WPC is a personal command line tool for collecting publicly shared WARP Plus key candidates from Telegram, checking their entitlement, and keeping saved results current. You can inspect and export the complete collection, run scheduled work in the foreground, or manage it through a Linux systemd user service.

WPC reads public Telegram previews without a Telegram login. It stores candidates, source provenance, crawl coverage, and validation history in a private local SQLite database. Keys stay hidden in normal output. There is no fixed 100 candidate limit on listing or export.

A collected key is only a candidate. Docker validation uses an isolated official WARP client and requires matching Unlimited evidence plus confirmed cleanup before reporting success. A result describes an observation at a particular time; it does not reserve a device slot or guarantee later activation. You apply a key manually outside WPC.

**Platforms:** Linux amd64 and arm64. macOS may be used for development, but is outside the release scope. Native Linux service and test evidence exists for both architectures. Background operation remains in progress in the [scope](docs/scope/scope.md): the latest verification excluded new provider attachments and fresh Unlimited confirmation, which remain unverified.

## Contents

* [Quick start](#quick-start)
* [Commands](#commands)
* [Collect candidates](#collect-candidates)
* [Check keys with Docker](#check-keys-with-docker)
* [List, filter, and export](#list-filter-and-export)
* [Configuration](#configuration)
* [Scheduled operation](#scheduled-operation)
* [Background service](#background-service)
* [Troubleshooting](#troubleshooting)
* [Development](#development)

## Quick start

You need Go `1.27.2`, the toolchain recorded in [go.mod](go.mod), to build from source. Collection needs network access to public Telegram previews. Docker is needed only for validation; background service controls also require a Linux systemd user manager.

All examples below assume you are in the repository root.

```sh
go mod download
go build -o bin/wpc ./cmd/wpc
./bin/wpc version
./bin/wpc --help
```

For a first collection, no configuration file or Docker setup is required:

```sh
./bin/wpc sources
./bin/wpc crawl
./bin/wpc list
./bin/wpc export --output candidates.json
```

`sources` shows the effective channels and page limits. `crawl` creates local state and saves discoveries. `list` shows saved candidate IDs and outcomes. The export creates a private JSON file without keys and refuses to replace an existing file.

To check candidates, you can continue with [Docker setup](#check-keys-with-docker). Without it, the default validator records `validation_unsupported` as an inconclusive attempt and exits 1.

## Commands

| Command | Purpose |
| --- | --- |
| `sources` | Show configured channels, enabled state, and page limits |
| `crawl` | Fetch bounded public previews and save candidates |
| `crawl show <run-id>` | Read a saved crawl report and coverage |
| `check <candidate-id>...` | Validate selected saved candidates |
| `check show <run-id>` | Read saved selection, policy, attempts, and totals |
| `list` | Show every matching candidate as text |
| `export` | Export summaries as JSON or exact key lines |
| `validator recover` | Reconcile retained validation sessions without a new registration |
| `run` | Continuously crawl and validate on saved schedules |
| `schedule show [candidate-id]` | Explain saved scheduling facts as text or JSON |
| `service` | Install and control background operation, inspect status, logs, and diagnostics |
| `version` | Show the executable version and supplied commit |

You can append `--help` to any command for its flags. With no subcommand, WPC displays help. Help and version do not read configuration, create state, or contact the network.

Candidate IDs come from `list`; run IDs come from crawl and check reports. The numeric IDs in this README are examples you can replace with your saved IDs.

## Collect candidates

The default sources are `warpplus`, `warppluscn`, `warpplushome`, and `warp_veyke`, with one page per channel per crawl.

```sh
./bin/wpc crawl
./bin/wpc crawl --source warpplus --source warppluscn --page-limit 2
./bin/wpc crawl show 1
```

`--source` selects configured enabled channels and preserves configuration order. It cannot add an unconfigured channel or enable a disabled one. `--page-limit` overrides the limits of all selected channels for that invocation. Both flags belong to `crawl`; automatic crawling uses the source settings in your configuration.

Repeated crawls retain exact candidate identity and provenance from source messages. Reports distinguish exhausted page budgets, the end of the visible preview, and failures. They always report `history_complete=false`: public previews do not establish complete channel history.

Requests are sequential, with at least one second between starts, a 20 second deadline, and a 2 MiB decoded body limit. A channel failure allows later channels to run; HTTP 429 stops further requests. Higher page limits follow validated older navigation without an extra probe or retry.

## Check keys with Docker

Validation requires a native Linux executable, a local Linux Docker daemon, and a worker image with the same amd64 or arm64 architecture. The Docker CLI must be available to your account. Only local Unix socket endpoints are accepted.

You can build the validator image on that Linux host and capture its immutable image ID:

```sh
validator_image_id=$(sh scripts/build-validator-image.sh)
./bin/wpc check 1 2 3 \
  --validator docker \
  --validator-image "$validator_image_id"
./bin/wpc check show 2
./bin/wpc list
```

You can select saved candidates you are authorized to activate. A check creates a temporary WARP registration, attaches the candidate, inspects attributable account evidence, and deletes that registration. It does not change your host WARP registration or connect a host tunnel.

Image preparation downloads the signed official WARP package pinned to `2026.8.2100.0`. The script detects the local daemon architecture, or accepts a matching `amd64` or `arm64` argument. It verifies the image, installed package, protocol labels, and worker bytes before printing the complete `sha256:` ID. Checks use the prepared local image without pulling or installing software. Image preparation is also possible on a development host with a matching local Linux daemon; validation itself requires Linux.

### Outcomes and retries

| Latest outcome | Meaning |
| --- | --- |
| `unchecked` | No completed validation attempt is saved |
| `confirmed` | Matching Unlimited evidence was observed and cleanup was proven |
| `invalid` | A definitive rejection was recorded |
| `inconclusive` | The attempt could not establish a definitive result |

A matching Free account is inconclusive with `not_unlimited`. A full account reports `device_limit`. Temporary failures do not become invalid results. Inconclusive attempts preserve earlier definitive evidence, so `LATEST` and `LAST_DEFINITIVE` may differ.

`check` deduplicates candidate IDs in supplied order and resolves the complete selection before saving a batch. Each final item prints after its result is committed, followed by batch totals. `check show` includes the policy, every attempt, retry waits, and totals without Docker access.

| Check flag, also available on `run` | Default | Accepted range |
| --- | --- | --- |
| `--workers` | `2` | 1 through 4 |
| `--max-attempts` | `3` | 1 through 5 |
| `--mutation-interval` | `1s` | Whole seconds from 1s through 60s |
| `--candidate-timeout` | `15m` | 1m through 60m |

Mutation pacing applies after registration and license commands. The candidate budget starts at the first admitted attempt and includes later waits. Only classified temporary failures retry, after proven cleanup and container removal. Retry waits increase with jitter. Free accounts, device capacity, unsupported validation, and unclassified errors do not retry. The pinned adapter cannot provide a trusted server delay, so throttling stops the batch.

### Recover interrupted checks

The validator shares one recovery registry across profiles and supports up to four isolated sessions. New checks first reconcile retained sessions. You can also request cleanup alone:

```sh
./bin/wpc validator recover \
  --validator docker \
  --validator-image "$validator_image_id"
```

An unknown remote outcome retains the container and blocks further attachments with `validation_cleanup_pending`. Deleting a container or its journal does not prove remote cleanup. Recovery does not create a registration or reapply a license. The [validator contract](docs/specs/0003-source-crawl-validation/0003-docker-validator.md) describes this boundary.

## List, filter, and export

Saved reports, listing, export, and schedule inspection work without contacting Telegram or Docker. They remain available while `run` holds the profile writer.

```sh
./bin/wpc list
./bin/wpc list --status confirmed --freshness fresh
./bin/wpc list --status unchecked,inconclusive --limit 25
./bin/wpc list --definitive confirmed --show-keys
./bin/wpc export --output candidates.json
./bin/wpc export --show-keys --output candidates-with-keys.json
./bin/wpc export --format keys \
  --status confirmed --freshness fresh --output confirmed-keys.txt
```

`list` and `export` share these filters:

| Flag | Values |
| --- | --- |
| `--status` | `unchecked`, `confirmed`, `invalid`, `inconclusive` |
| `--definitive` | `none`, `confirmed`, `invalid` |
| `--freshness` | `fresh`, `stale`, `unknown`, `not_applicable` |

Filters accept repeated flags or comma separated values. Values within a filter are alternatives; different filters must all match. `--status` examines the latest outcome. `--definitive` examines the last definitive result, including evidence retained after an inconclusive attempt. Freshness is evaluated using `--recheck-interval`, which defaults to six hours.

Results are unlimited unless you supply a positive `--limit`. Each command reads one consistent database snapshot. `truncated=true` means another matching candidate existed beyond the requested limit. JSON includes schema version 1, evaluation time, filters, count, limit, and truncation metadata. Text and key line completion summaries go to stderr.

Keys appear only with `list --show-keys`, `export --show-keys`, or `export --format keys`. The keys format writes one exact key per line and rejects unsafe line characters. It cannot be combined with `--show-keys`, including `--show-keys=false`.

Without `--output`, export writes to stdout. `--output -` is not supported. Named exports create a private file atomically, refuse replacement, and must be outside the canonical state directory. JSON contains candidate summaries, not a complete history or provenance archive. The [export contract](docs/specs/0006-complete-results-export/index.md) documents the schema and delivery guarantees.

## Configuration

WPC resolves settings in this order: built in defaults, a private JSON file, then explicitly supplied flags. The default file is optional. An explicit `--config` path must exist.

On Linux, the default configuration is `$XDG_CONFIG_HOME/warp-plus-connector/config.json`, or `$HOME/.config/warp-plus-connector/config.json` when `XDG_CONFIG_HOME` is unset. The file must be a regular file owned by your account, with no group or other access and no symlink. You can use file mode `0600`.

For example, this configuration collects two channels and supports scheduled collection without Docker:

```json
{
  "crawl_enabled": true,
  "validation_enabled": false,
  "crawl_interval": "1h",
  "recheck_interval": "6h",
  "failure_interval": "1h",
  "crawl_page_limit": 1,
  "sources": [
    {"channel": "warpplus", "page_limit": 2},
    {"channel": "warppluscn"},
    {"channel": "warpplushome", "enabled": false}
  ]
}
```

After saving it to your chosen path, you can set its permissions and inspect the resolved sources:

```sh
chmod 600 /absolute/path/config.json
./bin/wpc sources --config /absolute/path/config.json
./bin/wpc run --config /absolute/path/config.json
```

### Settings reference

| JSON field | Default | Meaning or corresponding flag |
| --- | --- | --- |
| `state_dir` | Platform state directory | Profile database location, `--state-dir` |
| `validator` | `"unsupported"` | `unsupported` or `docker`, `--validator` |
| `validator_image` | Unset | Complete immutable `sha256:` image ID, `--validator-image` |
| `validator_socket` | Selected local Docker context | Absolute Unix socket path, `--validator-socket` |
| `check_workers` | `2` | `--workers` |
| `check_max_attempts` | `3` | `--max-attempts` |
| `check_mutation_interval` | `"1s"` | `--mutation-interval` |
| `check_candidate_timeout` | `"15m"` | `--candidate-timeout` |
| `crawl_enabled` | `true` | Enable collection in `run`, `--crawl-enabled` |
| `validation_enabled` | `true` | Enable validation in `run`, `--validation-enabled` |
| `crawl_interval` | `"1h"` | Routine collection interval, `--crawl-interval` |
| `recheck_interval` | `"6h"` | Evidence freshness and routine validation interval, `--recheck-interval` |
| `failure_interval` | `"1h"` | Failed work recovery interval, `--failure-interval` |
| `crawl_page_limit` | `1` | Default pages per configured channel |
| `sources` | Four default channels | Replacement source list |

To enable Docker in the example, you can set `validation_enabled` to `true`, set `validator` to `"docker"`, and add `validator_image` with the ID returned by the build script. An optional `validator_socket` selects an absolute local Unix socket path.

Omitting `sources` uses the defaults. An explicit list replaces them, including an empty list. Channel names use bare ASCII letters, digits, or underscores and normalize to lowercase. Duplicate channels are invalid. WPC accepts up to 32 entries and page limits from 1 through 100. A source's `page_limit` overrides `crawl_page_limit`; a crawl's explicit `--page-limit` overrides both. Disabled entries are validated too. `crawl` and `run` with crawling enabled reject an empty enabled selection.

Unknown or duplicate JSON fields, nulls, invalid values, extra JSON documents, and files larger than 64 KiB are rejected. Relative state paths resolve from the invocation directory. JSON values do not expand `~` or shell variables.

### State and privacy

On Linux, state defaults to an absolute `$XDG_STATE_HOME/warp-plus-connector`, or `$HOME/.local/state/warp-plus-connector`. `--state-dir` selects a separate profile. On a macOS development host, the default configuration and state base is `~/Library/Application Support`.

State is private to your account and includes complete candidate keys. Hidden output is not encryption. The validator recovery directory remains under the platform default state directory, independent of a profile override. Service records live under the account home at `.local/state/warp-plus-connector/service`.

Only one writer can own a profile. Writers apply migrations and recover interrupted local work; saved readers do neither. Current writes use SQLite schema 5, validator registry 3, and worker protocol 2. Older executables cannot use the upgraded state. Retained validator sessions are reconciled with their original image during recovery.

## Scheduled operation

`run` keeps independent crawl and validation lanes active until stopped. Both lanes are enabled by default, so an unconfigured `run` requires Docker validation setup. Collection alone is available explicitly:

```sh
./bin/wpc run --validation-enabled=false
```

For both lanes, you can use the image ID from the earlier setup:

```sh
./bin/wpc run \
  --validator docker --validator-image "$validator_image_id"
```

`--crawl-enabled=false` selects validation alone. Both lanes cannot be disabled together. You can change intervals with `--crawl-interval`, `--recheck-interval`, and `--failure-interval`. For `run`, all three must be between `1m` and `720h`, including intervals for disabled lanes.

The scheduler persists timing across restarts and coalesces missed intervals. Validation admits at most twice the worker count per batch, then reads the next due candidates. Ordinary failures receive a future attempt. A backward clock pauses automatic admission until the saved checkpoint is reached.

From another terminal, you can inspect saved eligibility:

```sh
./bin/wpc schedule show
./bin/wpc schedule show 1 --format json
```

The report reads saved facts without applying current intervals or contacting Docker. A due time means eligible, not guaranteed execution while a lane is disabled or blocked. A running profile rejects other writers, including manual crawl and check commands, with `state_busy`.

## Background service

The service commands manage one systemd login service for your account on Linux amd64 or arm64. You need an existing private configuration file and an executable in a stable location. Installation binds the executable, configuration, state, and validator paths; Docker validation also binds the local Docker executable and socket.

For collection alone, the configuration example above is sufficient:

```sh
./bin/wpc service install --config /absolute/path/config.json
./bin/wpc service start
./bin/wpc service status --format json
./bin/wpc service logs --lines 100 --follow
```

You can stop following logs with Ctrl+C without stopping the service. Other controls are:

```sh
./bin/wpc service doctor --format json
./bin/wpc service restart
./bin/wpc service stop
./bin/wpc service disable
./bin/wpc service enable
./bin/wpc service uninstall
```

Installation enables startup at the next login and does not start work immediately. `stop` preserves that setting. `disable` changes future startup without stopping current work; `enable` restores it. `restart` confirms a stop before starting again. `uninstall` requires a stopped service and retains configuration, saved work, logs, and recovery state. WPC does not manage boot services or Linux lingering.

Settings are reread on start. Changing a bound path requires a stopped uninstall and new installation. Status reports native manager, runner, and saved work observations separately. A successful status command means its report was delivered, even if the service is unhealthy. Doctor performs local inspection without repairs, recovery, or container creation, and exits 1 when checks fail.

Logs retain at most 20 MiB across five private files. Following reports gaps if rotation loses or obscures history. Status, logs, and doctor support text and JSON output. See the [service verification contract](docs/specs/0008-background-operation-diagnostics/verify.md) for native lifecycle and fault checks.

## Troubleshooting

| Symptom | Meaning and next step |
| --- | --- |
| `state_not_initialized` | No saved database exists for the selected profile. You can run `crawl` first. |
| `state_busy` | Another writer owns the profile. You can stop its foreground runner or service before manual writes. Saved reads remain available. |
| `validation_unsupported` | The default backend cannot validate. You can configure Docker, or use `run --validation-enabled=false` for collection. |
| `validation_cleanup_pending` | Remote cleanup is uncertain. You can restore the original local Docker environment and run `validator recover`. Deleting retained state is not proof of cleanup. |
| `device_limit` | The account has no available device capacity. This is inconclusive and is not retried by the check batch. |
| `not_unlimited` | Matching account evidence was Free, so Plus entitlement was not confirmed. |
| `validation_rate_limited` | The provider throttled validation. The pinned adapter stops the batch; scheduled operation retains shared throttling state. |
| Configuration access error | You can check file ownership, regular file status, mode `0600`, and whether the supplied path exists without a symlink. |
| Export destination exists | Named export never overwrites. You can choose a new path outside the state directory. |
| Export error includes `published=true` | The complete destination file was already published and retained. You can inspect it before retrying. |
| Output error includes `delivered=true` | Output delivery had already occurred before a later failure. You can account for that before retrying in a pipeline. |

Results use stdout; diagnostics, completion summaries, and structured run logs use stderr. Ordinary command failures and inconclusive checks exit 1. Reading a historical failed check report exits 0 if the read succeeds. SIGINT and SIGTERM request cancellation and exit 130 and 143. Shutdown attempts to save terminal state and clean up remote work; forced termination does not establish remote cleanup.

## Development

WPC uses one Go module, Cobra for commands, SQLite through `database/sql` and `modernc.org/sqlite`, and goquery for public preview parsing. Exact dependencies are in [go.mod](go.mod) and [go.sum](go.sum). The build does not require a C compiler.

| Path | Responsibility |
| --- | --- |
| `cmd/wpc` | Process exit, signals, and output streams |
| `internal/cli` | Command parsing and rendering |
| `internal/app` | Crawl, validation, and scheduling coordination |
| `internal/config` | Private configuration and explicit overrides |
| `internal/state` | Domain records |
| `internal/store` | SQLite persistence, migrations, and recovery |
| `internal/source` | Bounded public retrieval and candidate extraction |
| `internal/validator` | Isolated Docker checks and remote recovery |
| `internal/service` | Native service controls and diagnostics |

### Local checks

The local gate uses Go `1.27.2`, `editorconfig-checker 4.0.2`, `golangci-lint 2.14.0`, and `pre-commit 4.6.2`. With those tools on your `PATH`, you can install and run the hooks:

```sh
pre-commit install
pre-commit run --all-files
```

The gate runs formatting, `go vet`, race tests, and lint in order, stopping at the first failure. It runs even for documentation changes. You can also run the checks separately:

```sh
sh scripts/check-format.sh
go vet ./...
go test -race ./...
golangci-lint run ./...
```

Formatting checks Go files under `cmd`, `internal`, and `scripts` against gofmt and [.editorconfig](.editorconfig), including the 120 character limit. To apply the GoLand rules and gofmt before checking, you can use `sh scripts/format.sh`. It uses GoLand `2026.2.0.1`, detected from standard macOS app locations or `goland.sh` on `PATH`; `GOLAND_BIN` selects another installation. The command line formatter requires other GoLand instances to be closed. You can also reformat in the open IDE. Long strings may still need manual splitting without changing their values.

The [tooling verification checklist](docs/reviews/tooling/verify.md) covers the gate and its failure cases. CI remains pending provider selection.

### Release builds

A local build reports `wpc dev`. You can supply actual release metadata through `main.version` and `main.commit`:

```sh
go build \
  -ldflags '-X main.version=0.1.0 -X main.commit=abc1234' \
  -o bin/wpc ./cmd/wpc
```

For Linux amd64 and arm64 snapshots, the repository uses GoReleaser `2.18.3`:

```sh
goreleaser check
goreleaser build --snapshot --clean
goreleaser release --snapshot --clean
```

The [release configuration](.goreleaser.yaml) disables CGO and embeds version metadata. Snapshot commands replace generated `dist/` output. The release command also creates local archives and SHA256 checksums without publishing. The validator image uses its separate build script.

### Project records

The [architecture](docs/specs/0001-stack-architecture/index.md) defines the package boundaries. The [scope](docs/scope/scope.md) records feature status, verification evidence, and deferred work. Feature contracts live under [docs/specs](docs/specs/).

Automatic key application or rotation, private Telegram access, unlimited historical backfill, a public API or dashboard, notifications, and public release automation are outside the current scope.
