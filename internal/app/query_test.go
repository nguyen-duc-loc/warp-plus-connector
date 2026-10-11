package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// covers: spec 0006 AC-2, AC-4. Normalization owns its data and rejects ambiguous values.
func TestCandidateQuery_NormalizesWithoutSharingInputs(t *testing.T) {
	limit := int64(100)
	input := app.CandidateQuery{
		Status:     []string{" invalid, unchecked ", "invalid"},
		Definitive: []string{"invalid,none"}, Freshness: []string{"unknown,fresh"}, Limit: &limit,
	}
	got, err := input.Normalize()
	require.NoError(t, err)
	require.Equal(t, []string{"unchecked", "invalid"}, got.Status)
	require.Equal(t, []string{"none", "invalid"}, got.Definitive)
	require.Equal(t, []string{"fresh", "unknown"}, got.Freshness)
	input.Status[0], limit = "confirmed", 1
	require.Equal(t, []string{"unchecked", "invalid"}, got.Status)
	require.Equal(t, int64(100), *got.Limit)
	for _, value := range []string{"", "confirmed,", ",confirmed", "Confirmed", "confirmed,,invalid", "private-key"} {
		for _, field := range []string{"status", "definitive", "freshness"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				q := app.CandidateQuery{}
				switch field {
				case "status":
					q.Status = []string{value}
				case "definitive":
					q.Definitive = []string{value}
				default:
					q.Freshness = []string{value}
				}
				_, err := q.Normalize()
				require.ErrorIs(t, err, app.ErrCandidateQuery)
				require.NotContains(t, err.Error(), "private-key")
			})
		}
	}
}

// covers: spec 0006 AC-2, AC-3, AC-4. Filters intersect and expiry uses one clock capture.
func TestQueryCandidates_FiltersRetainedEvidenceAtFreshnessBoundaries(t *testing.T) {
	a := crawlApp(t)
	seedQuerySnapshot(t, a.Settings.StateDir)
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	run, err := w.StartRun(t.Context(), state.RunInput{
		RequestID: "filter-evidence", Kind: state.Validation, StartedAt: crawlTime,
	})
	require.NoError(t, err)
	for _, id := range []int64{1, 2, 3, 338} {
		observed := crawlTime.Add(-time.Hour)
		switch id {
		case 1, 338:
			observed = observed.Add(time.Microsecond)
		case 3:
			observed = crawlTime.Add(time.Microsecond)
		}
		for number, outcome := range []state.Outcome{state.Confirmed, state.Inconclusive} {
			attempt, err := w.BeginAttempt(t.Context(), state.AttemptInput{
				RequestID: fmt.Sprintf("filter-%d-%d", id, number), RunID: run.ID, CandidateID: id,
				BackendID: "fixture", BackendVersion: "1", StartedAt: observed,
			})
			require.NoError(t, err)
			completion := state.AttemptCompletion{
				AttemptID: attempt.ID, Outcome: outcome, FinishedAt: observed, ReasonCode: "fixture_result",
			}
			if outcome == state.Confirmed {
				completion.ReasonCode = ""
				completion.Evidence = &state.Evidence{
					Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: observed,
				}
			}
			_, err = w.CompleteAttempt(t.Context(), completion)
			require.NoError(t, err)
		}
	}
	for _, tt := range []struct {
		freshness string
		ids       []int64
	}{
		{"fresh", []int64{1, 338}}, {"stale", []int64{2}}, {"unknown", []int64{3}},
		{"not_applicable", []int64{}},
	} {
		t.Run(tt.freshness, func(t *testing.T) {
			calls := 0
			a.Clock = func() time.Time { calls++; return crawlTime.Add(999 * time.Nanosecond) }
			ids := []int64{}
			result, err := a.QueryCandidates(t.Context(), app.CandidateQuery{
				Status: []string{"inconclusive,confirmed"}, Definitive: []string{"confirmed"},
				Freshness: []string{tt.freshness},
			}, func(c state.Candidate) error {
				ids = append(ids, c.ID)
				require.Equal(t, state.Inconclusive, c.LatestOutcome)
				require.Equal(t, state.Confirmed, c.LastDefinitiveAttempt.Outcome)
				require.Equal(t, tt.freshness == "unknown", c.ClockAnomaly)
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, tt.ids, ids)
			require.Equal(t, 1, calls)
			require.Equal(t, crawlTime, result.Evaluation.AsOf)
			require.Equal(t, int64(len(ids)), result.ReturnedCount)
			require.False(t, result.Truncated)
		})
	}
}

// covers: spec 0006 AC-8, AC-10. Failed callbacks are not counted or replaced with store errors.
func TestQueryCandidates_StopsOnCallbackFailureAndCancellation(t *testing.T) {
	a := crawlApp(t)
	seedQuerySnapshot(t, a.Settings.StateDir)
	sentinel := errors.New("caller preparation failed")
	for _, cancelDuring := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelDuring), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			result, err := a.QueryCandidates(ctx, app.CandidateQuery{}, func(state.Candidate) error {
				calls++
				if cancelDuring {
					cancel()
					return nil
				}
				return sentinel
			})
			require.Equal(t, 1, calls)
			if cancelDuring {
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, int64(1), result.ReturnedCount)
			} else {
				require.ErrorIs(t, err, sentinel)
				require.Zero(t, result.ReturnedCount)
			}
		})
	}
	calls := 0
	err := a.List(t.Context(), func(state.Candidate) error { calls++; return sentinel })
	require.EqualError(t, err, "output_error")
	require.Equal(t, 1, calls)
}

// Spec 0006 AC-1, AC-3, AC-4: later commits cannot change membership, summaries or the extra match probe.
func TestQuerySnapshotAcrossWriterProcess(t *testing.T) {
	for _, limit := range []int64{337, 338} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			a := crawlApp(t)
			seedQuerySnapshot(t, a.Settings.StateDir)
			var ids []int64
			result, err := a.QueryCandidates(t.Context(), app.CandidateQuery{
				Status: []string{"unchecked"}, Limit: &limit,
			}, func(c state.Candidate) error {
				ids = append(ids, c.ID)
				if c.ID == 100 {
					cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestQueryWriterProcess$")
					cmd.Env = append(os.Environ(), "WPC_QUERY_WRITER="+a.Settings.StateDir)
					output, err := cmd.CombinedOutput()
					require.NoError(t, err, "%s", output)
				}
				if c.ID == 250 {
					require.NotNil(t, c.UnresolvedAttempt, "writer recovery must not enter the old snapshot")
				}
				require.Equal(t, state.Unchecked, c.LatestOutcome)
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, limit, result.ReturnedCount)
			require.Equal(t, limit == 337, result.Truncated)
			for i, id := range ids {
				require.Equal(t, int64(i+1), id)
			}
			var current []state.Candidate
			result, err = a.QueryCandidates(t.Context(), app.CandidateQuery{}, func(c state.Candidate) error {
				current = append(current, c)
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, int64(339), result.ReturnedCount)
			require.Equal(t, state.Inconclusive, current[249].LatestOutcome)
			require.Nil(t, current[249].UnresolvedAttempt)
			require.Equal(t, state.Invalid, current[337].LatestOutcome)
			require.Equal(t, int64(339), current[338].ID)
		})
	}
}

func seedQuerySnapshot(t *testing.T, dir string) {
	t.Helper()
	w, err := store.OpenWriter(t.Context(), dir, func() time.Time { return crawlTime })
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: crawlTime})
	require.NoError(t, err)
	keys := make([]string, 338)
	for i := range keys {
		keys[i] = fmt.Sprintf("fixture-%04d", i+1)
	}
	_, err = w.RecordMessage(t.Context(), state.MessageInput{
		RunID: run.ID, SourceID: "fixture", MessageID: "1", ObservedAt: crawlTime, Keys: keys,
	})
	require.NoError(t, err)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{
		RunID: run.ID, Status: state.Completed, FinishedAt: crawlTime,
	})
	require.NoError(t, err)
	run, err = w.StartRun(t.Context(), state.RunInput{
		RequestID: "unfinished", Kind: state.Validation, StartedAt: crawlTime,
	})
	require.NoError(t, err)
	_, err = w.BeginAttempt(t.Context(), state.AttemptInput{
		RequestID: "active", RunID: run.ID, CandidateID: 250,
		BackendID: "fixture", BackendVersion: "1", StartedAt: crawlTime,
	})
	require.NoError(t, err)
}

func TestQueryWriterProcess(t *testing.T) {
	dir := os.Getenv("WPC_QUERY_WRITER")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	require.True(t, filepath.IsAbs(dir))
	w, err := store.OpenWriter(t.Context(), dir, func() time.Time { return crawlTime.Add(time.Minute) })
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	run, err := w.StartRun(t.Context(), state.RunInput{
		RequestID: "concurrent", Kind: state.Crawl, StartedAt: crawlTime,
	})
	require.NoError(t, err)
	_, err = w.RecordMessage(t.Context(), state.MessageInput{
		RunID: run.ID, SourceID: "fixture", MessageID: "new", ObservedAt: crawlTime,
		Keys: []string{"new-after-snapshot"},
	})
	require.NoError(t, err)
	run, err = w.StartRun(t.Context(), state.RunInput{
		RequestID: "new-evidence", Kind: state.Validation, StartedAt: crawlTime,
	})
	require.NoError(t, err)
	attempt, err := w.BeginAttempt(t.Context(), state.AttemptInput{
		RequestID: "rejection", RunID: run.ID, CandidateID: 338,
		BackendID: "fixture", BackendVersion: "1", StartedAt: crawlTime,
	})
	require.NoError(t, err)
	_, err = w.CompleteAttempt(t.Context(), state.AttemptCompletion{
		AttemptID: attempt.ID, Outcome: state.Invalid, FinishedAt: crawlTime,
		ReasonCode: "fixture_rejection", Evidence: &state.Evidence{
			Kind: state.Rejection, RejectionCode: "fixture_rejection", ObservedAt: crawlTime,
		},
	})
	require.NoError(t, err)
}
