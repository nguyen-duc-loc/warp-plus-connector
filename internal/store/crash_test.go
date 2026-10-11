package store

import (
	"bufio"
	"database/sql/driver"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
	"modernc.org/sqlite"
)

var crashTime = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func pauseChild() {
	fmt.Println("ready")
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
}

func TestCrashWriterHelper(t *testing.T) {
	mode := os.Getenv("WPC_CRASH_MODE")
	if mode == "" {
		return
	}
	dir := os.Getenv("WPC_CRASH_STATE")
	if mode == "before_commit" {
		sqlite.MustRegisterScalarFunction(
			"pause_fixture", 0, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
				pauseChild()
				return int64(0), nil
			},
		)
	}
	if mode == "initialization" {
		require.NoError(t, os.MkdirAll(dir, 0700))
		f, err := privateFile(filepath.Join(dir, "state.db"), true)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		db, err := openDB(t.Context(), dir, false)
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), "PRAGMA journal_mode=WAL")
		require.NoError(t, err)
		tx, err := db.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), initialSchema)
		require.NoError(t, err)
		pauseChild()
		return
	}
	w, err := OpenWriter(t.Context(), dir, func() time.Time { return crashTime })
	require.NoError(t, err)
	crawl, err := w.StartRun(t.Context(), state.RunInput{RequestID: "crawl", Kind: state.Crawl, StartedAt: crashTime})
	require.NoError(t, err)
	saved, err := w.RecordMessage(
		t.Context(),
		state.MessageInput{
			RunID: crawl.ID, SourceID: "fixture", MessageID: "saved", ObservedAt: crashTime, Keys: []string{"saved"},
		},
	)
	require.NoError(t, err)
	validation, err := w.StartRun(
		t.Context(),
		state.RunInput{RequestID: "validation", Kind: state.Validation, StartedAt: crashTime},
	)
	require.NoError(t, err)
	_, err = w.BeginAttempt(
		t.Context(),
		state.AttemptInput{
			RequestID: "unresolved", RunID: validation.ID, CandidateID: saved.Discoveries[0].CandidateID,
			BackendID: "fixture", BackendVersion: "dev", StartedAt: crashTime,
		},
	)
	require.NoError(t, err)
	if mode == "before_commit" {
		_, err = w.h.writeDB.ExecContext(
			t.Context(), `CREATE TRIGGER pause_fixture BEFORE INSERT ON candidates WHEN NEW.key_text='partial_z'
   BEGIN SELECT pause_fixture(); END`,
		)
		require.NoError(t, err)
	}
	_, err = w.RecordMessage(
		t.Context(),
		state.MessageInput{
			RunID: crawl.ID, SourceID: "fixture", MessageID: "interrupted", ObservedAt: crashTime,
			Keys: []string{"partial_a", "partial_z"},
		},
	)
	require.NoError(t, err)
	pauseChild()
}

// covers: AC-6, AC-7, AC-8, AC-11, AC-12.
func TestProcessCrashAtomicityAndRecovery(t *testing.T) {
	for _, mode := range []string{"before_commit", "after_commit", "initialization"} {
		t.Run(
			mode, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "state")
				cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCrashWriterHelper$")
				cmd.Env = append(os.Environ(), "WPC_CRASH_MODE="+mode, "WPC_CRASH_STATE="+dir)
				stdout, err := cmd.StdoutPipe()
				require.NoError(t, err)
				stdin, err := cmd.StdinPipe()
				require.NoError(t, err)
				cmd.Stderr = os.Stderr
				require.NoError(t, cmd.Start())
				t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill() })
				ready := make(chan bool, 1)
				go func() { scan := bufio.NewScanner(stdout); ready <- scan.Scan() && scan.Text() == "ready" }()
				select {
				case ok := <-ready:
					require.True(t, ok, "child did not reach synchronization point")
				case <-time.After(10 * time.Second):
					t.Fatal("child did not reach synchronization point")
				}
				if mode != "initialization" {
					other, err := OpenWriter(t.Context(), dir, time.Now)
					require.ErrorIs(t, err, ErrBusy)
					require.Nil(t, other)
					reader, err := OpenReader(t.Context(), dir)
					require.NoError(t, err)
					page, err := reader.ListCandidates(
						t.Context(),
						state.PageRequest{},
						state.Evaluation{AsOf: crashTime, MaxAge: time.Hour},
					)
					require.NoError(t, err)
					count := 1
					if mode == "after_commit" {
						count = 3
					}
					require.Len(t, page.Items, count)
					require.NoError(t, reader.Close())
				}
				require.NoError(t, cmd.Process.Kill())
				require.Error(t, cmd.Wait())
				recovered, err := OpenWriter(t.Context(), dir, func() time.Time { return crashTime.Add(time.Hour) })
				require.NoError(t, err)
				defer func() { require.NoError(t, recovered.Close()) }()
				candidates, err := recovered.ListCandidates(
					t.Context(),
					state.PageRequest{},
					state.Evaluation{AsOf: crashTime, MaxAge: time.Hour},
				)
				require.NoError(t, err)
				switch mode {
				case "initialization":
					require.Empty(t, candidates.Items)
				case "before_commit":
					require.Len(t, candidates.Items, 1)
					require.Equal(t, "saved", candidates.Items[0].Key)
					require.Equal(t, state.Inconclusive, candidates.Items[0].LatestOutcome)
					require.Nil(t, candidates.Items[0].LatestAttempt.FinishedAt)
				case "after_commit":
					require.Len(t, candidates.Items, 3)
					replay, err := recovered.RecordMessage(
						t.Context(),
						state.MessageInput{
							RunID: 1, SourceID: "fixture", MessageID: "interrupted", ObservedAt: crashTime,
							Keys: []string{"partial_a", "partial_z"},
						},
					)
					require.NoError(t, err)
					require.Zero(t, replay.NewCandidates)
					require.Zero(t, replay.NewObservations)
				}
			},
		)
	}
}
