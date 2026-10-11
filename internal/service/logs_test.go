package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Spec 0008 AC-12: five bounded files survive rotation and a partial crash tail.
func TestLogRotationAndTailRecovery(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	a, err := openArchive(dir, strings.Repeat("a", 32), strings.Repeat("b", 32))
	require.NoError(t, err)
	for i := 0; i < 80000; i++ {
		require.NoError(t, a.append(Event{Type: "runner_phase", Status: "running"}))
	}
	require.NoError(t, a.close())
	files, err := os.ReadDir(filepath.Join(dir, "logs"))
	require.NoError(t, err)
	require.Len(t, files, 5)
	var size int64
	for _, entry := range files {
		info, err := entry.Info()
		require.NoError(t, err)
		require.LessOrEqual(t, info.Size(), int64(logLimit))
		size += info.Size()
	}
	require.LessOrEqual(t, size, int64(20*1024*1024))
	view, err := ReadLogs(t.Context(), Account{Root: dir}, 3)
	require.NoError(t, err)
	require.Equal(t, 3, view.Returned)
	require.True(t, view.OlderAvailable)
	require.Equal(t, int64(80000), view.Entries[2].Sequence)
	path := filepath.Join(dir, "logs", "current.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(`{"truncated":`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	view, err = ReadLogs(t.Context(), Account{Root: dir}, 3)
	require.NoError(t, err)
	require.True(t, view.TruncatedTail)
	a, err = openArchive(dir, strings.Repeat("a", 32), strings.Repeat("c", 32))
	require.NoError(t, err)
	require.NoError(t, a.append(Event{Type: "runner_phase", Status: "starting"}))
	require.NoError(t, a.close())
	view, err = ReadLogs(t.Context(), Account{Root: dir}, 3)
	require.NoError(t, err)
	require.False(t, view.TruncatedTail)
	require.True(t, view.ContinuityUnknown)
	require.Equal(t, int64(1), view.Entries[2].Sequence)
}

// Spec 0008 AC-12 and AC-14: complete invalid data and duplicates are never repaired.
func TestLogRejectsCompleteMalformedRecordAndDuplicate(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0700))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "logs"), 0700))
		e := Entry{
			SchemaVersion: 1, InstallationID: strings.Repeat("a", 32), InvocationID: strings.Repeat("b", 32),
			Sequence: 1, At: time.Now().UTC().Truncate(time.Microsecond), Stream: "diagnostic",
			Event: Event{Type: "runner_phase", Status: "running"},
		}
		data, err := json.Marshal(e)
		require.NoError(t, err)
		data = append(data, '\n')
		if duplicate {
			data = append(data, data...)
		} else {
			data = append(data, []byte("{bad}\n")...)
		}
		path := filepath.Join(dir, "logs", "current.jsonl")
		require.NoError(t, os.WriteFile(path, data, 0600))
		_, err = ReadLogs(t.Context(), Account{Root: dir}, 100)
		require.Error(t, err)
		_, err = openArchive(dir, strings.Repeat("a", 32), strings.Repeat("c", 32))
		require.Error(t, err)
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, data, after)
	}
}

// Spec 0008 AC-12: following reports a retained sequence gap before later entries.
func TestLogFollowReportsRetentionBeforeResuming(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	a, err := openArchive(dir, strings.Repeat("a", 32), strings.Repeat("b", 32))
	require.NoError(t, err)
	require.NoError(t, a.append(Event{Type: "runner_phase", Status: "running"}))
	require.NoError(t, a.close())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var types []string
	err = FollowLogs(ctx, Account{Root: dir}, 100, func(envelope LogEnvelope) error {
		types = append(types, envelope.Type)
		switch envelope.Type {
		case "snapshot":
			entry := envelope.Snapshot.Entries[0]
			entry.Sequence = 5
			data, err := json.Marshal(entry)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "logs", "current.jsonl"), append(data, '\n'), 0600))
		case "gap":
			require.Equal(t, "retention", envelope.Reason)
		case "entry":
			require.Equal(t, int64(5), envelope.Entry.Sequence)
			cancel()
		}
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"snapshot", "gap", "entry"}, types)
}
