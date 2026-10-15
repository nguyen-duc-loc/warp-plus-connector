package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// Spec 0011 AC-1 through AC-4, AC-8, AC-11: independent writer commits cannot
// change a displayed snapshot, and disappearance cannot erase a retained export.
func TestResultsSession_FixedViewsAndRetainedExport(t *testing.T) {
	a := crawlApp(t)
	seedQuerySnapshot(t, a.Settings.StateDir)
	s := a.NewResultsSession()
	defer func() { require.NoError(t, s.Close()) }()
	progress := func(string, int64) {}
	commit := func(f func() error) error { return f() }
	ctx := t.Context()
	require.NoError(t, s.Open(ctx, false, nil, progress, commit))
	require.Equal(t, int64(338), s.View.Count)
	for offset := int64(0); offset < 338; {
		page, err := s.Page(ctx, offset)
		require.NoError(t, err)
		require.NotEmpty(t, page)
		for _, r := range page {
			require.Equal(t, offset+1, r.Candidate.ID)
			offset++
		}
	}
	require.NoError(t, s.Mark(ctx, 338, true))
	require.NoError(t, s.Mark(ctx, 338, true))
	require.Equal(t, int64(1), s.View.Marked)
	before := *s.View
	w, err := store.OpenWriter(ctx, a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	run, err := w.StartRun(ctx, state.RunInput{RequestID: "results-later", Kind: state.Crawl, StartedAt: a.Clock()})
	require.NoError(t, err)
	_, err = w.RecordMessage(ctx, state.MessageInput{
		RunID: run.ID, SourceID: "later", MessageID: "1", ObservedAt: a.Clock(),
		Keys: []string{"later-candidate"}})
	require.NoError(t, err)
	for i := 1; i <= 65; i++ {
		row, err := s.Candidate(ctx, 1)
		require.NoError(t, err)
		_, err = w.RecordMessage(ctx, state.MessageInput{RunID: run.ID, SourceID: "later", MessageID: fmt.Sprint(i + 1),
			ObservedAt: a.Clock(), Keys: []string{row.Candidate.Key}})
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	var exported int64
	require.NoError(t, s.VisitExport(ctx, false, func(c state.Candidate) error { exported++; return nil }))
	require.Equal(t, int64(338), exported)
	require.Equal(t, before, *s.View)
	require.NoError(t, s.OpenDetail(ctx, 1, progress, commit))
	require.Equal(t, int64(66), s.Detail.Sources)
	rows, err := s.History(ctx, "sources", 50)
	require.NoError(t, err)
	require.Len(t, rows, 16)
	var sighting state.Sighting
	require.NoError(t, json.Unmarshal(rows[15], &sighting))
	require.Equal(t, "later", sighting.SourceID)
	require.NoError(t, s.CloseDetail(ctx))
	focus := int64(338)
	require.NoError(t, s.Open(ctx, true, &focus, progress, commit))
	require.Equal(t, int64(339), s.View.Count)
	require.Equal(t, int64(1), s.View.Marked)
	require.Equal(t, focus, *s.View.FocusID)
	q, err := (app.CandidateQuery{Status: []string{"unchecked"}}).Normalize()
	require.NoError(t, err)
	require.NoError(t, s.Filter(ctx, q, progress, commit))
	require.Zero(t, s.View.Marked)
	require.NoError(t, s.Mark(ctx, 338, true))
	require.NoError(t, s.Filter(ctx, q, progress, commit))
	require.Equal(t, int64(1), s.View.Marked)
	old := *s.View
	require.NoError(t, os.Rename(a.Settings.StateDir, a.Settings.StateDir+"-moved"))
	require.Error(t, s.Open(ctx, true, &focus, progress, commit))
	require.True(t, s.View.Retained)
	require.Equal(t, old.Evaluation, s.View.Evaluation)
	require.Equal(t, old.ID, s.View.ID)
	exported = 0
	require.NoError(t, s.VisitExport(ctx, true, func(c state.Candidate) error {
		exported++
		require.Equal(t, int64(338), c.ID)
		return nil
	}))
	require.Equal(t, int64(1), exported)
	_, err = s.Page(ctx, 340)
	require.Error(t, err)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.Error(t, s.Filter(cancelled, app.CandidateQuery{}, progress, commit))
	require.Equal(t, old.ID, s.View.ID)
}

// covers: spec 0011 AC-1, AC-14. Opening an absent collection must not initialize it.
func TestResultsSession_AbsentAndEmptyRemainDistinct(t *testing.T) {
	a := crawlApp(t)
	s := a.NewResultsSession()
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	commit := func(f func() error) error { return f() }
	err := s.Open(t.Context(), false, nil, func(string, int64) {}, commit)
	var failure *app.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "state_not_initialized", failure.Code)
	require.Nil(t, s.View)
	_, err = os.Stat(a.Settings.StateDir)
	require.ErrorIs(t, err, os.ErrNotExist)
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	// Keep the writer owned while the reader opens an initialized empty collection.
	require.NoError(t, s.Open(t.Context(), false, nil, func(string, int64) {}, commit))
	require.Zero(t, s.View.Count)
	require.Nil(t, s.View.FocusID)
	page, err := s.Page(t.Context(), 0)
	require.NoError(t, err)
	require.Empty(t, page)
}

// covers: spec 0011 AC-3, AC-11. A reused ID must not retain a mark or a detail identity.
func TestResultsSession_ReplacementReconcilesIdentity(t *testing.T) {
	a := crawlApp(t)
	seedQuerySnapshot(t, a.Settings.StateDir)
	s := a.NewResultsSession()
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	ctx := t.Context()
	progress := func(string, int64) {}
	commit := func(f func() error) error { return f() }
	require.NoError(t, s.Open(ctx, false, nil, progress, commit))
	require.NoError(t, s.Mark(ctx, 1, true))
	require.NoError(t, s.Mark(ctx, 338, true))
	require.NoError(t, os.Rename(a.Settings.StateDir, a.Settings.StateDir+"-old"))
	w, err := store.OpenWriter(ctx, a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	run, err := w.StartRun(ctx, state.RunInput{RequestID: "replacement", Kind: state.Crawl, StartedAt: a.Clock()})
	require.NoError(t, err)
	_, err = w.RecordMessage(ctx, state.MessageInput{RunID: run.ID, SourceID: "fixture", MessageID: "1",
		ObservedAt: a.Clock(), Keys: []string{"new-identity", "successor"}})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	var failure *app.Error
	require.ErrorAs(t, s.OpenDetail(ctx, 1, progress, commit), &failure)
	require.Equal(t, "ui_collection_changed", failure.Code)
	require.Nil(t, s.Detail)
	focus := int64(1)
	require.NoError(t, s.Open(ctx, true, &focus, progress, commit))
	require.Zero(t, s.View.Marked)
	require.Equal(t, int64(2), s.View.Removed)
	require.Equal(t, int64(2), *s.View.FocusID)
	require.Equal(t, int64(1), *s.View.FocusOffset)
}

// covers: spec 0011 AC-2, AC-3, AC-11. A cancelled activation cannot replace the usable view.
func TestResultsSession_CancelledActivationPreservesView(t *testing.T) {
	for _, action := range []string{"refresh", "filter", "details"} {
		t.Run(action, func(t *testing.T) {
			a := crawlApp(t)
			seedQuerySnapshot(t, a.Settings.StateDir)
			s := a.NewResultsSession()
			t.Cleanup(func() { require.NoError(t, s.Close()) })
			ctx := t.Context()
			progress := func(string, int64) {}
			require.NoError(t, s.Open(ctx, false, nil, progress, func(f func() error) error { return f() }))
			require.NoError(t, s.Mark(ctx, 338, true))
			before := *s.View
			refuse := func(func() error) error { return context.Canceled }
			var err error
			switch action {
			case "refresh":
				err = s.Open(ctx, true, nil, progress, refuse)
			case "filter":
				err = s.Filter(ctx, app.CandidateQuery{Status: []string{"invalid"}}, progress, refuse)
			case "details":
				err = s.OpenDetail(ctx, 338, progress, refuse)
			}
			require.Error(t, err)
			require.Equal(t, before.ID, s.View.ID)
			require.Equal(t, before.Evaluation, s.View.Evaluation)
			require.Equal(t, before.Marked, s.View.Marked)
			require.Nil(t, s.Detail)
			row, err := s.Candidate(ctx, 338)
			require.NoError(t, err)
			require.True(t, row.Marked)
			require.Equal(t, action == "refresh", s.View.Retained)
			require.NoError(t, s.OpenDetail(ctx, 338, progress, func(f func() error) error { return f() }))
			require.Equal(t, int64(1), s.Detail.Sources)
		})
	}
}

// covers: spec 0011 AC-8, AC-10, AC-12. Sparse marks cross pages and callback failures stop iteration.
func TestResultsSession_ExportStopsWithoutLosingMarks(t *testing.T) {
	a := crawlApp(t)
	seedQuerySnapshot(t, a.Settings.StateDir)
	s := a.NewResultsSession()
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	require.NoError(t, s.Open(t.Context(), false, nil, func(string, int64) {}, func(f func() error) error { return f() }))
	want := []int64{1, 51, 201, 338}
	for _, id := range want {
		require.NoError(t, s.Mark(t.Context(), id, true))
	}
	sentinel := errors.New("encoder stopped")
	for _, cancellation := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		err := s.VisitExport(ctx, true, func(state.Candidate) error {
			calls++
			if cancellation {
				cancel()
				return nil
			}
			return sentinel
		})
		cancel()
		if cancellation {
			require.ErrorIs(t, err, context.Canceled)
		} else {
			require.ErrorIs(t, err, sentinel)
		}
		require.Equal(t, 1, calls)
	}
	var got []int64
	require.NoError(t, s.VisitExport(t.Context(), true, func(c state.Candidate) error {
		got = append(got, c.ID)
		return nil
	}))
	require.Equal(t, want, got)
}

// covers: spec 0011 AC-2. Changing filters cannot advance the evaluation clock.
func TestResultsSession_FilterUsesSavedFreshness(t *testing.T) {
	a := crawlApp(t)
	seedQuerySnapshot(t, a.Settings.StateDir)
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	run, err := w.StartRun(t.Context(), state.RunInput{
		RequestID: "freshness", Kind: state.Validation, StartedAt: a.Clock(),
	})
	require.NoError(t, err)
	for i, observed := range []time.Time{crawlTime, crawlTime.Add(-time.Hour), crawlTime.Add(time.Microsecond)} {
		attempt, err := w.BeginAttempt(t.Context(), state.AttemptInput{RequestID: fmt.Sprint("fresh-", i),
			RunID: run.ID, CandidateID: int64(i + 1), BackendID: "fixture", BackendVersion: "1", StartedAt: observed})
		require.NoError(t, err)
		_, err = w.CompleteAttempt(t.Context(), state.AttemptCompletion{AttemptID: attempt.ID,
			Outcome: state.Confirmed, FinishedAt: observed,
			Evidence: &state.Evidence{Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: observed}})
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	s := a.NewResultsSession()
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	progress := func(string, int64) {}
	commit := func(f func() error) error { return f() }
	require.NoError(t, s.Open(t.Context(), false, nil, progress, commit))
	a.Clock = func() time.Time { t.Fatal("filters sampled the live clock"); return time.Time{} }
	for i, freshness := range []string{"fresh", "stale", "unknown", "not_applicable"} {
		t.Run(freshness, func(t *testing.T) {
			require.NoError(t, s.Filter(t.Context(), app.CandidateQuery{Status: []string{"confirmed"},
				Definitive: []string{"confirmed"}, Freshness: []string{freshness}}, progress, commit))
			page, err := s.Page(t.Context(), 0)
			require.NoError(t, err)
			if freshness == "not_applicable" {
				require.Empty(t, page)
			} else {
				require.Len(t, page, 1)
				require.Equal(t, int64(i+1), page[0].Candidate.ID)
			}
			require.Equal(t, crawlTime, s.View.Evaluation.AsOf)
		})
	}
}

// Spec 0011 AC-4: one detail snapshot retains complete history, an unresolved
// attempt, and nullable saved retry facts without refreshing Results evidence.
func TestResultsSession_DetailHistoryAndRetryFacts(t *testing.T) {
	a := crawlApp(t)
	seedQuerySnapshot(t, a.Settings.StateDir)
	s := a.NewResultsSession()
	defer func() { require.NoError(t, s.Close()) }()
	ctx := t.Context()
	progress := func(string, int64) {}
	commit := func(f func() error) error { return f() }
	require.NoError(t, s.Open(ctx, false, nil, progress, commit))
	w, err := store.OpenWriter(ctx, a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	run, err := w.StartRun(ctx, state.RunInput{RequestID: "history", Kind: state.Validation, StartedAt: a.Clock()})
	require.NoError(t, err)
	for i := 0; i < 65; i++ {
		attempt, err := w.BeginAttempt(ctx, state.AttemptInput{RequestID: fmt.Sprint("history-", i), RunID: run.ID,
			CandidateID: 1, BackendID: "fixture", BackendVersion: "saved-version", StartedAt: a.Clock()})
		require.NoError(t, err)
		completion := state.AttemptCompletion{AttemptID: attempt.ID, FinishedAt: a.Clock(),
			Outcome: state.Inconclusive, ReasonCode: "validation_timeout"}
		if i == 0 {
			completion.Outcome = state.Confirmed
			completion.ReasonCode = ""
			completion.Evidence = &state.Evidence{Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: a.Clock()}
		}
		_, err = w.CompleteAttempt(ctx, completion)
		require.NoError(t, err)
	}
	_, err = w.FinishRun(ctx, state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: a.Clock()})
	require.NoError(t, err)
	batch, err := w.StartValidationBatch(ctx, state.ValidationBatchInput{RequestID: "batch-history", StartedAt: a.Clock(),
		CandidateIDs: []int64{1}, BackendID: "fixture", BackendVersion: "saved-version", Policy: state.DefaultCheckPolicy()})
	require.NoError(t, err)
	_, err = w.BeginBatchAttempt(ctx, state.BatchAttemptInput{Number: 1, AttemptInput: state.AttemptInput{
		RequestID: "batch-history-attempt", RunID: batch.Run.ID, CandidateID: 1, BackendID: "fixture",
		BackendVersion: "saved-version", StartedAt: a.Clock(),
	}})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, s.OpenDetail(ctx, 1, progress, commit))
	require.Equal(t, int64(66), s.Detail.Attempts)
	require.Equal(t, state.Inconclusive, s.Detail.Summary.LatestOutcome)
	require.Equal(t, state.Confirmed, s.Detail.Summary.LastDefinitiveAttempt.Outcome)
	require.NotNil(t, s.Detail.Summary.UnresolvedAttempt)
	rows, err := s.History(ctx, "attempts", 50)
	require.NoError(t, err)
	require.Len(t, rows, 16)
	var legacy, retry store.DetailAttempt
	require.NoError(t, json.Unmarshal(rows[0], &legacy))
	require.Nil(t, legacy.Retry)
	require.NoError(t, json.Unmarshal(rows[15], &retry))
	require.NotNil(t, retry.Retry)
	require.Equal(t, 1, retry.Retry.Number)
	require.Zero(t, retry.Retry.RetryWait)
	require.Empty(t, retry.Attempt.Outcome)
	saved, err := s.Candidate(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, state.Unchecked, saved.Candidate.LatestOutcome)
}
