#!/bin/sh
set -eu

# Use the synchronous IDE binary, not Toolbox's macOS `open` launcher.
formatter=${GOLAND_BIN:-}
if [ -z "$formatter" ]; then
    for candidate in "$HOME/Applications/GoLand.app/Contents/MacOS/goland" \
        /Applications/GoLand.app/Contents/MacOS/goland; do
        if [ -x "$candidate" ]; then
            formatter=$candidate
            break
        fi
    done
fi
if [ -z "$formatter" ]; then
    formatter=$(command -v goland.sh || true)
fi
if [ -z "$formatter" ] || ! [ -x "$formatter" ]; then
    printf 'Set GOLAND_BIN to the GoLand executable or bin/goland.sh.\n' >&2
    exit 1
fi
if ! command -v editorconfig-checker >/dev/null 2>&1; then
    printf 'Formatting requires editorconfig-checker (tested with 4.0.2).\n' >&2
    exit 1
fi

# GoLand reads .editorconfig, including its ij_go_* wrapping settings.
"$formatter" format -allowDefaults -r -m '*.go' cmd internal scripts
gofmt -w cmd internal scripts
sh scripts/check-format.sh
