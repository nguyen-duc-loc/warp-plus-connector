# Diagnostic presentation

The contract is [spec 0009](../../docs/specs/0009-readable-development-logs/index.md). This package owns the event catalogue, severity, presentation options, destination detection, safe scalar encoding, serialized output, and diagnostic failure state. Cobra parsing stays in `internal/cli`, archive ownership stays in `internal/service`, and process exits stay in `cmd/wpc`.

## Events and privacy

[events.go](events.go) defines `Field`, `Record`, `Emit`, `Decode`, and severity mapping. [codes.go](codes.go) holds the permitted diagnostic codes. Use `String`, `Int`, and `Bool` fields with the event schema. `Decode` rejects unknown fields, unsupported values, malformed records, and transport records larger than 16 KiB. Preserve integer IDs and counts without floating point conversion.

`NewLogger` uses the supplied clock at the event boundary and UTC timestamps with microsecond precision. Avoid changes to global zerolog settings. A nil logger disables `Emit`. Keep raw errors, keys, credentials, vendor content, and validator session identifiers outside the event catalogue. Final error details come from typed facts, never parsed error strings.

## Presentation

[sink.go](sink.go) owns invocation options and destination capabilities. Detect the original destination before wrapping it. Automatic output is pretty on a terminal unless `TERM=dumb`, and structured text otherwise. Styling also requires an empty `NO_COLOR` and a color choice other than `never`; explicit pretty output on a redirected stream has no styling.

Pretty rows retain full IDs, local milliseconds, full level labels, and date and UTC offset context whenever either changes. Event order remains delivery order, including clock regression. Structured text preserves quoting, and JSON contains complete objects without terminal styling. Escape dynamic control characters before display. Historical `Display` uses the supplied record time; `Notice` has no invented timestamp.

Minimum levels filter activity, but `command_cancelled` remains visible even at error verbosity. Service archive selection and adaptation belong to the caller; this package does not add missing historical events or change retention.

## Failure handling

`Capture` serializes consumption, retains the first failure, and invokes its cancellation callback once after unlocking. That callback should only cancel work. Transport `Write` deliberately acknowledges all bytes even after failure to contain zerolog fallback output; `Capture.Err` is the authoritative result. Never retry the destination after a short write, formatting failure, or write error.

`Sink.Display` and `Sink.Notice` return failures to the archive reader. They do not stop the managed service. Foreground callers retain the same sink through final rendering and honor its failure state.

## Checks

You can run `rtk proxy go test -race ./internal/diagnostic ./internal/cli ./internal/service ./cmd/wpc` for focused checks. Tests supply explicit destination capabilities, clocks, and failing writers. Native terminal and archive evidence is recorded in [the verification checklist](../../docs/specs/0009-readable-development-logs/verify.md).

_Drafted by /sync from the introducing change, worth a quick human pass._
