package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/spf13/cobra"
)

func addScheduleCommands(root *cobra.Command, version string) {
	run := &cobra.Command{Use: "run", Short: "Continuously crawl and revalidate on saved schedules", Args: noArguments}
	commonFlags(run)
	validatorFlags(run)
	checkPolicyFlags(run)
	run.Flags().Bool("crawl-enabled", true, "Enable scheduled source collection")
	run.Flags().Bool("validation-enabled", true, "Enable scheduled candidate validation")
	run.Flags().String("crawl-interval", "1h", "Crawl interval (1m through 720h)")
	run.Flags().String("failure-interval", "1h", "Failed work recovery interval (1m through 720h)")
	run.RunE = func(cmd *cobra.Command, _ []string) error {
		a, err := application(cmd, version)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		output := &checkedStream{writer: cmd.OutOrStdout(), cancel: cancel}
		err = a.Run(ctx, func(s app.RunSummary) error { return renderRunSummary(output, s) })
		if output.failed() {
			return &app.Error{Code: "output_error"}
		}
		return err
	}
	schedule := &cobra.Command{Use: "schedule", Short: "Inspect saved scheduling facts"}
	show := &cobra.Command{
		Use: "show [candidate-id]", Short: "Explain saved eligibility without changing state",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return oneID(cmd, args)
		},
	}
	commonFlags(show)
	show.Flags().String("format", "text", "Output format: text or json")
	show.RunE = func(cmd *cobra.Command, args []string) (err error) {
		format, _ := cmd.Flags().GetString("format")
		if format != "text" && format != "json" {
			return errArguments
		}
		var id int64
		if len(args) > 0 {
			id, _ = strconv.ParseInt(args[0], 10, 64)
		}
		a, err := application(cmd, version)
		if err != nil {
			return err
		}
		saved, err := a.ShowSchedule(cmd.Context(), id)
		if err != nil {
			return err
		}
		stage, err := newOutputStage("", a.Settings.StateDir)
		if err != nil {
			return &app.Error{Code: "output_error"}
		}
		defer func() {
			if e := stage.cleanup(); e != nil && err == nil {
				err = &app.Error{Code: "output_error"}
			}
		}()
		object := scheduleObject(saved)
		if format == "json" {
			err = json.NewEncoder(stage.file).Encode(object)
		} else {
			err = renderScheduleText(stage.file, "", object)
		}
		if err != nil {
			return &app.Error{Code: "output_error"}
		}
		if _, err = stage.copy(cmd.Context(), cmd.OutOrStdout()); err != nil {
			return &app.Error{Code: "output_error"}
		}
		return nil
	}
	schedule.AddCommand(show)
	root.AddCommand(run, schedule)
}

func checkPolicyFlags(cmd *cobra.Command) {
	cmd.Flags().String("workers", "2", "Active checks (1 through 4)")
	cmd.Flags().String("max-attempts", "3", "Attempts per candidate (1 through 5)")
	cmd.Flags().String("mutation-interval", "1s", "Interval after registration and license commands (1s through 60s)")
	cmd.Flags().String("candidate-timeout", "15m", "Active candidate budget (1m through 60m)")
}

type checkedStream struct {
	mu     sync.Mutex
	writer io.Writer
	cancel context.CancelFunc
	err    error
}

func (w *checkedStream) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
		w.cancel()
	}
	return n, err
}
func (w *checkedStream) failed() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.err != nil }

func renderRunSummary(w io.Writer, s app.RunSummary) error {
	if r := s.Crawl; r != nil {
		_, err := fmt.Fprintf(
			w, "kind=crawl origin=%s run_id=%d status=%s reason=%s saved_candidates=%d saved_sightings=%d\n",
			r.Run.Origin, r.Run.ID, r.Run.Status, textValue(r.Run.ReasonCode), r.SavedCandidates, r.SavedSightings,
		)
		return err
	}
	r := s.Validation
	_, err := fmt.Fprintf(
		w,
		"kind=validation origin=%s run_id=%d status=%s reason=%s "+
			"selected=%d confirmed=%d invalid=%d inconclusive=%d unstarted=%d\n",
		r.Run.Origin,
		r.Run.ID,
		r.Run.Status,
		textValue(r.Run.ReasonCode),
		r.Totals.Selected,
		r.Totals.Confirmed,
		r.Totals.Invalid,
		r.Totals.Inconclusive,
		r.Totals.Unstarted,
	)
	return err
}

func scheduleTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000Z") }
func optionalScheduleTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return scheduleTime(*t)
}
func optionalScheduleID(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}
func scheduleObject(s state.ScheduleSummary) map[string]any {
	out := map[string]any{
		"schema_version": 1, "view": "summary", "as_of": scheduleTime(s.AsOf),
		"clock_regressed": s.ClockRegressed,
	}
	if c := s.Candidate; c != nil {
		out["view"] = "candidate"
		out["candidate_id"] = c.CandidateID
		out["due_at"] = scheduleTime(c.DueAt)
		out["reason"] = c.Reason
		out["basis_at"] = scheduleTime(c.BasisAt)
		out["interval"] = c.Interval.String()
		out["basis_run_id"] = optionalScheduleID(c.BasisRunID)
		out["basis_attempt_id"] = optionalScheduleID(c.BasisAttemptID)
		out["active_run_id"] = optionalScheduleID(c.ActiveRunID)
		out["eligible_by_time"] = c.EligibleByTime
		return out
	}
	out["checkpoint_at"] = scheduleTime(s.CheckpointAt)
	out["validation_recovery_pending"] = s.ValidationRecoveryPending
	out["crawl"] = nil
	out["validation_wait"] = nil
	if c := s.Crawl; c != nil {
		coverage := make([]map[string]any, 0, len(c.Coverage))
		for _, p := range c.Coverage {
			coverage = append(coverage, map[string]any{"channel": p.Channel, "page_limit": p.PageLimit})
		}
		out["crawl"] = map[string]any{
			"coverage": coverage, "due_at": scheduleTime(c.DueAt), "reason": c.Reason,
			"basis_at": scheduleTime(c.BasisAt), "interval": c.Interval.String(),
			"basis_run_id":  optionalScheduleID(c.BasisRunID),
			"active_run_id": optionalScheduleID(c.ActiveRunID),
		}
	}
	out["candidates"] = map[string]any{
		"total": s.Candidates.Total, "active": s.Candidates.Active, "due": s.Candidates.Due,
		"waiting": s.Candidates.Waiting, "next_due_at": optionalScheduleTime(s.Candidates.NextDueAt),
	}
	if v := s.ValidationWait; v != nil {
		out["validation_wait"] = map[string]any{
			"reason": v.Reason, "basis_at": scheduleTime(v.BasisAt),
			"retry_at": scheduleTime(v.RetryAt), "observed_at": scheduleTime(v.ObservedAt),
		}
	}
	return out
}
func renderScheduleText(w io.Writer, prefix string, object map[string]any) error {
	keys := make([]string, 0, len(object))
	for k := range object {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		value := object[k]
		if nested, ok := value.(map[string]any); ok {
			if err := renderScheduleText(w, prefix+k+".", nested); err != nil {
				return err
			}
			continue
		}
		text := "none"
		switch v := value.(type) {
		case nil:
		case []map[string]any:
			var entries []string
			for _, p := range v {
				entries = append(entries, fmt.Sprintf("%s:%v", p["channel"], p["page_limit"]))
			}
			text = strings.Join(entries, ",")
		default:
			text = fmt.Sprint(v)
		}
		if _, err := fmt.Fprintf(w, "%s%s=%s\n", prefix, k, text); err != nil {
			return err
		}
	}
	return nil
}

type failedDiagnostic struct{}

func (*failedDiagnostic) Error() string          { return "output_error" }
func (*failedDiagnostic) Unwrap() error          { return &app.Error{Code: "output_error"} }
func (*failedDiagnostic) DiagnosticFailed() bool { return true }
