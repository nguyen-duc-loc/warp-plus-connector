#!/bin/sh
set -eu

# gofmt reports files but exits successfully even when formatting is needed.
unformatted=$(gofmt -l cmd internal)
if [ -n "$unformatted" ]; then
    printf 'Go files need formatting:\n%s\n' "$unformatted" >&2
    printf 'You can format them with gofmt -w <files>.\n' >&2
    exit 1
fi
