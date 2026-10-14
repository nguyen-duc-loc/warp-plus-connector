#!/bin/sh
set -eu
ui_arch=${1:?supply amd64 or arm64}
case "$ui_arch:$(uname -m)" in
  amd64:x86_64|arm64:aarch64) ;;
  *) echo 'Package checks require the matching native architecture' >&2; exit 1 ;;
esac
ui_baseline=$(python3 -c 'import json; print(json.load(open("ui/runtime.json"))["baseline"])')
CGO_ENABLED=0 go build -o ".ui-build/$ui_arch/wpc" ./cmd/wpc
CGO_ENABLED=0 go test -c -o ".ui-build/terminal-$ui_arch.test" ./internal/terminal
WPC_UI_PACKAGE="$(pwd)/.ui-build/$ui_arch/wpc" ".ui-build/terminal-$ui_arch.test" -test.v -test.run '^TestPackagedTerminal$'
docker run --rm --network none \
  -v "$(pwd)/.ui-build:/fixture:ro" \
  -e "WPC_UI_PACKAGE=/fixture/$ui_arch/wpc" \
  "$ui_baseline" "/fixture/terminal-$ui_arch.test" -test.v -test.run '^TestPackagedTerminal$'
