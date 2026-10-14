# Terminal overview

Bare `wpc` opens the overview on Linux amd64 or arm64 when stdin and stdout are terminals and `TERM` is set to a value other than `dumb`. Other invocations show plain root help. Explicit commands, help, and version continue to work without the UI runtime.

You can use `--config`, `--state-dir`, and `--recheck-interval` on the root invocation. Configuration is resolved once. Restart the overview to apply configuration edits. Opening or leaving the overview never starts or stops background work.

## Installation

Extract the complete release archive into its own version directory. Keep `wpc` and `lib/` together, then place a symlink to that `wpc` on your `PATH`. The executable resolves launcher symlinks before finding its runtime. Copying only `wpc` supports explicit commands but cannot open the overview.

The bundled runtime is official Node 24.20.0. It requires Linux kernel 4.18 or newer and glibc 2.28 or newer. The pinned Debian 10 fixture uses its stock loader and C++ runtime. The native arm64 runtime probe used kernel 6.8.0 and glibc 2.28. These prerequisites alone do not prove every distribution combination works. No host Node, npm, global UI installation, or startup network access is needed.

Both pinned Node ELF files require C++ symbols through `GLIBCXX_3.4.21` and `CXXABI_1.3.11`. The passing arm64 baseline supplied `libc6 2.28-10+deb10u3`, `libstdc++6 8.3.0-6`, and `libgcc1 1:8.3.0-6`. The native Ubuntu 26.04.1 arm64 package checks used kernel `7.0.0-38-generic`. Exact observations and the baseline image digest are recorded in `ui/runtime.json`. Native amd64 runtime execution remains pending.

The UI manifest binds the executable version and commit, operating system, architecture, dependency lock digest, and every shipped runtime or app file digest. Missing or changed files cause a startup error. The package includes runtime and dependency licenses. Installing another UI version does not update a bound background service, which retains its existing upgrade procedure.

## Reading the overview

Collection totals, freshness, the latest ten saved runs, and schedule facts share one database snapshot. Fresh confirmed evidence can coexist with a later inconclusive outcome. Freshness is separate from the latest outcome counts. Older saved history has an explicit notice.

Installation, manager, and runner observations remain independent. A service bound to another collection is labeled with both paths. An unavailable observation never becomes a zero count or stopped status. Previous successful data may remain visible as stale, with its original time. A changed or uncertain service identity clears retained service observations.

The first read starts on entry. Automatic reads start five seconds after the preceding read completes. Pressing `r` during a read queues one refresh. No refresh overlaps another.

## Keyboard

| Key | Action |
| --- | --- |
| Tab, Shift+Tab | Focus the next or previous section |
| Up, Down | Scroll one body row |
| Page Up, Page Down | Scroll one viewport |
| Home, End | Go to the body limits |
| r | Refresh |
| ? | Toggle keyboard help |
| Esc | Leave help |
| q | Quit normally |
| Ctrl+C | Interrupt, exit 130 |

The overview needs at least 60 columns and 16 rows. At smaller sizes, quit, refresh, and help remain available. `--color never` or a nonempty `NO_COLOR` removes decorative styling. Dynamic control characters are escaped. Terminal movement still needs escape sequences.

## Local packaging

Use the exact Node and npm versions in `ui/runtime.json`. On native Linux, `sh scripts/install-ui-tools.sh` prepares those build tools under `.ui-build/tools/node`; add its `bin` directory to your build shell's `PATH`. This is build tooling, never an application startup step.

```sh
rtk proxy python3 scripts/prepare-ui.py --arch amd64
rtk proxy python3 scripts/prepare-ui.py --arch arm64
rtk proxy goreleaser release --snapshot --clean
```

Preparation installs from the lockfile, compiles TypeScript, preserves production modules and their assets, checks the official Node archive checksum, and writes the file manifest. GoReleaser stamps the exact release identity before archiving. On each matching native Linux host, `sh scripts/check-ui-package.sh amd64` (or `arm64`) exercises the real package with PTYs on the host and the pinned Debian baseline, with networking disabled in the baseline. It includes normal quit, signals, child crash, resizing, refresh, missing state, and a real saved collection of 105 synthetic candidates.

CI prepares each tree on its matching architecture and passes the verified trees to snapshot and release packaging. A successful hosted workflow is still required to establish hosted execution. Native Ubuntu 26.04 checks and the full acceptance checklist remain part of spec 0010 verification.
