#!/bin/sh
set -eu
ui_arch=${1:?supply amd64 or arm64}
case "$ui_arch:$(uname -m)" in
  amd64:x86_64|arm64:aarch64) ;;
  *) echo 'Package checks require the matching native architecture' >&2; exit 1 ;;
esac
ui_baseline=$(python3 -c 'import json; print(json.load(open("ui/runtime.json"))["baseline"])')
mkdir -p .ui-build
if [ "$#" -gt 1 ]; then
  ui_bundle=$(mktemp -d "$(pwd)/.ui-build/archive-$ui_arch.XXXXXX")
  trap 'rm -rf "$ui_bundle"' EXIT HUP INT TERM
  tar -xzf "$2" -C "$ui_bundle"
else
  ui_bundle="$(pwd)/.ui-build/$ui_arch"
  CGO_ENABLED=0 go build -o "$ui_bundle/wpc" ./cmd/wpc
fi
CGO_ENABLED=0 go test -c -o ".ui-build/terminal-$ui_arch.test" ./internal/terminal
uname -srmo
getconf GNU_LIBC_VERSION
"$ui_bundle/lib/wpc-ui/node/bin/node" --version
PATH=/nonexistent WPC_UI_PACKAGE="$ui_bundle/wpc" \
  ".ui-build/terminal-$ui_arch.test" -test.v -test.run '^TestPackagedTerminal$'
docker run --rm --network none \
  -v "$ui_bundle:/package:ro" \
  -v "$(pwd)/.ui-build/terminal-$ui_arch.test:/terminal.test:ro" \
  -e WPC_UI_PACKAGE=/package/wpc \
  "$ui_baseline" sh -c '
    set -eu
    uname -srmo
    getconf GNU_LIBC_VERSION
    dpkg-query -W libc6 libstdc++6 libgcc1
    /package/lib/wpc-ui/node/bin/node --version
    PATH=/nonexistent exec /terminal.test -test.v -test.run "^TestPackagedTerminal$"
  '
