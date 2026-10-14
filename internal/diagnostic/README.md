# Diagnostic presentation

Spec 0009 defines this package. Zerolog `v1.35.1` and go-isatty `v0.0.24` were built and tested with Go `1.27.2` on macOS arm64 on 2026-10-14.

You can select `--log-level debug|info|warn|error`, `--log-format auto|pretty|text|json`, and `--color auto|never`. These flags do not change configuration or result formatting. Automatic presentation follows the actual destination of each stream. Redirected diagnostics use structured text. Pretty output keeps full record IDs and shows local date and UTC offset context.

`Emit` accepts named scalar fields. The receiving sink validates the event catalogue before display, preserves integer precision, and serializes each event with its context header. The first formatting or delivery failure is retained. Its callback only cancels work and runs after unlocking. The zerolog transport acknowledges input to prevent fallback error output. Callers must inspect `Capture.Err` before reporting success.

Managed service capture validates the same transport but saves only existing work diagnostic events. The archive retains its own timestamps, sequence, identities, and schema. Service display filters the selected records for pretty and text output, retaining counts and continuity notices. JSON remains complete.

Local checks covered a real public crawl, pseudo terminal and redirected output, check verbosity using the unsupported validator, signal exits, archive compatibility fixtures, formatting, vet, race tests, and lint. Linux amd64 and arm64 compilation is separate from native execution. Native runtime acceptance on both Linux architectures remains for `/check verify readable development logs`. This change does not establish new live provider attachment or cleanup evidence.
