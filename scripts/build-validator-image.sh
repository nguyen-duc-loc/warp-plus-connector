#!/bin/sh
set -eu

# Build only local, reviewed worker code into the pinned vendor image.
repo_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
build_dir=$(mktemp -d "${TMPDIR:-/tmp}/wpc-validator-image.XXXXXX")
trap 'rm -rf "$build_dir"' EXIT HUP INT TERM
cd "$repo_dir"
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$build_dir/wpc-worker" ./cmd/wpc-worker
cp internal/validator/image/Dockerfile "$build_dir/Dockerfile"
docker build --platform linux/arm64 --iidfile "$build_dir/image-id" -t wpc-validator:2026.8.2100.0 "$build_dir" >&2
cat "$build_dir/image-id"
