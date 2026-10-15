#!/bin/sh
set -eu
# CI bootstrap uses the same checked archive as the shipped runtime.
case "$(uname -m)" in
  x86_64) ui_arch=amd64 ;;
  aarch64) ui_arch=arm64 ;;
  *) echo 'UI build tools require a supported native Linux host' >&2; exit 1 ;;
esac
ui_file=$(python3 -c 'import json,sys; print(json.load(open("ui/runtime.json"))["archives"][sys.argv[1]]["file"])' "$ui_arch")
ui_sha=$(python3 -c 'import json,sys; print(json.load(open("ui/runtime.json"))["archives"][sys.argv[1]]["sha256"])' "$ui_arch")
ui_node=$(python3 -c 'import json; print(json.load(open("ui/runtime.json"))["node"])')
ui_pnpm=$(python3 -c 'import json; print(json.load(open("ui/runtime.json"))["pnpm"])')
mkdir -p .ui-build/downloads .ui-build/tools/node
curl --fail --location --silent --show-error "https://nodejs.org/dist/v$ui_node/$ui_file" -o ".ui-build/downloads/$ui_file"
printf '%s  %s\n' "$ui_sha" ".ui-build/downloads/$ui_file" | sha256sum --check
tar -xJf ".ui-build/downloads/$ui_file" --strip-components=1 -C .ui-build/tools/node
PATH="$(pwd)/.ui-build/tools/node/bin:$PATH"
export PATH
# npm is used only to bootstrap the exact pnpm build tool from the checked Node archive.
npm install --global --prefix "$(pwd)/.ui-build/tools/node" "pnpm@$ui_pnpm" --ignore-scripts --no-audit --no-fund
# Initialize only pnpm's pinned native launcher. Its fallback script has no
# shebang, so a shell version check can pass while Python exec fails with ENOEXEC.
node .ui-build/tools/node/lib/node_modules/pnpm/install.js
python3 -c 'import subprocess,sys; assert subprocess.check_output(["pnpm", "--version"], text=True).strip() == sys.argv[1]' "$ui_pnpm"
if [ -n "${GITHUB_PATH:-}" ]; then
  printf '%s\n' "$(pwd)/.ui-build/tools/node/bin" >> "$GITHUB_PATH"
fi
