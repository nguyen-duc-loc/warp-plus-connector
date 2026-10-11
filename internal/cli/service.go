package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/service"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/spf13/cobra"
)

func addServiceCommands(root *cobra.Command, version string) {
	parent := &cobra.Command{
		Use: "service", Short: "Manage background operation and inspect diagnostics", Args: noArguments,
	}
	parent.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	install := &cobra.Command{Use: "install", Short: "Enable a user service for the next login", Args: noArguments}
	install.Flags().String("config", "", "Existing private configuration file (required)")
	install.RunE = func(cmd *cobra.Command, _ []string) error {
		path, _ := cmd.Flags().GetString("config")
		if path == "" {
			return errArguments
		}
		result, err := service.Install(cmd.Context(), path)
		if err != nil {
			return err
		}
		return renderControl(cmd.OutOrStdout(), result)
	}
	parent.AddCommand(install)
	for _, action := range []string{"start", "stop", "restart", "enable", "disable", "uninstall"} {
		cmd := &cobra.Command{Use: action, Short: "Apply the " + action + " service action", Args: noArguments}
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			result, err := service.Control(cmd.Context(), cmd.Name())
			if err != nil {
				return err
			}
			return renderControl(cmd.OutOrStdout(), result)
		}
		parent.AddCommand(cmd)
	}
	for _, action := range []string{"status", "doctor", "logs"} {
		cmd := &cobra.Command{Use: action, Short: "Inspect service " + action, Args: noArguments}
		cmd.Flags().String("format", "text", "Output format: text or json")
		if action == "logs" {
			cmd.Flags().Int("lines", 100, "Recent entries (1 through 10000)")
			cmd.Flags().Bool("follow", false, "Follow later entries and report archive gaps")
		}
		cmd.RunE = serviceRead
		parent.AddCommand(cmd)
	}
	execute := &cobra.Command{Use: "exec", Hidden: true, Args: noArguments}
	execute.Flags().String("installation-id", "", "Managed installation identity")
	execute.RunE = func(cmd *cobra.Command, _ []string) error {
		id, _ := cmd.Flags().GetString("installation-id")
		return service.RunManaged(cmd.Context(), id, version)
	}
	parent.AddCommand(execute)
	root.AddCommand(parent)
}

func renderControl(w io.Writer, r service.ControlResult) error {
	_, err := fmt.Fprintf(
		w,
		"action=%s changed=%t installation_id=%s state=%s\n",
		r.Action,
		r.Changed,
		r.InstallationID,
		r.State,
	)
	if err != nil {
		return &controlDeliveryError{result: r}
	}
	return nil
}

type controlDeliveryError struct{ result service.ControlResult }

func (e *controlDeliveryError) Error() string {
	return fmt.Sprintf("output_error action=%s changed=%t installation_id=%s state=%s",
		e.result.Action, e.result.Changed, e.result.InstallationID, e.result.State)
}

type doctorFailed struct{}

func (*doctorFailed) Error() string          { return "service_doctor_failed" }
func (*doctorFailed) DiagnosticFailed() bool { return true }

func serviceRead(cmd *cobra.Command, _ []string) error {
	format, _ := cmd.Flags().GetString("format")
	if format != "text" && format != "json" {
		return errArguments
	}
	lines, follow := 100, false
	if cmd.Name() == "logs" {
		lines, _ = cmd.Flags().GetInt("lines")
		follow, _ = cmd.Flags().GetBool("follow")
		if lines < 1 || lines > 10000 {
			return errArguments
		}
	}
	if cmd.Name() == "doctor" {
		report := service.Doctor(cmd.Context())
		if err := deliverService(cmd.OutOrStdout(), format, report); err != nil {
			return err
		}
		if !report.OK {
			return &doctorFailed{}
		}
		return nil
	}
	a, err := service.ResolveAccount(cmd.Context())
	if err != nil {
		return err
	}
	if cmd.Name() == "status" {
		report := service.Status(cmd.Context(), a)
		if work, ok := report.Work.Data.(state.OperationalWork); ok {
			report.Work.Data = workObject(work)
		}
		return deliverService(cmd.OutOrStdout(), format, report)
	}
	if follow {
		return service.FollowLogs(
			cmd.Context(), a, lines, func(e service.LogEnvelope) error {
				return deliverService(cmd.OutOrStdout(), format, e)
			},
		)
	}
	view, err := service.ReadLogs(cmd.Context(), a, lines)
	if err != nil {
		return err
	}
	return deliverService(cmd.OutOrStdout(), format, view)
}

func deliverService(w io.Writer, format string, object any) error {
	var prepared bytes.Buffer
	if format == "json" {
		if err := json.NewEncoder(&prepared).Encode(object); err != nil {
			return &app.Error{Code: "output_error"}
		}
	} else {
		data, err := json.Marshal(object)
		if err != nil {
			return &app.Error{Code: "output_error"}
		}
		var value any
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		if err := d.Decode(&value); err != nil {
			return &app.Error{Code: "output_error"}
		}
		if err := renderDiagnosticText(&prepared, "", value); err != nil {
			return &app.Error{Code: "output_error"}
		}
	}
	n, err := w.Write(prepared.Bytes())
	if err != nil || n != prepared.Len() {
		return &app.Error{Code: "output_error"}
	}
	return nil
}

func renderDiagnosticText(w io.Writer, prefix string, value any) error {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			p := key
			if prefix != "" {
				p = prefix + "." + key
			}
			if err := renderDiagnosticText(w, p, v[key]); err != nil {
				return err
			}
		}
		return nil
	case []any:
		if len(v) == 0 {
			_, err := fmt.Fprintf(w, "%s=none\n", prefix)
			return err
		}
		for i, item := range v {
			if err := renderDiagnosticText(w, prefix+"["+strconv.Itoa(i)+"]", item); err != nil {
				return err
			}
		}
		return nil
	case nil:
		_, err := fmt.Fprintf(w, "%s=unknown\n", prefix)
		return err
	default:
		_, err := fmt.Fprintf(w, "%s=%v\n", prefix, v)
		return err
	}
}

func workObject(w state.OperationalWork) map[string]any {
	schedule := scheduleObject(w.Schedule)
	delete(schedule, "schema_version")
	delete(schedule, "view")
	return map[string]any{
		"as_of": scheduleTime(w.AsOf), "schedule": schedule, "outcomes": w.Outcomes,
		"latest_crawl": runObject(w.LatestCrawl), "last_terminal_crawl": runObject(w.LastTerminalCrawl),
		"latest_validation":        runObject(w.LatestValidation),
		"last_terminal_validation": runObject(w.LastTerminalValidation),
	}
}

func runObject(r *state.OperationalRun) any {
	if r == nil {
		return nil
	}
	out := map[string]any{
		"run_id": r.Run.ID, "origin": r.Run.Origin, "status": r.Run.Status,
		"started_at": scheduleTime(r.Run.StartedAt), "finished_at": optionalScheduleTime(r.Run.FinishedAt),
		"recovered_at": optionalScheduleTime(r.Run.RecoveredAt), "reason_code": r.Run.ReasonCode,
		"clock_anomaly": r.Run.ClockAnomaly,
	}
	if r.Run.Kind == state.Crawl {
		out["saved_candidates"], out["saved_sightings"], out["sources"] = r.SavedCandidates, r.SavedSightings, r.Sources
	} else {
		out["totals"] = nil
		if r.Totals != nil {
			out["totals"] = service.ValidationCounts(*r.Totals)
		}
		out["failures"] = r.Failures
	}
	return out
}
