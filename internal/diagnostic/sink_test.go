package diagnostic_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/diagnostic"
	"github.com/stretchr/testify/require"
)

// Spec 0009 AC-2 and AC-5: each original destination controls format and styling.
func TestDestinationPresentation(t *testing.T) {
	for _, tc := range []struct {
		name, format, color, term, noColor, want string
		terminal, styled                         bool
	}{
		{"terminal", "auto", "auto", "xterm", "", "pretty", true, true},
		{"redirected", "auto", "auto", "xterm", "", "text", false, false},
		{"dumb", "auto", "auto", "dumb", "", "text", true, false},
		{"explicit pretty", "pretty", "auto", "xterm", "", "pretty", false, false},
		{"no color", "pretty", "auto", "xterm", "1", "pretty", true, false},
		{"never", "pretty", "never", "xterm", "", "pretty", true, false},
		{"text", "text", "auto", "xterm", "", "text", true, false},
		{"json", "json", "auto", "xterm", "", "json", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := diagnostic.Options{Level: "info", Format: tc.format, Color: tc.color}
			format, color := o.Resolve(diagnostic.Capabilities{Terminal: tc.terminal, Term: tc.term, NoColor: tc.noColor})
			require.Equal(t, tc.want, format)
			require.Equal(t, tc.styled, color)
		})
	}
	require.False(t, diagnostic.Detect(&bytes.Buffer{}).Terminal)
}

// Spec 0009 AC-3 and AC-4: headers follow event time, including backward offset changes.
func TestPrettyClockContextAndExactIDs(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	times := []string{"2026-11-01T05:59:59.123456Z", "2026-11-01T06:00:00.000001Z",
		"2026-11-01T05:59:58.000001Z", "2026-11-02T05:00:00.000001Z"}
	var out bytes.Buffer
	sink := diagnostic.NewSink(&out, diagnostic.Options{Level: "info", Format: "pretty", Color: "never"},
		diagnostic.Capabilities{}, location, nil)
	i := 0
	logger := diagnostic.NewLogger(sink, func() time.Time {
		at, e := time.Parse(time.RFC3339Nano, times[i])
		require.NoError(t, e)
		i++
		return at
	})
	for range times {
		diagnostic.Emit(logger, "crawl_started", diagnostic.Int("run_id", 9007199254740993),
			diagnostic.String("origin", "manual"))
	}
	require.NoError(t, sink.Err())
	text := out.String()
	require.Equal(t, 4, strings.Count(text, "Date "))
	require.Contains(t, text, "Date 2026-11-01 | Zone America/New_York | UTC-04:00")
	require.Contains(t, text, "Date 2026-11-01 | Zone America/New_York | UTC-05:00")
	require.Contains(t, text, "Date 2026-11-02 | Zone America/New_York | UTC-05:00")
	require.Less(t, strings.Index(text, "01:00:00.000 INFO"), strings.Index(text, "01:59:58.000 INFO"))
	require.Equal(t, 4, strings.Count(text, "run_id=9007199254740993"))
}

// Spec 0009 AC-4, AC-6, AC-9: exact integers, escaped paths, and unfiltered cancellation.
func TestFormatsPrivacyAndCancellation(t *testing.T) {
	for _, format := range []string{"pretty", "text", "json"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			sink := diagnostic.NewSink(&out, diagnostic.Options{Level: "error", Format: format, Color: "never"},
				diagnostic.Capabilities{}, time.UTC, nil)
			logger := diagnostic.NewLogger(sink, func() time.Time {
				return time.Date(2026, 10, 14, 0, 0, 0, 123456000, time.UTC)
			})
			diagnostic.Emit(logger, "crawl_started", diagnostic.Int("run_id", 1), diagnostic.String("origin", "manual"))
			diagnostic.CommandError(logger, "output_error", "untrusted error text", false,
				diagnostic.Int("candidate_id", 9007199254740993), diagnostic.Bool("published", true),
				diagnostic.String("output", "/tmp/a=\"b\\c\n\t\x1b[31m"))
			diagnostic.Emit(logger, "command_cancelled", diagnostic.Int("exit_code", 143))
			require.NoError(t, sink.Err())
			text := out.String()
			require.NotContains(t, text, "untrusted error text")
			require.NotContains(t, text, "Crawl started")
			require.NotContains(t, text, "\x1b")
			require.Contains(t, text, "9007199254740993")
			require.Contains(t, text, "Command cancelled")
			if format == "json" {
				lines := strings.Split(strings.TrimSpace(text), "\n")
				require.Len(t, lines, 2)
				var event map[string]any
				d := json.NewDecoder(strings.NewReader(lines[0]))
				d.UseNumber()
				require.NoError(t, d.Decode(&event))
				require.Equal(t, json.Number("9007199254740993"), event["candidate_id"])
				require.Equal(t, "2026-10-14T00:00:00.123456Z", event["time"])
				require.Equal(t, true, event["published"])
			} else {
				require.Contains(t, text, `\n\t\x1b[31m`)
				if format == "text" {
					require.Contains(t, text, "time=2026-10-14T00:00:00.123+00:00 level=ERROR")
				}
			}
		})
	}
}

type shortWriter struct{ calls atomic.Int32 }

func (w *shortWriter) Write(p []byte) (int, error) { w.calls.Add(1); return len(p) - 1, nil }

// Spec 0009 AC-10: notify once after unlocking, contain fallback output, never retry.
func TestSinkFailureLatch(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "short write", true: "format error"}[invalid],
			func(t *testing.T) {
				w := &shortWriter{}
				var cancels atomic.Int32
				var sink *diagnostic.Sink
				sink = diagnostic.NewSink(w, diagnostic.Options{Level: "info", Format: "text", Color: "never"},
					diagnostic.Capabilities{}, time.UTC, func() { require.Error(t, sink.Err()); cancels.Add(1) })
				logger := diagnostic.NewLogger(sink, time.Now)
				event := "crawl_started"
				if invalid {
					event = "unknown_event"
				}
				diagnostic.Emit(logger, event, diagnostic.Int("run_id", 1), diagnostic.String("origin", "manual"))
				diagnostic.Emit(logger, "crawl_started", diagnostic.Int("run_id", 2), diagnostic.String("origin", "manual"))
				require.Equal(t, int32(1), cancels.Load())
				require.Error(t, sink.Err())
				if invalid {
					require.Zero(t, w.calls.Load())
				} else {
					require.ErrorIs(t, sink.Err(), io.ErrShortWrite)
				}
				require.LessOrEqual(t, w.calls.Load(), int32(1))
				n, err := sink.Write([]byte("invalid"))
				require.NoError(t, err)
				require.Equal(t, 7, n)
			})
	}
}

// Spec 0009 AC-9 and AC-10: unsupported shapes and oversize records fail before delivery.
func TestMalformedEventsDoNotReachOutput(t *testing.T) {
	for _, fields := range [][]diagnostic.Field{
		{diagnostic.String("session", "private session")},
		{{Name: "run_id", Value: float64(1)}},
		{diagnostic.String("code", "cancelled deadline_exceeded")},
		{diagnostic.String("code", strings.Repeat("secret", 4000))},
	} {
		var out bytes.Buffer
		sink := diagnostic.NewSink(&out, diagnostic.Options{Level: "error", Format: "json", Color: "never"},
			diagnostic.Capabilities{}, time.UTC, nil)
		logger := diagnostic.NewLogger(sink, time.Now)
		diagnostic.Emit(logger, "work_diagnostic", append([]diagnostic.Field{
			diagnostic.Int("run_id", 1), diagnostic.String("code", "completed")}, fields...)...)
		require.Error(t, sink.Err())
		require.Empty(t, out.String())
	}
}

// Spec 0009 AC-10: an event and its context remain indivisible under concurrent writes.
func TestConcurrentEventLines(t *testing.T) {
	var out bytes.Buffer
	sink := diagnostic.NewSink(&out, diagnostic.Options{Level: "info", Format: "pretty", Color: "never"},
		diagnostic.Capabilities{}, time.UTC, nil)
	logger := diagnostic.NewLogger(sink, func() time.Time { return time.Date(2026, 10, 14, 1, 2, 3, 0, time.UTC) })
	var group sync.WaitGroup
	for i := int64(1); i <= 100; i++ {
		group.Add(1)
		go func(id int64) {
			defer group.Done()
			diagnostic.Emit(logger, "crawl_started", diagnostic.Int("run_id", id), diagnostic.String("origin", "manual"))
		}(i)
	}
	group.Wait()
	require.NoError(t, sink.Err())
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 101)
	require.True(t, strings.HasPrefix(lines[0], "Date "))
	for _, line := range lines[1:] {
		require.True(t, strings.HasPrefix(line, "01:02:03.000 INFO"))
		require.Equal(t, 1, strings.Count(line, "run_id="))
	}
}

// A capture consumer error is retained without invoking zerolog's global error printer.
func TestCaptureConsumerFailure(t *testing.T) {
	want := errors.New("private error")
	var calls int
	capture := diagnostic.NewCapture(func([]byte) error { calls++; return want }, nil)
	logger := diagnostic.NewLogger(capture, time.Now)
	diagnostic.Emit(logger, "scheduler", diagnostic.String("code", "schedule_starting"))
	diagnostic.Emit(logger, "scheduler", diagnostic.String("code", "schedule_stopped"))
	require.ErrorIs(t, capture.Err(), want)
	require.Equal(t, 1, calls)
}
