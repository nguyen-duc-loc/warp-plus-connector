package app_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/stretchr/testify/require"
)

type failedBackend struct{ err error }

func (f failedBackend) Identity() (string, string) { return "fixture", "1" }
func (f failedBackend) Check(context.Context, validator.Input) (validator.Result, error) {
	return validator.Result{}, f.err
}

type confirmedBackend struct{}

func (confirmedBackend) Identity() (string, string) { return "controlled_fixture", "1" }
func (confirmedBackend) Check(context.Context, validator.Input) (validator.Result, error) {
	return validator.Result{
		Outcome:  state.Confirmed,
		Evidence: &state.Evidence{Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: time.Now().UTC()},
	}, nil
}

// AC-8: the normalized confirmation crosses application persistence unchanged.
func TestConfirmedCheckPersistsEvidence(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	ctx := context.Background()
	w, err := store.OpenWriter(ctx, dir, time.Now)
	require.NoError(t, err)
	run, err := w.StartRun(ctx, state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: time.Now()})
	require.NoError(t, err)
	_, err = w.RecordMessage(
		ctx,
		state.MessageInput{
			RunID: run.ID, SourceID: "controlled_fixture", MessageID: "1", ObservedAt: time.Now(),
			Keys: []string{"ABCDEFGH-12345678-AbCdEf01"},
		},
	)
	require.NoError(t, err)
	_, err = w.FinishRun(ctx, state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	a := app.App{
		Settings: config.Settings{StateDir: dir, RecheckInterval: time.Hour}, Validator: confirmedBackend{},
		Clock: time.Now,
	}
	attempt, err := a.Check(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, state.Confirmed, attempt.Outcome)
	r, err := store.OpenReader(ctx, dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	c, err := r.GetCandidate(ctx, 1, state.Evaluation{AsOf: time.Now(), MaxAge: time.Hour})
	require.NoError(t, err)
	require.Equal(t, attempt, *c.LatestAttempt)
	require.Equal(t, attempt, *c.LastDefinitiveAttempt)
	require.Equal(t, state.Fresh, c.Freshness)
	require.Equal(t, "Unlimited", c.LastDefinitiveAttempt.Evidence.AccountType)
}

// AC-4 and AC-6: printed attempts must describe the same terminal record as SQLite.
func TestFailedCheckReturnsItsSavedAttempt(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		err          error
	}{
		{"runtime", "validator_runtime_error", &validator.Error{Code: "validator_runtime_error"}},
		{"cancelled", "cancelled", context.Canceled},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				dir := t.TempDir()
				require.NoError(t, os.Chmod(dir, 0700))
				ctx := context.Background()
				w, err := store.OpenWriter(ctx, dir, time.Now)
				require.NoError(t, err)
				run, err := w.StartRun(ctx, state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: time.Now()})
				require.NoError(t, err)
				_, err = w.RecordMessage(
					ctx,
					state.MessageInput{
						RunID: run.ID, SourceID: "fixture", MessageID: "1", ObservedAt: time.Now(),
						Keys: []string{"ABCDEFGH-12345678-AbCdEf01"},
					},
				)
				require.NoError(t, err)
				_, err = w.FinishRun(
					ctx,
					state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: time.Now()},
				)
				require.NoError(t, err)
				require.NoError(t, w.Close())
				a := app.App{
					Settings:  config.Settings{StateDir: dir, RecheckInterval: time.Hour},
					Validator: failedBackend{tc.err}, Clock: time.Now,
				}
				attempt, err := a.Check(ctx, 1)
				require.Error(t, err)
				require.Equal(t, state.Inconclusive, attempt.Outcome)
				require.Equal(t, tc.reason, *attempt.ReasonCode)
				r, err := store.OpenReader(ctx, dir)
				require.NoError(t, err)
				defer func() { require.NoError(t, r.Close()) }()
				c, err := r.GetCandidate(ctx, 1, state.Evaluation{AsOf: time.Now(), MaxAge: time.Hour})
				require.NoError(t, err)
				require.Equal(t, attempt, *c.LatestAttempt)
			},
		)
	}
}

// covers: spec 0003 AC-3, AC-4. Unsupported checks cannot erase or refresh evidence.
func TestUnsupportedCheckPreservesDefinitiveEvidenceAndUnknownIDCreatesNoWork(t *testing.T) {
	a := crawlApp(t)
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: crawlTime})
	require.NoError(t, err)
	_, err = w.RecordMessage(
		t.Context(),
		state.MessageInput{
			RunID: run.ID, SourceID: "fixture", MessageID: "1", ObservedAt: crawlTime, Keys: []string{crawlKey},
		},
	)
	require.NoError(t, err)
	_, err = w.FinishRun(
		t.Context(),
		state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: crawlTime},
	)
	require.NoError(t, err)
	run, err = w.StartRun(
		t.Context(),
		state.RunInput{RequestID: "validation", Kind: state.Validation, StartedAt: crawlTime},
	)
	require.NoError(t, err)
	attempt, err := w.BeginAttempt(
		t.Context(),
		state.AttemptInput{
			RequestID: "confirmed", RunID: run.ID, CandidateID: 1, BackendID: "fixture", BackendVersion: "1",
			StartedAt: crawlTime,
		},
	)
	require.NoError(t, err)
	confirmed, err := w.CompleteAttempt(
		t.Context(),
		state.AttemptCompletion{
			AttemptID: attempt.ID, Outcome: state.Confirmed, FinishedAt: crawlTime,
			Evidence: &state.Evidence{Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: crawlTime},
		},
	)
	require.NoError(t, err)
	_, err = w.FinishRun(
		t.Context(),
		state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: crawlTime},
	)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	a.Clock = func() time.Time { return crawlTime.Add(2 * time.Hour) }
	a.Validator = validator.Unsupported{Version: "test"}
	attempt, err = a.Check(t.Context(), 1)
	require.EqualError(t, err, "validation_batch_inconclusive")
	require.Equal(t, state.Inconclusive, attempt.Outcome)
	_, err = a.Check(t.Context(), 999)
	require.ErrorIs(t, err, store.ErrNotFound)
	r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	candidate, err := r.GetCandidate(t.Context(), 1, state.Evaluation{AsOf: a.Clock(), MaxAge: time.Hour})
	require.NoError(t, err)
	require.Equal(t, confirmed, *candidate.LastDefinitiveAttempt)
	require.Equal(t, attempt, *candidate.LatestAttempt)
	require.Equal(t, state.Stale, candidate.Freshness)
	attempts, err := r.ListAttempts(t.Context(), state.AttemptFilter{CandidateID: 1}, state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, attempts.Items, 2)
	runs, err := r.ListRuns(t.Context(), state.Validation, state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, runs.Items, 2)
}
