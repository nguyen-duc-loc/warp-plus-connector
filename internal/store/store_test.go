package store_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

var fixtureTime = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func openWriter(t *testing.T) (*store.Writer, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	w, err := store.OpenWriter(t.Context(), dir, func() time.Time { return fixtureTime })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	return w, dir
}

func startRun(t *testing.T, w *store.Writer, kind state.RunKind) state.Run {
	t.Helper()
	r, err := w.StartRun(t.Context(), state.RunInput{RequestID: rand.Text(), Kind: kind, StartedAt: fixtureTime})
	require.NoError(t, err)
	return r
}

func discover(t *testing.T, w *store.Writer, run int64, keys ...string) state.MessageResult {
	t.Helper()
	result, err := w.RecordMessage(
		t.Context(),
		state.MessageInput{
			RunID: run, SourceID: "fixture_source", MessageID: "fixture_message", ObservedAt: fixtureTime, Keys: keys,
		},
	)
	require.NoError(t, err)
	return result
}

func evaluation() state.Evaluation { return state.Evaluation{AsOf: fixtureTime, MaxAge: 6 * time.Hour} }

// covers: AC-1, AC-2, AC-3.
func TestRestartThroughPublicInterface(t *testing.T) {
	if dir := os.Getenv("WPC_STORE_RESTART_FIXTURE"); dir != "" {
		r, err := store.OpenReader(context.Background(), dir)
		require.NoError(t, err)
		defer func() { require.NoError(t, r.Close()) }()
		c, err := r.GetCandidate(t.Context(), 1, evaluation())
		require.NoError(t, err)
		require.Equal(t, "synthetic_key", c.Key)
		require.Equal(t, state.Unchecked, c.LatestOutcome)
		p, err := r.ListSightings(t.Context(), c.ID, state.PageRequest{})
		require.NoError(t, err)
		require.Len(t, p.Items, 1)
		require.Nil(t, p.Items[0].PublishedAt)
		require.Equal(t, fixtureTime, p.Items[0].FirstSeenAt)
		return
	}
	w, dir := openWriter(t)
	run := startRun(t, w, state.Crawl)
	result := discover(t, w, run.ID, " synthetic_key ")
	require.Equal(t, 1, result.NewCandidates)
	require.NoError(t, w.Close())
	cmd := exec.Command(os.Args[0], "-test.run=^TestRestartThroughPublicInterface$")
	cmd.Env = append(os.Environ(), "WPC_STORE_RESTART_FIXTURE="+dir)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

// covers: AC-7, AC-10.
func TestPrivateStateAndWriterOwnership(t *testing.T) {
	w, dir := openWriter(t)
	second, err := store.OpenWriter(t.Context(), dir, func() time.Time { return fixtureTime })
	require.ErrorIs(t, err, store.ErrBusy)
	require.Nil(t, second)
	r, err := store.OpenReader(t.Context(), dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	run := startRun(t, w, state.Crawl)
	discover(t, w, run.ID, "synthetic_key")
	candidates, err := r.ListCandidates(t.Context(), state.PageRequest{}, evaluation())
	require.NoError(t, err)
	require.Len(t, candidates.Items, 1)
	for _, name := range []string{"", "state.db", "writer.lock", "state.db-wal", "state.db-shm"} {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err)
		require.Zero(t, info.Mode().Perm()&0077)
	}
	require.NoError(t, w.Close())
	require.NoError(t, w.Close())
	_, err = w.ListRuns(t.Context(), "", state.PageRequest{})
	require.ErrorIs(t, err, store.ErrClosed)
	_, err = w.StartRun(
		t.Context(),
		state.RunInput{RequestID: "after_close", Kind: state.Crawl, StartedAt: fixtureTime},
	)
	require.ErrorIs(t, err, store.ErrClosed)
	stillReadable, err := r.ListCandidates(t.Context(), state.PageRequest{}, evaluation())
	require.NoError(t, err)
	require.Equal(t, candidates, stillReadable)
	require.NoError(t, r.Close())
	require.NoError(t, r.Close())
	_, err = r.ListCandidates(t.Context(), state.PageRequest{}, evaluation())
	require.ErrorIs(t, err, store.ErrClosed)
}

// covers: AC-9, AC-12.
func TestBoundedCandidateTraversal(t *testing.T) {
	w, _ := openWriter(t)
	run := startRun(t, w, state.Crawl)
	keys := make([]string, 338)
	for i := range keys {
		keys[i] = rand.Text()
	}
	discover(t, w, run.ID, keys...)
	first, err := w.ListCandidates(t.Context(), state.PageRequest{Size: 37}, evaluation())
	require.NoError(t, err)
	require.Len(t, first.Items, 37)
	discover(t, w, run.ID, "later_insert")
	preview, err := w.ListCandidates(t.Context(), state.PageRequest{Size: 37, Cursor: first.Next}, evaluation())
	require.NoError(t, err)
	validation := startRun(t, w, state.Validation)
	changedID := preview.Items[0].ID
	attempt := beginAttempt(t, w, validation.ID, changedID, fixtureTime)
	_, err = w.CompleteAttempt(
		t.Context(),
		state.AttemptCompletion{
			AttemptID: attempt.ID, FinishedAt: fixtureTime, Outcome: state.Confirmed,
			Evidence: &state.Evidence{Kind: state.AccountType, ObservedAt: fixtureTime, AccountType: "Unlimited"},
		},
	)
	require.NoError(t, err)
	seen := map[int64]bool{}
	page := first
	for {
		for _, c := range page.Items {
			require.False(t, seen[c.ID])
			seen[c.ID] = true
			if c.ID == changedID {
				require.Equal(t, state.Confirmed, c.LatestOutcome)
			}
		}
		if page.Next == nil {
			break
		}
		// JSON round trip proves the typed cursor crosses process serialization.
		b, err := json.Marshal(page.Next)
		require.NoError(t, err)
		var cursor state.Cursor
		require.NoError(t, json.Unmarshal(b, &cursor))
		page, err = w.ListCandidates(t.Context(), state.PageRequest{Size: 37, Cursor: &cursor}, evaluation())
		require.NoError(t, err)
	}
	require.Len(t, seen, 338)
	invalid := *first.Next
	invalid.Collection = "runs"
	_, err = w.ListCandidates(t.Context(), state.PageRequest{Cursor: &invalid}, evaluation())
	require.ErrorIs(t, err, store.ErrInvalidCursor)
	_, err = w.ListCandidates(
		t.Context(),
		state.PageRequest{Cursor: first.Next},
		state.Evaluation{AsOf: fixtureTime.Add(time.Second), MaxAge: 6 * time.Hour},
	)
	require.ErrorIs(t, err, store.ErrInvalidCursor)
	_, err = w.FinishRun(
		t.Context(),
		state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: fixtureTime},
	)
	require.NoError(t, err)
	observations := 0
	var cursor *state.Cursor
	for {
		p, err := w.ListObservations(t.Context(), run.ID, state.PageRequest{Size: 37, Cursor: cursor})
		require.NoError(t, err)
		observations += len(p.Items)
		cursor = p.Next
		if cursor == nil {
			break
		}
	}
	require.Equal(t, 339, observations)
}
