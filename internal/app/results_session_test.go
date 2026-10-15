package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

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
