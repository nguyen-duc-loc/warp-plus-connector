package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
)

func scheduleSeed(t *testing.T, w *Writer, at time.Time, keys ...string) []int64 {
	t.Helper()
	r, err := w.StartRun(t.Context(), state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: at})
	require.NoError(t, err)
	d, err := w.RecordMessage(t.Context(), state.MessageInput{
		RunID: r.ID, SourceID: "fixture", MessageID: "1", ObservedAt: at, Keys: keys,
	})
	require.NoError(t, err)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{RunID: r.ID, Status: state.Completed, FinishedAt: at})
	require.NoError(t, err)
	var ids []int64
	for _, candidate := range d.Discoveries {
		ids = append(ids, candidate.CandidateID)
	}
	return ids
}

// covers: spec 0007 AC-3, AC-5, AC-11, AC-13. Recovery delays only started work.
func TestSchedule_RecoveryPreservesPendingEligibilityAndOriginalBasis(t *testing.T) {
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "state")
	w, err := OpenWriter(t.Context(), dir, func() time.Time { return at })
	require.NoError(t, err)
	ids := scheduleSeed(t, w, at, "first", "second", "third")
	b, err := w.StartValidationBatch(t.Context(), state.ValidationBatchInput{
		RequestID: "batch", StartedAt: at, CandidateIDs: ids[:2], Origin: "scheduled",
		BackendID: "fixture", BackendVersion: "1", Policy: state.DefaultCheckPolicy(),
	})
	require.NoError(t, err)
	_, err = w.BeginBatchAttempt(t.Context(), state.BatchAttemptInput{
		AttemptInput: state.AttemptInput{
			RequestID: "attempt", RunID: b.Run.ID, CandidateID: ids[0], StartedAt: at,
			BackendID: "fixture", BackendVersion: "1",
		}, Number: 1,
	})
	require.NoError(t, err)
	s, err := w.GetSchedule(t.Context(), 0, func() time.Time { return at })
	require.NoError(t, err)
	require.Equal(t, int64(2), s.Candidates.Active)
	require.Equal(t, int64(1), s.Candidates.Due)
	due, err := w.DueCandidates(t.Context(), at, 8)
	require.NoError(t, err)
	require.Equal(t, ids[2:], due)
	require.NoError(t, w.Close())
	recoveredAt := at.Add(10 * time.Minute)
	for _, reopenAt := range []time.Time{recoveredAt, recoveredAt.Add(time.Minute)} {
		w, err = OpenWriter(t.Context(), dir, func() time.Time { return reopenAt })
		require.NoError(t, err)
		s, err = w.GetSchedule(t.Context(), ids[0], func() time.Time { return reopenAt })
		require.NoError(t, err)
		require.Equal(t, "failure", s.Candidate.Reason)
		require.Equal(t, recoveredAt, s.Candidate.BasisAt)
		require.Equal(t, recoveredAt.Add(time.Hour), s.Candidate.DueAt)
		require.Nil(t, s.Candidate.ActiveRunID)
		due, err = w.DueCandidates(t.Context(), reopenAt, 8)
		require.NoError(t, err)
		require.Equal(t, ids[1:], due)
		pending, err := w.GetSchedule(t.Context(), ids[1], func() time.Time { return reopenAt })
		require.NoError(t, err)
		require.Equal(t, "first_check", pending.Candidate.Reason)
		require.Equal(t, at, pending.Candidate.DueAt)
		require.NoError(t, w.Close())
	}
}

// covers: spec 0007 AC-5, AC-7, AC-8, AC-9. Reconciliation retains timing bases
// and never shortens a copied provider deadline.
func TestSchedule_WaitsSurviveRestartAndPolicyChanges(t *testing.T) {
	for _, reason := range []string{"backend_unavailable", "cleanup_pending", "provider_cooldown"} {
		t.Run(reason, func(t *testing.T) {
			at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
			dir := filepath.Join(t.TempDir(), "state")
			w, err := OpenWriter(t.Context(), dir, func() time.Time { return at })
			require.NoError(t, err)
			wait := &state.ValidationWait{
				Reason: reason, BasisAt: at, RetryAt: at.Add(time.Hour), ObservedAt: at, Interval: time.Hour,
			}
			require.NoError(t, w.SaveValidationWait(t.Context(), at, wait, true))
			require.NoError(t, w.Close())
			for i := 1; i <= 2; i++ {
				now := at.Add(time.Duration(i) * time.Minute)
				w, err = OpenWriter(t.Context(), dir, func() time.Time { return now })
				require.NoError(t, err)
				p := state.DefaultSchedulePolicy()
				p.Failure = 30 * time.Minute
				ok, err := w.PrepareSchedule(t.Context(), now, p, nil, true)
				require.NoError(t, err)
				require.True(t, ok)
				s, err := w.GetSchedule(t.Context(), 0, func() time.Time { return now })
				require.NoError(t, err)
				require.True(t, s.ValidationRecoveryPending)
				require.Equal(t, at, s.ValidationWait.BasisAt)
				require.Equal(t, at, s.ValidationWait.ObservedAt)
				want := at.Add(30 * time.Minute)
				if reason == "provider_cooldown" {
					want = wait.RetryAt
				}
				require.Equal(t, want, s.ValidationWait.RetryAt)
				require.NoError(t, w.Close())
			}
		})
	}
}

// covers: spec 0007 AC-4, AC-6. Manual outcomes choose the appropriate interval.
func TestSchedule_ManualTerminalOutcomesAndRediscovery(t *testing.T) {
	for _, outcome := range []state.Outcome{state.Confirmed, state.Invalid, state.Inconclusive} {
		t.Run(string(outcome), func(t *testing.T) {
			at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
			w, err := OpenWriter(t.Context(), filepath.Join(t.TempDir(), "state"), func() time.Time { return at })
			require.NoError(t, err)
			defer func() { require.NoError(t, w.Close()) }()
			id := scheduleSeed(t, w, at, "synthetic")[0]
			r, err := w.StartRun(t.Context(), state.RunInput{RequestID: "check", Kind: state.Validation, StartedAt: at})
			require.NoError(t, err)
			a, err := w.BeginAttempt(t.Context(), state.AttemptInput{
				RequestID: "attempt", RunID: r.ID, CandidateID: id, StartedAt: at,
				BackendID: "fixture", BackendVersion: "1",
			})
			require.NoError(t, err)
			finished := at.Add(time.Minute)
			in := state.AttemptCompletion{AttemptID: a.ID, FinishedAt: finished, Outcome: outcome}
			if outcome == state.Confirmed {
				in.Evidence = &state.Evidence{Kind: state.AccountType, ObservedAt: at, AccountType: "Unlimited"}
			} else {
				in.ReasonCode = "controlled_result"
			}
			if outcome == state.Invalid {
				in.Evidence = &state.Evidence{
					Kind: state.Rejection, ObservedAt: at, RejectionCode: "controlled_result",
				}
			}
			_, err = w.CompleteAttempt(t.Context(), in)
			require.NoError(t, err)
			before, err := w.GetSchedule(t.Context(), id, func() time.Time { return finished })
			require.NoError(t, err)
			reason, interval := "routine", 6*time.Hour
			if outcome == state.Inconclusive {
				reason, interval = "failure", time.Hour
			}
			require.Equal(t, reason, before.Candidate.Reason)
			require.Equal(t, finished.Add(interval), before.Candidate.DueAt)
			require.Equal(t, a.ID, *before.Candidate.BasisAttemptID)
			r, err = w.StartRun(t.Context(), state.RunInput{RequestID: "rediscover", Kind: state.Crawl, StartedAt: finished})
			require.NoError(t, err)
			_, err = w.RecordMessage(t.Context(), state.MessageInput{
				RunID: r.ID, SourceID: "fixture", MessageID: "2", ObservedAt: finished, Keys: []string{"synthetic"},
			})
			require.NoError(t, err)
			after, err := w.GetSchedule(t.Context(), id, func() time.Time { return finished })
			require.NoError(t, err)
			require.Equal(t, before.Candidate, after.Candidate)
		})
	}
}

// covers: spec 0007 AC-6. Only identical ordered coverage advances the crawl deadline.
func TestSchedule_ManualCoverageMustMatchWholePlan(t *testing.T) {
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	full := []state.SourcePlan{{Channel: "alpha", PageLimit: 1}, {Channel: "beta", PageLimit: 2}}
	for _, tc := range []struct {
		name    string
		plan    []state.SourcePlan
		matches bool
	}{
		{"exact", full, true}, {"subset", full[:1], false},
		{"reordered", []state.SourcePlan{full[1], full[0]}, false},
		{"different budget", []state.SourcePlan{{Channel: "alpha", PageLimit: 2}, full[1]}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, err := OpenWriter(t.Context(), filepath.Join(t.TempDir(), "state"), func() time.Time { return at })
			require.NoError(t, err)
			defer func() { require.NoError(t, w.Close()) }()
			_, err = w.PrepareSchedule(t.Context(), at, state.DefaultSchedulePolicy(), full, false)
			require.NoError(t, err)
			r, err := w.StartCrawlPlan(t.Context(), state.CrawlPlanInput{
				RequestID: "manual", StartedAt: at, Sources: tc.plan,
			})
			require.NoError(t, err)
			finished := at.Add(time.Minute)
			for _, source := range tc.plan {
				id := state.CrawlPageID{RunID: r.Run.ID, SourceID: "telegram:" + source.Channel, PageNumber: 1}
				_, err = w.StartCrawlRequest(t.Context(), state.CrawlRequest{CrawlPageID: id, RequestedAt: at})
				require.NoError(t, err)
				_, err = w.FinishCrawlPage(t.Context(), state.CrawlPageCompletion{
					CrawlPageID: id, Status: "failed", ReasonCode: "source_unavailable", FinishedAt: finished,
				})
				require.NoError(t, err)
				_, err = w.FinishCrawlSource(t.Context(), state.CrawlSourceCompletion{
					RunID: r.Run.ID, SourceID: id.SourceID, Status: "failed",
					StopReason: "source_unavailable", FinishedAt: finished,
				})
				require.NoError(t, err)
			}
			_, err = w.FinishCrawlPlan(t.Context(), state.RunCompletion{
				RunID: r.Run.ID, Status: state.Failed, ReasonCode: "source_failed", FinishedAt: finished,
			})
			require.NoError(t, err)
			s, err := w.GetSchedule(t.Context(), 0, func() time.Time { return finished })
			require.NoError(t, err)
			if tc.matches {
				require.Equal(t, finished.Add(time.Hour), s.Crawl.DueAt)
				require.Equal(t, "failed", s.Crawl.Reason)
				require.Equal(t, r.Run.ID, *s.Crawl.BasisRunID)
			} else {
				require.Equal(t, at, s.Crawl.DueAt)
				require.Equal(t, "initial", s.Crawl.Reason)
				require.Nil(t, s.Crawl.BasisRunID)
			}
		})
	}
}

// covers: spec 0007 AC-3, AC-11. Ordering uses due time before identity and limits are explicit.
func TestSchedule_DueOrderingAndReadValidation(t *testing.T) {
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	w, err := OpenWriter(t.Context(), filepath.Join(t.TempDir(), "state"), func() time.Time { return at })
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	ids := scheduleSeed(t, w, at, "first", "second", "third")
	run, err := w.StartRun(t.Context(), state.RunInput{
		RequestID: "older_observation", Kind: state.Crawl, StartedAt: at.Add(-time.Minute),
	})
	require.NoError(t, err)
	discovery, err := w.RecordMessage(t.Context(), state.MessageInput{
		RunID: run.ID, SourceID: "fixture", MessageID: "older", ObservedAt: at.Add(-time.Minute),
		Keys: []string{"older_due"},
	})
	require.NoError(t, err)
	olderID := discovery.Discoveries[0].CandidateID
	ordered := append([]int64{olderID}, ids...)
	for _, limit := range []int{0, -1, 9} {
		_, err := w.DueCandidates(t.Context(), at, limit)
		require.ErrorIs(t, err, ErrInvalidInput)
	}
	for _, id := range []int64{-1, 999} {
		_, err := w.GetSchedule(t.Context(), id, func() time.Time { return at })
		require.Error(t, err)
	}
	for _, limit := range []int{1, 2, 8} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			due, err := w.DueCandidates(t.Context(), at, limit)
			require.NoError(t, err)
			require.Equal(t, ordered[:min(limit, len(ordered))], due)
		})
	}
	due, err := w.DueCandidates(t.Context(), at.Add(-time.Microsecond), 8)
	require.NoError(t, err)
	require.Equal(t, []int64{olderID}, due)
}

// Spec 0007 AC-4, AC-7, AC-13: exact duration arithmetic and replay identity survive
// policy edits while evidence and the original terminal basis remain unchanged.
func TestScheduleCompletionReplayAndPolicyReconciliation(t *testing.T) {
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)
	p := state.DefaultSchedulePolicy()
	p.Routine = time.Hour + time.Nanosecond
	w, err := OpenWriterWithPolicy(ctx, filepath.Join(t.TempDir(), "state"), func() time.Time { return at }, p)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	crawl, err := w.StartRun(ctx, state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: at})
	require.NoError(t, err)
	discovery, err := w.RecordMessage(
		ctx, state.MessageInput{
			RunID: crawl.ID, SourceID: "fixture", MessageID: "1",
			ObservedAt: at, Keys: []string{"synthetic"},
		},
	)
	require.NoError(t, err)
	id := discovery.Discoveries[0].CandidateID
	validation, err := w.StartRun(ctx, state.RunInput{RequestID: "check", Kind: state.Validation, StartedAt: at})
	require.NoError(t, err)
	attempt, err := w.BeginAttempt(
		ctx, state.AttemptInput{
			RequestID: "attempt", RunID: validation.ID, CandidateID: id,
			StartedAt: at, BackendID: "fixture", BackendVersion: "1",
		},
	)
	require.NoError(t, err)
	completion := state.AttemptCompletion{
		SchedulePolicy: p, AttemptID: attempt.ID, FinishedAt: at, Outcome: state.Confirmed,
		Evidence: &state.Evidence{Kind: state.AccountType, ObservedAt: at, AccountType: "Unlimited"},
	}
	_, err = w.CompleteAttempt(ctx, completion)
	require.NoError(t, err)
	saved, err := w.GetSchedule(ctx, id, func() time.Time { return at })
	require.NoError(t, err)
	require.Equal(t, at.Add(time.Hour+time.Microsecond), saved.Candidate.DueAt)
	require.Equal(t, time.Hour+time.Nanosecond, saved.Candidate.Interval)
	p.Routine = 2 * time.Hour
	_, err = w.PrepareSchedule(ctx, at, p, nil, false)
	require.NoError(t, err)
	_, err = w.CompleteAttempt(ctx, completion)
	require.NoError(t, err)
	saved, err = w.GetSchedule(ctx, id, func() time.Time { return at })
	require.NoError(t, err)
	require.Equal(t, at.Add(2*time.Hour), saved.Candidate.DueAt)
	completion.SchedulePolicy = p
	_, err = w.CompleteAttempt(ctx, completion)
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, at, saved.Candidate.BasisAt)
}

// Spec 0007 AC-11: a writer commit during the clock callback cannot alter the
// already established inspection snapshot or create a false regression warning.
func TestScheduleSnapshotPrecedesClockSample(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond)
	w, err := OpenWriter(t.Context(), filepath.Join(t.TempDir(), "state"), func() time.Time { return at })
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	saved, err := w.GetSchedule(
		t.Context(), 0, func() time.Time {
			_, e := w.ObserveSchedule(t.Context(), at.Add(time.Minute))
			require.NoError(t, e)
			return at
		},
	)
	require.NoError(t, err)
	require.False(t, saved.ClockRegressed)
	require.Equal(t, at, saved.CheckpointAt)
	later, err := w.GetSchedule(t.Context(), 0, func() time.Time { return at })
	require.NoError(t, err)
	require.True(t, later.ClockRegressed)
}

// Spec 0007 AC-13: damaged provenance rolls back the migration and recovery together.
func TestScheduleMigrationFailurePreservesVersionFour(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "state")
	_, err := prepareDir(dir, true)
	require.NoError(t, err)
	f, err := privateFile(filepath.Join(dir, "state.db"), true)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	db, err := openDB(ctx, dir, false)
	require.NoError(t, err)
	require.NoError(t, migrate(ctx, db, []string{initialSchema, coverageSchema, planSchema, batchSchema}))
	_, err = db.ExecContext(ctx, "INSERT INTO candidates(key_text) VALUES('damaged_synthetic')")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	w, err := OpenWriter(ctx, dir, time.Now)
	require.Error(t, err)
	require.Nil(t, w)
	db, err = openDB(ctx, dir, true)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	var version, count int
	require.NoError(t, db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version))
	require.Equal(t, 4, version)
	require.NoError(
		t, db.QueryRowContext(
			ctx,
			"SELECT count(*) FROM sqlite_schema WHERE name='candidate_schedule'",
		).Scan(&count),
	)
	require.Zero(t, count)
}
