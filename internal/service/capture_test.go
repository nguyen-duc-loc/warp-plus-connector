package service

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/diagnostic"
	"github.com/stretchr/testify/require"
)

// Spec 0009 AC-11: old archives and new capture share exactly the same saved event shape.
func TestZerologCapturePreservesArchiveContract(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	archive, err := openArchive(dir, strings.Repeat("a", 32), strings.Repeat("b", 32))
	require.NoError(t, err)
	require.NoError(t, archive.append(Event{Type: "work_diagnostic", RunID: 1, Code: "completed"}))
	capture := diagnostic.NewCapture(func(p []byte) error { return captureDiagnostic(p, archive.append) }, nil)
	logger := diagnostic.NewLogger(capture, func() time.Time { return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) })
	diagnostic.Emit(logger, "crawl_started", diagnostic.Int("run_id", 2), diagnostic.String("origin", "manual"))
	diagnostic.Emit(logger, "work_diagnostic", diagnostic.Int("run_id", 9007199254740993),
		diagnostic.String("code", "completed"), diagnostic.String("origin", "manual"),
		diagnostic.Int("saved_candidates", 20))
	require.NoError(t, capture.Err())
	require.NoError(t, archive.close())
	view, err := ReadLogs(t.Context(), Account{Root: dir}, 10)
	require.NoError(t, err)
	require.Equal(t, 2, view.Returned)
	require.Equal(t, Event{Type: "work_diagnostic", RunID: 9007199254740993, Code: "completed"}, view.Entries[1].Event)
	require.Equal(t, int64(2), view.Entries[1].Sequence)
	require.Equal(t, strings.Repeat("b", 32), view.Entries[1].InvocationID)
	require.Greater(t, view.Entries[1].At.Year(), 2000)
}

// Spec 0009 AC-10 and AC-11: unknown event or malformed work capture fails before archive writes.
func TestZerologCaptureRejectsUnknownAndMalformed(t *testing.T) {
	for _, event := range []string{"unknown_event", "work_diagnostic"} {
		var transport bytes.Buffer
		logger := diagnostic.NewLogger(&transport, time.Now)
		diagnostic.Emit(logger, event, diagnostic.String("code", "completed"))
		calls := 0
		err := captureDiagnostic(transport.Bytes(), func(Event) error { calls++; return nil })
		require.Error(t, err)
		require.Zero(t, calls)
	}
}
