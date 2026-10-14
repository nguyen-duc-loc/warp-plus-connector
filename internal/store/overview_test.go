package store_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// Spec 0010 AC-4 through AC-6: totals and bounded history share an established snapshot.
func TestOverviewSnapshotAndHistory(t *testing.T) {
	w, dir := openWriter(t)
	var run state.Run
	for range 12 {
		run = startRun(t, w, state.Crawl)
		_, err := w.FinishRun(
			t.Context(),
			state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: fixtureTime},
		)
		require.NoError(t, err)
	}
	run = startRun(t, w, state.Crawl)
	keys := make([]string, 338)
	for i := range keys {
		keys[i] = fmt.Sprintf("overview_fixture_%d", i)
	}
	discover(t, w, run.ID, keys...)
	r, err := store.OpenReader(t.Context(), dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	o, err := r.GetOverview(
		t.Context(), func() time.Time {
			discover(t, w, run.ID, "later_commit")
			return fixtureTime
		}, time.Hour,
	)
	require.NoError(t, err)
	require.Equal(t, int64(338), o.Outcomes.Total)
	require.Equal(t, o.Outcomes.Total, o.Schedule.Candidates.Total)
	require.Equal(t, o.AsOf, o.Schedule.AsOf)
	require.Len(t, o.RecentRuns, 10)
	require.True(t, o.OlderRunsAvailable)
	require.Equal(t, run.ID, o.RecentRuns[0].Run.ID)
	require.Equal(t, int64(338), o.RecentRuns[0].SavedCandidates)
}

// Spec 0010 AC-4: later uncertainty, the exact boundary, and future evidence remain distinct.
func TestOverviewDefinitiveFreshness(t *testing.T) {
	w, _ := openWriter(t)
	crawl := startRun(t, w, state.Crawl)
	discover(t, w, crawl.ID, "first")
	run := startRun(t, w, state.Validation)
	a := beginAttempt(t, w, run.ID, 1, fixtureTime)
	_, err := w.CompleteAttempt(
		t.Context(), state.AttemptCompletion{
			AttemptID:  a.ID,
			FinishedAt: fixtureTime, Outcome: state.Confirmed,
			Evidence: &state.Evidence{Kind: state.AccountType, ObservedAt: fixtureTime, AccountType: "Unlimited"},
		},
	)
	require.NoError(t, err)
	a = beginAttempt(t, w, run.ID, 1, fixtureTime)
	_, err = w.CompleteAttempt(
		t.Context(), state.AttemptCompletion{
			AttemptID:  a.ID,
			FinishedAt: fixtureTime, Outcome: state.Inconclusive, ReasonCode: "validation_timeout",
		},
	)
	require.NoError(t, err)
	for _, tc := range []struct {
		elapsed               time.Duration
		fresh, stale, unknown int64
	}{
		{time.Hour - time.Microsecond, 1, 0, 0}, {time.Hour, 0, 1, 0}, {-time.Microsecond, 0, 0, 1},
	} {
		o, err := w.GetOverview(t.Context(), func() time.Time { return fixtureTime.Add(tc.elapsed) }, time.Hour)
		require.NoError(t, err)
		require.Equal(t, int64(1), o.Outcomes.Inconclusive)
		require.Equal(t, tc.fresh, o.FreshConfirmed)
		require.Equal(t, tc.stale, o.StaleConfirmed)
		require.Equal(t, tc.unknown, o.UnknownFreshness)
		require.Nil(t, o.RecentRuns[0].Totals)
	}
}
