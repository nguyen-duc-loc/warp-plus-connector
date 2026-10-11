#!/bin/sh
set -eu

if ! command -v editorconfig-checker >/dev/null 2>&1; then
    printf 'Formatting requires editorconfig-checker (tested with 4.0.2).\n' >&2
    exit 1
fi

# gofmt reports files but exits successfully even when formatting is needed.
unformatted=$(gofmt -l cmd internal scripts)
if [ -n "$unformatted" ]; then
    printf 'Go files need formatting:\n%s\n' "$unformatted" >&2
    printf 'You can format them with gofmt -w <files>.\n' >&2
    exit 1
fi

# gofmt owns Go indentation. A text-only checker would also flag whitespace
# inside raw string literals, where changing indentation changes program data.
# Keep all other EditorConfig checks, including max_line_length, enabled.
find cmd internal scripts -type f -name '*.go' \
    -exec editorconfig-checker -disable-indentation {} + >&2
