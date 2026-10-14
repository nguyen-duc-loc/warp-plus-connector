package cli

import (
	"context"
	"errors"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/diagnostic"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/service"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/terminal"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

type diagnosticKey struct{}
type invocationDiagnostic struct {
	sink   *diagnostic.Sink
	logger *zerolog.Logger
}

func presentation(cmd *cobra.Command) diagnostic.Options {
	f := cmd.Root().PersistentFlags()
	level, _ := f.GetString("log-level")
	format, _ := f.GetString("log-format")
	color, _ := f.GetString("color")
	return diagnostic.Options{Level: level, Format: format, Color: color}
}

func diagnosticFlags(root *cobra.Command) {
	root.PersistentFlags().String("log-level", "info", "Diagnostic minimum level: debug, info, warn, error")
	root.PersistentFlags().String("log-format", "auto", "Diagnostic format on stderr: auto, pretty, text, json")
	root.PersistentFlags().String("color", "auto", "Terminal styling: auto or never")
	// Wrapping execution also catches application failures, for which Cobra skips post hooks.
	var wrap func(*cobra.Command)
	wrap = func(cmd *cobra.Command) {
		if run := cmd.RunE; run != nil && cmd.Name() != "version" && cmd != root && cmd.Name() != "service" {
			cmd.RunE = func(cmd *cobra.Command, args []string) (err error) {
				if presentation(cmd).Validate() != nil {
					return errArguments
				}
				ctx := cmd.Context()
				cancel := func() {}
				if cmd.Name() == "crawl" || cmd.Name() == "check" || cmd.Name() == "run" {
					ctx, cancel = context.WithCancel(ctx)
				}
				defer cancel()
				inv := newInvocation(cmd, presentation(cmd), cancel)
				ctx = context.WithValue(ctx, diagnosticKey{}, inv)
				cmd.SetContext(ctx)
				// Retain the same sink for main's final error or signal even after work returns.
				root.SetContext(context.WithValue(root.Context(), diagnosticKey{}, inv))
				defer func() {
					if inv.sink.Err() != nil {
						err = &failedDiagnostic{}
					}
				}()
				return run(cmd, args)
			}
		}
		for _, child := range cmd.Commands() {
			wrap(child)
		}
	}
	wrap(root)
}

func newInvocation(cmd *cobra.Command, o diagnostic.Options, cancel func()) *invocationDiagnostic {
	w := cmd.ErrOrStderr()
	sink := diagnostic.NewSink(w, o, diagnostic.Detect(w), time.Local, cancel)
	return &invocationDiagnostic{sink: sink, logger: diagnostic.NewLogger(sink, time.Now)}
}
func invocation(cmd *cobra.Command) *invocationDiagnostic {
	if cmd.Context() == nil {
		return nil
	}
	inv, _ := cmd.Context().Value(diagnosticKey{}).(*invocationDiagnostic)
	return inv
}

// RenderFailure delivers one safe final failure through the invocation diagnostic sink.
// Process status remains the caller's responsibility. A failed stream is never retried.
func RenderFailure(cmd *cobra.Command, err error, exitCode int) {
	if err == nil && exitCode == 0 {
		return
	}
	var suppressed interface{ DiagnosticFailed() bool }
	if errors.As(err, &suppressed) && suppressed.DiagnosticFailed() {
		return
	}
	inv := invocation(cmd)
	if inv == nil {
		o := presentation(cmd)
		// An invalid presentation value cannot select its own error format.
		if o.Validate() != nil {
			o.Level, o.Format = "info", "auto"
			if o.Color != "never" {
				o.Color = "auto"
			}
		}
		inv = newInvocation(cmd, o, nil)
	}
	if inv.sink.Err() != nil {
		return
	}
	if exitCode == 130 || exitCode == 143 {
		diagnostic.Emit(inv.logger, "command_cancelled", diagnostic.Int("exit_code", int64(exitCode)))
		return
	}
	code, fields, omit := diagnosticDetails(err)
	diagnostic.CommandError(inv.logger, code, Diagnostic(err), omit, fields...)
}

func diagnosticDetails(err error) (string, []diagnostic.Field, bool) {
	var ui *terminal.Error
	if errors.As(err, &ui) {
		return safeDiagnosticCode(ui.Code), nil, false
	}
	var control *controlDeliveryError
	if errors.As(err, &control) {
		r := control.result
		return "output_error", []diagnostic.Field{
			diagnostic.String("action", r.Action), diagnostic.Bool("changed", r.Changed),
			diagnostic.String("installation_id", r.InstallationID), diagnostic.String("state", r.State),
		}, false
	}
	var native *service.Error
	if errors.As(err, &native) {
		var fields []diagnostic.Field
		if native.Changed {
			fields = append(
				fields, diagnostic.Bool("changed", true),
				diagnostic.Bool("operation_incomplete", true),
			)
		}
		return safeDiagnosticCode(native.Code), fields, false
	}
	var output *deliveryError
	if errors.As(err, &output) {
		var fields []diagnostic.Field
		if output.candidateID > 0 {
			fields = append(fields, diagnostic.Int("candidate_id", output.candidateID))
		}
		if output.published {
			fields = append(
				fields, diagnostic.Bool("published", true),
				diagnostic.String("output", output.destination),
			)
		}
		if output.delivered {
			fields = append(fields, diagnostic.Bool("delivered", true))
		}
		if output.remaining != "" {
			fields = append(fields, diagnostic.String("temporary_directory", output.remaining))
		}
		return safeDiagnosticCode(output.code), fields, output.omitOutput
	}
	var validation *validator.Error
	if errors.As(err, &validation) {
		return safeDiagnosticCode(validation.Code), nil, false
	}
	var runtime *app.Error
	if errors.As(err, &runtime) {
		return safeDiagnosticCode(runtime.Code), nil, false
	}
	switch {
	case errors.Is(err, config.ErrInvalid):
		return "configuration_invalid", nil, false
	case errors.Is(err, config.ErrAccess):
		return "configuration_access", nil, false
	case errors.Is(err, config.ErrScheduleDisabled):
		return "schedule_disabled", nil, false
	case errors.Is(err, config.ErrSelectionInvalid):
		return "source_selection_invalid", nil, false
	case errors.Is(err, config.ErrSelectionEmpty):
		return "source_selection_empty", nil, false
	case errors.Is(err, context.Canceled):
		return "cancelled", nil, false
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded", nil, false
	}
	if Diagnostic(err) != "command or output failed" {
		return "arguments_invalid", nil, false
	}
	return "command_failed", nil, false
}
func safeDiagnosticCode(code string) string {
	if diagnostic.SafeCode(code) {
		return code
	}
	return "command_failed"
}
