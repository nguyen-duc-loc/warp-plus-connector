package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// covers: AC-7, AC-10, AC-11.
func TestPermissionAndSymlinkRefusal(t *testing.T) {
	for _, name := range []string{"", "state.db", "writer.lock", "state.db-wal", "state.db-shm", "state.db-journal"} {
		for _, symlink := range []bool{false, true} {
			if name == "" && symlink {
				continue
			} // State directory aliases are explicitly supported.
			label := name
			if symlink {
				label += "_symlink"
			}
			t.Run(
				label, func(t *testing.T) {
					w, dir := openWriter(t)
					require.NoError(t, w.Close())
					path := filepath.Join(dir, name)
					if symlink {
						target := filepath.Join(t.TempDir(), "target")
						require.NoError(t, os.WriteFile(target, []byte("synthetic fixture"), 0600))
						if err := os.Remove(path); err != nil {
							require.True(t, os.IsNotExist(err))
						}
						require.NoError(t, os.Symlink(target, path))
					} else if name == "" {
						require.NoError(t, os.Chmod(path, 0755))
					} else {
						f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
						require.NoError(t, err)
						require.NoError(t, f.Close())
						require.NoError(t, os.Chmod(path, 0644))
					}
					before, err := os.Lstat(path)
					require.NoError(t, err)
					reader, err := store.OpenReader(t.Context(), dir)
					require.ErrorIs(t, err, store.ErrStateAccess)
					require.Nil(t, reader)
					writer, err := store.OpenWriter(t.Context(), dir, time.Now)
					require.ErrorIs(t, err, store.ErrStateAccess)
					require.Nil(t, writer)
					after, err := os.Lstat(path)
					require.NoError(t, err)
					require.Equal(t, before.Mode(), after.Mode())
				},
			)
		}
	}
	missing := filepath.Join(t.TempDir(), "missing")
	r, err := store.OpenReader(t.Context(), missing)
	require.ErrorIs(t, err, store.ErrNotInitialized)
	require.Nil(t, r)
	_, err = os.Stat(missing)
	require.True(t, os.IsNotExist(err))
}

// covers: AC-6, AC-9, AC-10.
func TestInputRejectionIsAtomicAndSanitized(t *testing.T) {
	w, _ := openWriter(t)
	run := startRun(t, w, state.Crawl)
	for _, keys := range [][]string{
		{"synthetic_secret", " "}, {"synthetic_secret", string([]byte{0xff})}, {"synthetic_secret", "nul\x00key"},
	} {
		_, err := w.RecordMessage(
			t.Context(),
			state.MessageInput{
				RunID: run.ID, SourceID: "source", MessageID: "message", ObservedAt: fixtureTime, Keys: keys,
			},
		)
		require.ErrorIs(t, err, store.ErrInvalidInput)
		require.NotContains(t, err.Error(), "synthetic_secret")
	}
	for _, url := range []string{
		"relative", "https://user:secret@example.com/a", "https://example.com/a?token=secret",
		"https://example.com/a#secret", "https://example.com/a?", "file:///tmp/key",
	} {
		_, err := w.RecordMessage(
			t.Context(),
			state.MessageInput{
				RunID: run.ID, SourceID: "source", MessageID: "message", ObservedAt: fixtureTime, MessageURL: &url,
				Keys: []string{"synthetic_secret"},
			},
		)
		require.ErrorIs(t, err, store.ErrInvalidInput)
		require.NotContains(t, err.Error(), "secret")
	}
	p, err := w.ListCandidates(t.Context(), state.PageRequest{}, evaluation())
	require.NoError(t, err)
	require.Empty(t, p.Items)
	for _, at := range []time.Time{{}, time.Date(400000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		_, err = w.StartRun(t.Context(), state.RunInput{RequestID: "invalid_time", Kind: state.Crawl, StartedAt: at})
		require.ErrorIs(t, err, store.ErrInvalidInput)
	}
	_, err = w.GetCandidate(t.Context(), -1, evaluation())
	require.ErrorIs(t, err, store.ErrInvalidInput)
	_, err = w.GetCandidate(t.Context(), 123, evaluation())
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = w.ListCandidates(t.Context(), state.PageRequest{Size: 1001}, evaluation())
	require.ErrorIs(t, err, store.ErrInvalidInput)
}

// covers: AC-9, AC-12.
func TestAllHistoryCollectionsAreUncapped(t *testing.T) {
	w, _ := openWriter(t)
	crawl := startRun(t, w, state.Crawl)
	candidate := discover(t, w, crawl.ID, "synthetic").Discoveries[0].CandidateID
	for i := 0; i < 105; i++ {
		run := startRun(t, w, state.Crawl)
		_, err := w.RecordMessage(
			t.Context(),
			state.MessageInput{
				RunID: run.ID, SourceID: run.RequestID, MessageID: "message", ObservedAt: fixtureTime,
				Keys: []string{"synthetic"},
			},
		)
		require.NoError(t, err)
	}
	validation := startRun(t, w, state.Validation)
	for i := 0; i < 105; i++ {
		attempt := beginAttempt(t, w, validation.ID, candidate, fixtureTime)
		_, err := w.CompleteAttempt(
			t.Context(),
			state.AttemptCompletion{
				AttemptID: attempt.ID, FinishedAt: fixtureTime, Outcome: state.Inconclusive,
				ReasonCode: "fixture_unknown",
			},
		)
		require.NoError(t, err)
	}
	count := 0
	var cursor *state.Cursor
	for {
		p, err := w.ListRuns(t.Context(), state.Crawl, state.PageRequest{Size: 17, Cursor: cursor})
		require.NoError(t, err)
		count += len(p.Items)
		cursor = p.Next
		if cursor == nil {
			break
		}
	}
	require.Equal(t, 106, count)
	count, cursor = 0, nil
	for {
		p, err := w.ListSightings(t.Context(), candidate, state.PageRequest{Size: 17, Cursor: cursor})
		require.NoError(t, err)
		count += len(p.Items)
		cursor = p.Next
		if cursor == nil {
			break
		}
	}
	require.Equal(t, 106, count)
	for _, filter := range []state.AttemptFilter{{CandidateID: candidate}, {RunID: validation.ID}} {
		count, cursor = 0, nil
		for {
			p, err := w.ListAttempts(t.Context(), filter, state.PageRequest{Size: 17, Cursor: cursor})
			require.NoError(t, err)
			count += len(p.Items)
			cursor = p.Next
			if cursor == nil {
				break
			}
		}
		require.Equal(t, 105, count)
	}
}

// covers: AC-9. A cursor cannot silently change its collection or evaluation.
func TestListCandidates_RejectsChangedCursorContract(t *testing.T) {
	w, _ := openWriter(t)
	run := startRun(t, w, state.Crawl)
	discover(t, w, run.ID, "a", "b", "c")
	first, err := w.ListCandidates(t.Context(), state.PageRequest{Size: 1}, evaluation())
	require.NoError(t, err)
	require.NotNil(t, first.Next)
	for _, tt := range []struct {
		name   string
		change func(*state.Cursor)
	}{
		{"version", func(c *state.Cursor) { c.Version++ }},
		{"collection", func(c *state.Cursor) { c.Collection = "runs" }},
		{"parent", func(c *state.Cursor) { c.ParentID = run.ID }},
		{"filter", func(c *state.Cursor) { c.Filter = "confirmed" }},
		{"zero last ID", func(c *state.Cursor) { c.LastID = 0 }},
		{"negative last ID", func(c *state.Cursor) { c.LastID = -1 }},
		{"exhausted bound", func(c *state.Cursor) { c.HighWaterID = c.LastID }},
		{"reversed bound", func(c *state.Cursor) { c.HighWaterID = c.LastID - 1 }},
		{"evaluation time", func(c *state.Cursor) { c.Evaluation.AsOf = fixtureTime.Add(time.Second) }},
		{"evaluation interval", func(c *state.Cursor) { c.Evaluation.MaxAge += time.Second }},
	} {
		t.Run(
			tt.name, func(t *testing.T) {
				cursor := *first.Next
				tt.change(&cursor)
				_, err := w.ListCandidates(t.Context(), state.PageRequest{Size: 1, Cursor: &cursor}, evaluation())
				require.ErrorIs(t, err, store.ErrInvalidCursor)
			},
		)
	}
	// Page size may change without changing the traversal's membership.
	next, err := w.ListCandidates(t.Context(), state.PageRequest{Size: 1000, Cursor: first.Next}, evaluation())
	require.NoError(t, err)
	require.Len(t, next.Items, 2)
	require.Nil(t, next.Next)
}

// covers: AC-9, AC-12. Histories cannot be traversed using another parent's cursor.
func TestHistoryPages_RejectChangedParentsAndFilters(t *testing.T) {
	w, _ := openWriter(t)
	crawl := startRun(t, w, state.Crawl)
	otherCrawl := startRun(t, w, state.Crawl)
	keys := discover(t, w, crawl.ID, "a", "b")
	_, err := w.RecordMessage(
		t.Context(),
		state.MessageInput{
			RunID: crawl.ID, SourceID: "other_source", MessageID: "message", ObservedAt: fixtureTime,
			Keys: []string{"a"},
		},
	)
	require.NoError(t, err)
	for _, run := range []state.Run{crawl, otherCrawl} {
		_, err := w.FinishRun(
			t.Context(),
			state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: fixtureTime},
		)
		require.NoError(t, err)
	}
	validation := startRun(t, w, state.Validation)
	for i := 0; i < 2; i++ {
		a := beginAttempt(t, w, validation.ID, keys.Discoveries[0].CandidateID, fixtureTime)
		_, err := w.CompleteAttempt(
			t.Context(),
			state.AttemptCompletion{
				AttemptID: a.ID, FinishedAt: fixtureTime, Outcome: state.Inconclusive, ReasonCode: "unsupported",
			},
		)
		require.NoError(t, err)
	}
	t.Run(
		"runs", func(t *testing.T) {
			page, err := w.ListRuns(t.Context(), state.Crawl, state.PageRequest{Size: 1})
			require.NoError(t, err)
			require.NotNil(t, page.Next)
			_, err = w.ListRuns(t.Context(), state.Validation, state.PageRequest{Cursor: page.Next})
			require.ErrorIs(t, err, store.ErrInvalidCursor)
		},
	)
	t.Run(
		"sightings", func(t *testing.T) {
			page, err := w.ListSightings(t.Context(), keys.Discoveries[0].CandidateID, state.PageRequest{Size: 1})
			require.NoError(t, err)
			require.NotNil(t, page.Next)
			_, err = w.ListSightings(t.Context(), keys.Discoveries[1].CandidateID, state.PageRequest{Cursor: page.Next})
			require.ErrorIs(t, err, store.ErrInvalidCursor)
		},
	)
	t.Run(
		"observations", func(t *testing.T) {
			page, err := w.ListObservations(t.Context(), crawl.ID, state.PageRequest{Size: 1})
			require.NoError(t, err)
			require.NotNil(t, page.Next)
			_, err = w.ListObservations(t.Context(), otherCrawl.ID, state.PageRequest{Cursor: page.Next})
			require.ErrorIs(t, err, store.ErrInvalidCursor)
		},
	)
	t.Run(
		"attempts", func(t *testing.T) {
			page, err := w.ListAttempts(
				t.Context(),
				state.AttemptFilter{CandidateID: keys.Discoveries[0].CandidateID},
				state.PageRequest{Size: 1},
			)
			require.NoError(t, err)
			require.NotNil(t, page.Next)
			_, err = w.ListAttempts(
				t.Context(),
				state.AttemptFilter{RunID: validation.ID},
				state.PageRequest{Cursor: page.Next},
			)
			require.ErrorIs(t, err, store.ErrInvalidCursor)
		},
	)
}

// covers: AC-7, AC-8, AC-11. Failed opening must release ownership for a retry.
func TestOpenWriter_ReleasesOwnershipAfterRecoveryClockFailure(t *testing.T) {
	w, dir := openWriter(t)
	run := startRun(t, w, state.Crawl)
	require.NoError(t, w.Close())
	failed, err := store.OpenWriter(t.Context(), dir, func() time.Time { return time.Time{} })
	require.ErrorIs(t, err, store.ErrInvalidInput)
	require.Nil(t, failed)
	r, err := store.OpenReader(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	page, err := r.ListRuns(t.Context(), "", state.PageRequest{})
	require.NoError(t, err)
	require.Equal(t, []state.Run{run}, page.Items, "reader must not recover unfinished work")
	recoveredAt := fixtureTime.Add(-time.Hour)
	retry, err := store.OpenWriter(t.Context(), dir, func() time.Time { return recoveredAt })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, retry.Close()) })
	page, err = r.ListRuns(t.Context(), "", state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, state.Interrupted, page.Items[0].Status)
	require.Equal(t, &recoveredAt, page.Items[0].RecoveredAt)
	require.Nil(t, page.Items[0].FinishedAt)
	require.True(t, page.Items[0].ClockAnomaly)
}

// covers: AC-7, AC-10. Directory aliases must share the same writer lock.
func TestOpenWriter_DirectoryAliasPreservesOwnership(t *testing.T) {
	w, dir := openWriter(t)
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(dir, alias))
	clock := func() time.Time { return fixtureTime }
	other, err := store.OpenWriter(t.Context(), alias, clock)
	require.ErrorIs(t, err, store.ErrBusy)
	require.Nil(t, other)
	before, err := os.Stat(filepath.Join(dir, "writer.lock"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	other, err = store.OpenWriter(t.Context(), alias, clock)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, other.Close()) })
	after, err := os.Stat(filepath.Join(dir, "writer.lock"))
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "closing must retain the lock file")
}
