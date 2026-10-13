# Configuration reference

[Back to the README](../README.md)


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
wpc sources --config /absolute/path/config.json
wpc run --config /absolute/path/config.json
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

To enable Docker in the example, you can set `validation_enabled` to `true`, set `validator` to `"docker"`, and add `validator_image` with the immutable local ID obtained by inspecting the downloaded release image or returned by the build script. An optional `validator_socket` selects an absolute local Unix socket path.

Omitting `sources` uses the defaults. An explicit list replaces them, including an empty list. Channel names use bare ASCII letters, digits, or underscores and normalize to lowercase. Duplicate channels are invalid. WPC accepts up to 32 entries and page limits from 1 through 100. A source's `page_limit` overrides `crawl_page_limit`; a crawl's explicit `--page-limit` overrides both. Disabled entries are validated too. `crawl` and `run` with crawling enabled reject an empty enabled selection.

Unknown or duplicate JSON fields, nulls, invalid values, extra JSON documents, and files larger than 64 KiB are rejected. Relative state paths resolve from the invocation directory. JSON values do not expand `~` or shell variables.

### State and privacy

On Linux, state defaults to an absolute `$XDG_STATE_HOME/warp-plus-connector`, or `$HOME/.local/state/warp-plus-connector`. `--state-dir` selects a separate profile. On a macOS development host, the default configuration and state base is `~/Library/Application Support`.

State is private to your account and includes complete candidate keys. Hidden output is not encryption. The validator recovery directory remains under the platform default state directory, independent of a profile override. Service records live under the account home at `.local/state/warp-plus-connector/service`.

Only one writer can own a profile. Writers apply migrations and recover interrupted local work; saved readers do neither. Current writes use SQLite schema 5, validator registry 3, and worker protocol 2. Older executables cannot use the upgraded state. Retained validator sessions are reconciled with their original image during recovery.

