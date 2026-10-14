package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/spf13/cobra"
)

type resultOptions struct {
	query    app.CandidateQuery
	format   string
	showKeys bool
	output   string
}

func candidateCommand(export bool) *cobra.Command {
	cmd := &cobra.Command{
		Use: "list", Short: "List every saved candidate, hiding keys by default", Args: noArguments,
	}
	if export {
		cmd.Use, cmd.Short = "export", "Export saved candidate summaries or complete key lines"
		cmd.Flags().String("format", "json", "Output format: json or keys")
		cmd.Flags().String("output", "", "Create a private file without replacement (omit for stdout)")
	}
	commonFlags(cmd)
	var status, definitive, freshness []string
	cmd.Flags().StringArrayVar(&status, "status", nil,
		"Latest outcome: unchecked, confirmed, invalid, inconclusive (repeatable, comma separated)")
	cmd.Flags().StringArrayVar(&definitive, "definitive", nil,
		"Last definitive outcome: none, confirmed, invalid (repeatable, comma separated)")
	cmd.Flags().StringArrayVar(&freshness, "freshness", nil,
		"Freshness: fresh, stale, unknown, not_applicable (repeatable, comma separated)")
	cmd.Flags().String("limit", "", "Positive maximum matching candidates (default unlimited)")
	cmd.Flags().Bool("show-keys", false, "Include complete saved keys in output")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		options := resultOptions{
			query: app.CandidateQuery{Status: status, Definitive: definitive, Freshness: freshness}, format: "text",
		}
		if cmd.Flags().Changed("limit") {
			raw, _ := cmd.Flags().GetString("limit")
			if err := oneID(cmd, []string{raw}); err != nil {
				return errArguments
			}
			n, _ := strconv.ParseInt(raw, 10, 64)
			options.query.Limit = &n
		}
		var err error
		options.query, err = options.query.Normalize()
		if err != nil {
			return errArguments
		}
		options.showKeys, _ = cmd.Flags().GetBool("show-keys")
		if export {
			options.format, _ = cmd.Flags().GetString("format")
			if options.format != "json" && options.format != "keys" {
				return errArguments
			}
			if options.format == "keys" && cmd.Flags().Changed("show-keys") {
				return errArguments
			}
			options.output, _ = cmd.Flags().GetString("output")
			if cmd.Flags().Changed("output") && (options.output == "" || options.output == "-") {
				return &deliveryError{code: "export_destination_invalid", omitOutput: options.output == "-"}
			}
		}
		settings, err := resolveSettings(cmd)
		if err != nil {
			return err
		}
		a := &app.App{Settings: settings, Clock: time.Now}
		return deliverCandidates(cmd.Context(), a, options, cmd.OutOrStdout(), cmd.ErrOrStderr())
	}
	return cmd
}

func deliverCandidates(
	ctx context.Context, a *app.App, options resultOptions, stdout, stderr io.Writer,
) (err error) {
	if err := ctx.Err(); err != nil {
		return deliveryFailure(err)
	}
	stage, err := newOutputStage(options.output, a.Settings.StateDir)
	if err != nil {
		return err
	}
	delivered, published := false, false
	defer func() {
		cleanupErr := stage.cleanup()
		if err == nil && cleanupErr != nil {
			err = &deliveryError{code: "export_cleanup_failed"}
		}
		if err != nil {
			failure := deliveryFailure(err)
			failure.delivered, failure.published = delivered, published
			if published {
				failure.destination = stage.destination
			}
			if cleanupErr != nil {
				failure.remaining = stage.directory
			}
			err = failure
		}
	}()
	writer := bufio.NewWriter(stage.file)
	renderer := candidateEncoder{writer: writer, format: options.format, showKeys: options.showKeys}
	if err := renderer.start(); err != nil {
		return &deliveryError{code: "export_prepare_failed"}
	}
	result, err := a.QueryCandidates(ctx, options.query, renderer.item)
	if err != nil {
		return err
	}
	// QueryCandidates has closed both the snapshot and reader before completion
	// metadata is appended or any bytes are delivered to an external writer.
	if err := renderer.finish(result); err != nil {
		return &deliveryError{code: "export_prepare_failed"}
	}
	if err := writer.Flush(); err != nil {
		return &deliveryError{code: "export_prepare_failed"}
	}
	if stage.destination != "" {
		published, err = stage.publish(ctx)
	} else {
		delivered, err = stage.copy(ctx, stdout)
	}
	if err != nil {
		return err
	}
	// Cleanup failures must not follow a success summary.
	if err := stage.cleanup(); err != nil {
		return &deliveryError{code: "export_cleanup_failed", remaining: stage.directory}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if options.format != "json" || published {
		limit := "none"
		if result.Query.Limit != nil {
			limit = strconv.FormatInt(*result.Query.Limit, 10)
		}
		line := fmt.Sprintf("returned_count=%d limit=%s truncated=%t", result.ReturnedCount, limit, result.Truncated)
		if published {
			line += fmt.Sprintf(" output=%q published=true", stage.destination)
		}
		if err := writeResultDiagnostic(ctx, stderr, []byte(line+"\n")); err != nil {
			return err
		}
	}
	return nil
}

func writeResultDiagnostic(ctx context.Context, w io.Writer, p []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := w.Write(p)
	failed := err != nil || n != len(p)
	if cancelled := ctx.Err(); cancelled != nil {
		failure := deliveryFailure(cancelled)
		failure.diagnosticFailed = failed
		return failure
	}
	if failed {
		return &deliveryError{code: "output_error", diagnosticFailed: true}
	}
	return nil
}

type deliveryError struct {
	code             string
	cause            error
	published        bool
	delivered        bool
	destination      string
	remaining        string
	candidateID      int64
	omitOutput       bool
	diagnosticFailed bool
}

func (e *deliveryError) Error() string { return e.code }
func (e *deliveryError) Unwrap() error { return e.cause }

// DiagnosticFailed prevents final rendering from retrying failed stderr metadata delivery.
func (e *deliveryError) DiagnosticFailed() bool { return e.diagnosticFailed }

func deliveryFailure(err error) *deliveryError {
	var own *deliveryError
	if errors.As(err, &own) {
		return own
	}
	code := Diagnostic(err)
	if errors.Is(err, context.Canceled) {
		code = "cancelled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = "deadline_exceeded"
	}
	return &deliveryError{code: code, cause: err}
}

func (e *deliveryError) diagnostic() string {
	message := e.code
	if e.candidateID != 0 {
		message += fmt.Sprintf(" candidate_id=%d", e.candidateID)
	}
	if e.published {
		message += fmt.Sprintf(" output=%q published=true", e.destination)
	}
	if e.delivered {
		message += " delivered=true"
	}
	if e.remaining != "" {
		message += fmt.Sprintf(" temporary_directory=%q", e.remaining)
	}
	if e.omitOutput {
		message += ", omit --output for stdout"
	}
	return message
}
