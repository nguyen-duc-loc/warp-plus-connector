package store_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// Spec 0008 AC-9 and AC-10: a concurrent commit cannot split status facts or hide terminal work.
func TestOperationalSnapshotIncludes338AndRetainsTerminalRun(t *testing.T) {
	w, dir := openWriter(t)
	run := startRun(t, w, state.Crawl)
	keys := make([]string, 338)
	for i := range keys {
		keys[i] = fmt.Sprintf("synthetic_%03d", i)
	}
	discover(t, w, run.ID, keys...)
	_, err := w.FinishRun(
		t.Context(),
		state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: fixtureTime},
	)
	require.NoError(t, err)
	active := startRun(t, w, state.Crawl)
	r, err := store.OpenReader(t.Context(), dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	calls := 0
	work, err := r.GetOperationalWork(
		t.Context(), func() time.Time {
			calls++
			discover(t, w, active.ID, "later_commit")
			return fixtureTime
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, int64(338), work.Outcomes.Total)
	require.Equal(t, int64(338), work.Outcomes.Unchecked)
	require.Equal(t, int64(338), work.Outcomes.DefinitiveNone)
	require.Equal(t, work.AsOf, work.Schedule.AsOf)
	require.Equal(t, int64(338), work.Schedule.Candidates.Total)
	require.Equal(t, int64(338), work.Schedule.Candidates.Due)
	require.Equal(t, active.ID, work.LatestCrawl.Run.ID)
	require.Equal(t, int64(0), work.LatestCrawl.SavedCandidates)
	require.Equal(t, run.ID, work.LastTerminalCrawl.Run.ID)
	require.Equal(t, int64(338), work.LastTerminalCrawl.SavedCandidates)
	require.Nil(t, work.LastTerminalCrawl.Sources)
	later, err := r.GetOperationalWork(t.Context(), func() time.Time { return fixtureTime })
	require.NoError(t, err)
	require.Equal(t, int64(339), later.Outcomes.Total)
}

// Spec 0008 AC-9: unresolved work and later uncertainty preserve definitive evidence.
func TestOperationalOutcomesKeepCompletedAndDefinitiveEvidenceSeparate(t *testing.T) {
	w, _ := openWriter(t)
	crawl := startRun(t, w, state.Crawl)
	discover(t, w, crawl.ID, "first", "second")
	run := startRun(t, w, state.Validation)
	first := beginAttempt(t, w, run.ID, 1, fixtureTime)
	_, err := w.CompleteAttempt(t.Context(), state.AttemptCompletion{
		AttemptID: first.ID, FinishedAt: fixtureTime, Outcome: state.Confirmed,
		Evidence: &state.Evidence{Kind: state.AccountType, ObservedAt: fixtureTime, AccountType: "Unlimited"},
	})
	require.NoError(t, err)
	second := beginAttempt(t, w, run.ID, 1, fixtureTime)
	_, err = w.CompleteAttempt(t.Context(), state.AttemptCompletion{
		AttemptID: second.ID, FinishedAt: fixtureTime, Outcome: state.Inconclusive, ReasonCode: "validation_timeout",
	})
	require.NoError(t, err)
	beginAttempt(t, w, run.ID, 1, fixtureTime)
	work, err := w.GetOperationalWork(t.Context(), func() time.Time { return fixtureTime })
	require.NoError(t, err)
	require.Equal(t, int64(2), work.Outcomes.Total)
	require.Equal(t, int64(1), work.Outcomes.Unchecked)
	require.Equal(t, int64(1), work.Outcomes.Inconclusive)
	require.Equal(t, int64(1), work.Outcomes.DefinitiveConfirmed)
	require.Equal(t, int64(1), work.Outcomes.DefinitiveNone)
	require.Nil(t, work.LatestValidation.Totals, "legacy runs have no invented batch metadata")
}
