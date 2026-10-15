package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/diagnostic"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/service"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// Spec 0010 AC-13: timeout output keeps its prepared destination and invocation failure latch.
func TestTerminalTimeoutDiagnostic(t *testing.T) {
	var out, replacement bytes.Buffer
	root := NewRootCommand("dev", "")
	root.SetErr(&out)
	require.NoError(t, root.PersistentFlags().Set("log-format", "json"))
	require.NoError(t, root.PersistentFlags().Set("log-level", "error"))
	report := PrepareTerminalTimeout(root)
	// A watchdog uses immutable presentation, even if command state later changes.
	root.SetErr(&replacement)
	require.NoError(t, root.PersistentFlags().Set("log-format", "pretty"))
	report()
	RenderFailure(root, errors.New("private late failure"), 130)
	report()
	require.Equal(t, 1, strings.Count(out.String(), `"code":"ui_shutdown_timeout"`))
	require.Contains(t, out.String(), `"message":"Terminal cleanup timed out; exiting"`)
	require.NotContains(t, out.String(), "private")
	require.Empty(t, replacement.String())
}

// Spec 0009 AC-6 and spec 0010 AC-13: failed timeout destinations are never replaced or retried.
func TestTerminalTimeoutDoesNotRetryFailedSink(t *testing.T) {
	for _, first := range []func([]byte) (int, error){
		func([]byte) (int, error) { return 0, io.ErrClosedPipe },
		func(p []byte) (int, error) { return len(p) - 1, nil },
		func(p []byte) (int, error) { return len(p), errors.New("private destination failure") },
	} {
		writer := &metadataWriter{first: first}
		root := NewRootCommand("dev", "")
		root.SetErr(writer)
		report := PrepareTerminalTimeout(root)
		require.Error(t, invocation(root).sink.Notice([]byte("prior diagnostic")))
		report()
		RenderFailure(root, errors.New("private late failure"), 0)
		require.Equal(t, 1, writer.writes)
	}
}

func TestTerminalTimeoutAndNormalFinalOutputShareOneClaim(t *testing.T) {
	root := NewRootCommand("dev", "")
	var out bytes.Buffer
	root.SetErr(&out)
	require.NoError(t, root.PersistentFlags().Set("log-format", "json"))
	report := PrepareTerminalTimeout(root)
	var work sync.WaitGroup
	for range 10 {
		work.Go(report)
		work.Go(func() { RenderFailure(root, errors.New("private late failure"), 0) })
	}
	work.Wait()
	require.Equal(t, 1, strings.Count(out.String(), `"event":"command_error"`))
	require.NotContains(t, out.String(), "private")
}

// Spec 0009 AC-5 and AC-6: flags precede configuration access and invalid values stay private.
func TestPresentationValidationAndSafeFinalErrors(t *testing.T) {
	for _, args := range [][]string{
		{"crawl", "--log-level", "credential-secret"},
		{"crawl", "--log-format", "credential-secret"},
		{"crawl", "--color", "credential-secret"},
	} {
		root := NewRootCommand("dev", "")
		var out, errout bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errout)
		dir := filepath.Join(t.TempDir(), "state")
		root.SetArgs(append(args, "--state-dir", dir, "--config", filepath.Join(t.TempDir(), "missing")))
		err := root.ExecuteContext(t.Context())
		require.ErrorIs(t, err, errArguments)
		RenderFailure(root, err, 0)
		require.Contains(t, errout.String(), "code=arguments_invalid")
		require.NotContains(t, errout.String(), "credential-secret")
		require.Empty(t, out.String())
		_, statErr := os.Stat(dir)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	}
	root := NewRootCommand("dev", "")
	var out bytes.Buffer
	root.SetErr(&out)
	root.SetArgs([]string{"--log-format", "json", "--unknown", "credential-secret"})
	err := root.ExecuteContext(t.Context())
	require.Error(t, err)
	RenderFailure(root, err, 0)
	require.Contains(t, out.String(), `"event":"command_error"`)
	require.Contains(t, out.String(), "unknown flag, see command help")
	require.NotContains(t, out.String(), "credential-secret")
}

// Spec 0009 AC-6: preserve typed publication and native mutation evidence, never arbitrary errors.
func TestFinalDiagnosticDetails(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   string
		fields []string
	}{
		{&deliveryError{code: "output_error", published: true, destination: "/tmp/result", candidateID: 42},
			"output_error", []string{`"published":true`, `"output":"/tmp/result"`, `"candidate_id":42`}},
		{&service.Error{Code: "service_operation_incomplete", Changed: true},
			"service_operation_incomplete", []string{`"changed":true`, `"operation_incomplete":true`}},
		{errors.New("secret credential upstream"), "command_failed", []string{"Command or output failed"}},
	} {
		var out bytes.Buffer
		root := NewRootCommand("dev", "")
		root.SetErr(&out)
		require.NoError(t, root.PersistentFlags().Set("log-format", "json"))
		RenderFailure(root, tc.err, 0)
		require.Contains(t, out.String(), `"code":"`+tc.code+`"`)
		for _, field := range tc.fields {
			require.Contains(t, out.String(), field)
		}
		require.NotContains(t, out.String(), "secret credential")
	}
}

func displayFixture(format, level string, out *bytes.Buffer) *serviceDisplay {
	return &serviceDisplay{sink: diagnostic.NewSink(out, diagnostic.Options{Level: level, Format: format, Color: "never"},
		diagnostic.Capabilities{}, time.UTC, nil), writer: out, json: format == "json"}
}
func logFixture() service.LogView {
	at := time.Date(2026, 10, 13, 3, 4, 5, 0, time.UTC)
	events := []service.Event{
		{Type: "lane_phase", Lane: "crawl", Status: "idle"},
		{Type: "work_diagnostic", RunID: 9007199254740993, Code: "temporary_error"},
		{Type: "crawl_result", RunID: 9007199254740994, Origin: "scheduled", Status: "completed",
			Counts: map[string]int64{"saved_candidates": 2, "saved_sightings": 3}},
	}
	v := service.LogView{SchemaVersion: 1, View: "logs", RequestedLines: 3, Returned: 3,
		OlderAvailable: true, TruncatedTail: true, ContinuityUnknown: true}
	for i, event := range events {
		v.Entries = append(v.Entries, service.Entry{
			SchemaVersion: 1, InstallationID: strings.Repeat("a", 32), InvocationID: strings.Repeat("b", 32),
			Sequence: int64(i + 1), At: at, Stream: "diagnostic", Event: event})
	}
	v.Entries[2].Stream = "result"
	return v
}

// Spec 0009 AC-7 and AC-8: filtering follows selection, text indices remain original, JSON is untouched.
func TestServiceDisplaySelectionAndContinuity(t *testing.T) {
	for _, format := range []string{"pretty", "text", "json"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			v := logFixture()
			display := displayFixture(format, "warn", &out)
			require.NoError(t, display.snapshot(v, ""))
			text := out.String()
			if format == "json" {
				var expected bytes.Buffer
				require.NoError(t, deliverService(&expected, "json", v))
				require.Equal(t, expected.String(), text)
				require.NotContains(t, text, "displayed")
			} else {
				require.Contains(t, text, "returned=3")
				require.Contains(t, text, "displayed=1")
				require.Contains(t, text, "filtered=2")
				require.Contains(t, text, "continuity_unknown=true")
				require.Contains(t, text, "older_available=true")
				require.Contains(t, text, "truncated_tail=true")
				require.NotContains(t, text, "saved_candidates")
				require.Contains(t, text, "9007199254740993")
				if format == "text" {
					require.Contains(t, text, "entries[1].event.code=temporary_error")
					require.NotContains(t, text, "entries[0]")
					require.NotContains(t, text, "entries[2]")
				} else {
					require.Contains(t, text, "Date 2026-10-13 | Zone UTC | UTC+00:00")
					require.Contains(t, text, "03:04:05.000 WARN")
					require.Contains(t, text, "Archive continuity is unknown")
				}
			}
			out.Reset()
			require.NoError(t, display.envelope(service.LogEnvelope{SchemaVersion: 1, Type: "gap", Reason: "retention"}))
			require.NotEmpty(t, out.String())
			require.NotContains(t, out.String(), "Date ")
		})
	}
	var out bytes.Buffer
	d := displayFixture("pretty", "error", &out)
	require.NoError(t, d.snapshot(logFixture(), ""))
	require.Contains(t, out.String(), "No entries at this level")
	require.Contains(t, out.String(), "returned=3 displayed=0 filtered=3")
}

// Spec 0009 AC-8: saved status takes precedence, a lane uses only its own reason.
func TestServiceEventSeverity(t *testing.T) {
	for _, tc := range []struct {
		event service.Event
		want  zerolog.Level
	}{
		{service.Event{Type: "runner_phase", Status: "stopped", Code: "failed"}, zerolog.InfoLevel},
		{service.Event{Type: "runner_phase", Status: "clock_paused"}, zerolog.WarnLevel},
		{service.Event{Type: "lane_phase", Lane: "validation", Status: "idle"}, zerolog.DebugLevel},
		{service.Event{Type: "lane_phase", Lane: "validation", Status: "idle", Code: "failed"}, zerolog.ErrorLevel},
		{service.Event{Type: "source_result", Status: "skipped"}, zerolog.WarnLevel},
		{service.Event{Type: "crawl_result", Status: "cancelled", Code: "failed"}, zerolog.WarnLevel},
		{service.Event{Type: "validation_result", Status: "completed", Counts: map[string]int64{"invalid": 5}},
			zerolog.InfoLevel},
		{service.Event{Type: "validation_result", Status: "completed", Counts: map[string]int64{"inconclusive": 1}},
			zerolog.WarnLevel},
		{service.Event{Type: "validation_result", Status: "completed",
			Counts: map[string]int64{"inconclusive": 1 << 62, "unresolved": 1 << 62}}, zerolog.WarnLevel},
	} {
		require.Equal(t, tc.want, serviceRecord(service.Entry{Event: tc.event}).Level)
	}
}

// Spec 0009 AC-5: help and version do not evaluate unused runtime presentation settings.
func TestHelpAndVersionIgnoreUnusedPresentation(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}, {"service"}} {
		root := NewRootCommand("dev", "")
		var out, diagnostics bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&diagnostics)
		root.SetArgs(append(args, "--log-level", "unused-invalid-value"))
		require.NoError(t, root.ExecuteContext(t.Context()))
		require.NotEmpty(t, out.String())
		require.Empty(t, diagnostics.String())
	}
}
