#!/bin/sh
set -eu

# Resolve the endpoint with the same rules as the runtime validator.
repo_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir"
exec go run ./scripts/validator-image "$@"
