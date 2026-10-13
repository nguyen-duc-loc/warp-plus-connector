package service

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// Spec 0008 AC-12 and AC-14: the fast path must retain strict JSON field validation.
func TestLogEntryDecodePreservesStrictSchema(t *testing.T) {
	entry := Entry{
		SchemaVersion: 1, InstallationID: strings.Repeat("a", 32), InvocationID: strings.Repeat("b", 32),
		Sequence: 1, At: time.Now().UTC().Truncate(time.Microsecond), Stream: "diagnostic",
		Event: Event{Type: "runner_phase", Status: "running"},
	}
	canonical, err := json.Marshal(entry)
	require.NoError(t, err)
	var indented bytes.Buffer
	require.NoError(t, json.Indent(&indented, canonical, "", "  "))
	for _, data := range [][]byte{canonical, indented.Bytes()} {
		var got Entry
		require.NoError(t, decodeLogEntry(data, &got))
		require.Equal(t, entry, got)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(canonical, []byte(`"schema_version":1,`), nil, 1),
		bytes.Replace(canonical, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1),
		bytes.Replace(canonical, []byte(`"schema_version":1`), []byte(`"schema_version":1,"extra":0`), 1),
		bytes.Replace(canonical, []byte(`"schema_version":1`), []byte(`"SCHEMA_VERSION":1`), 1),
		bytes.Replace(canonical, []byte(`"sequence":1`), []byte(`"sequence":null`), 1),
		bytes.Replace(canonical, []byte(`"status":"running"`), []byte(`"status":"running","status":"running"`), 1),
	} {
		var got Entry
		require.Error(t, decodeLogEntry(invalid, &got))
	}
}

// Spec 0008 AC-12: exercise the full archive under race instrumentation without
// conflating its overhead with the production read deadline, tested separately.
func TestLogRotationPreservesFullArchive(t *testing.T) {
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
	view, err := archiveSnapshot(t.Context(), dir, 3)
	require.NoError(t, err)
	require.Equal(t, 3, view.Returned)
	require.True(t, view.OlderAvailable)
	require.Equal(t, int64(80000), view.Entries[2].Sequence)
}

// Spec 0008 AC-12 and AC-14: recovery truncates only an incomplete current tail.
func TestLogTailRecovery(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	a, err := openArchive(dir, strings.Repeat("a", 32), strings.Repeat("b", 32))
	require.NoError(t, err)
	for i := 0; i < 4; i++ {
		require.NoError(t, a.append(Event{Type: "runner_phase", Status: "running"}))
	}
	require.NoError(t, a.close())
	path := filepath.Join(dir, "logs", "current.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(`{"truncated":`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	view, err := ReadLogs(t.Context(), Account{Root: dir}, 3)
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

// AC-12: rotation retries share one five second budget and never return partial data.
func TestLogReadRetriesShareBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		var deadlines []time.Time
		var readContext context.Context
		view, err := readArchiveWithSnapshot(
			t.Context(), "unused", 3, func(ctx context.Context, root string, lines int) (LogView, error) {
				require.Equal(t, "unused", root)
				require.Equal(t, 3, lines)
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				deadlines = append(deadlines, deadline)
				readContext = ctx
				time.Sleep(time.Second)
				return LogView{Returned: 1, Entries: []Entry{{Sequence: 1}}}, failure("service_log_busy")
			},
		)
		require.EqualError(t, err, "service_log_busy")
		require.Equal(t, LogView{}, view)
		require.Len(t, deadlines, 3)
		require.Equal(t, started.Add(5*time.Second), deadlines[0])
		require.Equal(t, deadlines[0], deadlines[1])
		require.Equal(t, deadlines[0], deadlines[2])
		require.Equal(t, 3*time.Second, time.Since(started))
		require.ErrorIs(t, readContext.Err(), context.Canceled)
	})
}

// AC-12: a completed filesystem operation cannot authorize delivery after its budget.
func TestLogReadRejectsLateSnapshot(t *testing.T) {
	for _, kind := range []string{"cancelled", "earlier deadline", "read budget"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				parent, cancel := context.WithCancel(t.Context())
				if kind == "earlier deadline" {
					cancel()
					parent, cancel = context.WithTimeout(t.Context(), time.Second)
				}
				defer cancel()
				started := time.Now()
				calls := 0
				view, err := readArchiveWithSnapshot(
					parent, "unused", 3, func(ctx context.Context, _ string, _ int) (LogView, error) {
						calls++
						if kind == "cancelled" {
							cancel()
						} else if kind == "earlier deadline" {
							deadline, _ := parent.Deadline()
							actual, _ := ctx.Deadline()
							require.Equal(t, deadline, actual)
						} else if calls < 3 {
							time.Sleep(2 * time.Second)
							return LogView{}, failure("service_log_busy")
						}
						<-ctx.Done()
						return LogView{Returned: 1, Entries: []Entry{{Sequence: 1}}}, nil
					},
				)
				require.EqualError(t, err, "service_log_busy")
				require.Equal(t, LogView{}, view)
				if kind == "read budget" {
					require.Equal(t, 3, calls)
					require.Equal(t, 5*time.Second, time.Since(started))
				} else {
					require.Equal(t, 1, calls)
				}
			})
		})
	}
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
