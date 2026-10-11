package store

import (
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
)

// AC-8, AC-11: the real migrated schema preserves retry history and interrupts work without resuming it.
func TestValidationBatchReplayRecoveryAndLegacyGuards(t *testing.T) {
	ctx := t.Context()
	dir := filepath.Join(t.TempDir(), "state")
	now := time.Now().UTC().Truncate(time.Microsecond)
	w, err := OpenWriter(ctx, dir, time.Now)
	require.NoError(t, err)
	run, err := w.StartRun(ctx, state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: now})
	require.NoError(t, err)
	_, err = w.RecordMessage(
		ctx, state.MessageInput{
			RunID: run.ID, SourceID: "fixture", MessageID: "1", ObservedAt: now,
			Keys: []string{"00000001-12345678-AbCdEf01", "00000002-12345678-AbCdEf01"},
		},
	)
	require.NoError(t, err)
	_, err = w.FinishRun(ctx, state.RunCompletion{RunID: run.ID, FinishedAt: now, Status: state.Completed})
	require.NoError(t, err)
	input := state.ValidationBatchInput{
		RequestID: "batch", StartedAt: now, CandidateIDs: []int64{2, 1},
		BackendID: "fixture", BackendVersion: "1", Policy: state.DefaultCheckPolicy(),
	}
	report, err := w.StartValidationBatch(ctx, input)
	require.NoError(t, err)
	again, err := w.StartValidationBatch(ctx, input)
	require.NoError(t, err)
	require.Equal(t, report, again)
	input.CandidateIDs = []int64{1, 2}
	_, err = w.StartValidationBatch(ctx, input)
	require.ErrorIs(t, err, ErrConflict)
	intent := state.BatchAttemptInput{
		AttemptInput: state.AttemptInput{
			RequestID: "attempt", RunID: report.Run.ID,
			CandidateID: 2, BackendID: "fixture", BackendVersion: "1", StartedAt: now,
		}, Number: 1,
	}
	_, err = w.BeginAttempt(ctx, intent.AttemptInput)
	require.ErrorIs(t, err, ErrInvalidInput)
	attempt, err := w.BeginBatchAttempt(ctx, intent)
	require.NoError(t, err)
	due := now.Add(5 * time.Second)
	completion := state.BatchAttemptCompletion{
		AttemptCompletion: state.AttemptCompletion{
			AttemptID: attempt.Attempt.ID, FinishedAt: now, Outcome: state.Inconclusive,
			ReasonCode: "validation_timeout",
		},
		FailureClass: state.Temporary, RetryWait: 5 * time.Second, RetryEligibleAt: &due,
	}
	_, err = w.CompleteAttempt(ctx, completion.AttemptCompletion)
	require.ErrorIs(t, err, ErrInvalidInput)
	saved, err := w.CompleteBatchAttempt(ctx, completion)
	require.NoError(t, err)
	repeated, err := w.CompleteBatchAttempt(ctx, completion)
	require.NoError(t, err)
	require.Equal(t, saved, repeated)
	completion.RetryWait = 6 * time.Second
	_, err = w.CompleteBatchAttempt(ctx, completion)
	require.ErrorIs(t, err, ErrConflict)
	_, err = w.FinishRun(ctx, state.RunCompletion{RunID: report.Run.ID, Status: state.Completed, FinishedAt: now})
	require.ErrorIs(t, err, ErrInvalidInput)
	var tables int
	require.NoError(
		t, w.h.writeDB.QueryRowContext(
			ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' "+
				"AND name IN ('validation_batches','validation_items','validation_attempt_details')",
		).Scan(&tables),
	)
	require.Equal(t, 3, tables)
	require.NoError(t, w.Close())
	w, err = OpenWriter(ctx, dir, time.Now)
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	recovered, err := w.GetValidationReport(ctx, report.Run.ID)
	require.NoError(t, err)
	require.Equal(t, state.Interrupted, recovered.Run.Status)
	require.Equal(t, 2, recovered.Totals.Interrupted)
	require.Equal(t, 1, recovered.Totals.Unstarted)
	item, err := w.GetValidationItem(ctx, report.Run.ID, 2)
	require.NoError(t, err)
	require.Nil(t, item.FinishedAt)
	require.NotNil(t, item.StartedAt)
	require.NotNil(t, item.RecoveredAt)
	history, err := w.ListBatchAttempts(ctx, report.Run.ID, 2, state.PageRequest{})
	require.NoError(t, err)
	require.Equal(t, saved, history.Items[0])
	intent.RequestID = "new"
	intent.Number = 2
	_, err = w.BeginBatchAttempt(ctx, intent)
	require.Error(t, err)
}
