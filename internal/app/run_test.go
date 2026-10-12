package app_test

import (
	"context"
	"errors"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/stretchr/testify/require"
)

// covers: spec 0007 AC-14. Concurrent workers share one cleanup allowance,
// including when cleanup finishes before the maximum deadline.
func TestRun_CancellationSharesRemoteCleanupAllowance(t *testing.T) {
	for _, duration := range []time.Duration{40 * time.Second, 60 * time.Second} {
		t.Run(duration.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a := batchApp(t, 2)
				scheduledSettings(a)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var started atomic.Int32
				stoppedAt := make(chan time.Time, 1)
				finished := make(chan time.Time, 2)
				fixture := &scheduledFixture{}
				fixture.run = func(ctx context.Context, _ validator.Input, h validator.Hooks) (validator.Result, error) {
					if started.Add(1) == 2 {
						stoppedAt <- time.Now()
						cancel()
					}
					<-ctx.Done()
					cleanup, release := h.CleanupContext()
					defer release()
					if duration == 60*time.Second {
						<-cleanup.Done()
					} else {
						time.Sleep(duration)
						if cleanup.Err() != nil {
							return validator.Result{}, cleanup.Err()
						}
					}
					finished <- time.Now()
					return validator.Result{
						Outcome: state.Inconclusive, ReasonCode: "cancelled", FailureClass: state.Terminal,
						CleanupProven: true, Removed: true,
					}, ctx.Err()
				}
				a.Validator = fixture
				err := a.Run(ctx, nil)
				require.ErrorIs(t, err, context.Canceled)
				stop := <-stoppedAt
				for range 2 {
					require.Equal(t, duration, (<-finished).Sub(stop))
				}
				require.LessOrEqual(t, time.Since(stop), duration+5*time.Second)
				saved, err := a.ShowSchedule(t.Context(), 0)
				require.NoError(t, err)
				require.Zero(t, saved.Candidates.Active)
				require.Equal(t, int64(2), saved.Candidates.Waiting)
			})
		})
	}
}

// Spec 0008 AC-7: active crawl and validation share the existing cleanup budget.
// Another cancellation cannot extend it, and finalization has only five seconds.
func TestRun_CancellationBoundsBothActiveLanes(t *testing.T) {
	synctest.Test(
		t, func(t *testing.T) {
			a := batchApp(t, 2)
			scheduledSettings(a)
			a.Settings.CrawlEnabled = true
			source := &cancelledScheduledSource{started: make(chan struct{})}
			a.Source = source
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var workers atomic.Int32
			stopped := make(chan time.Time, 1)
			finished := make(chan time.Time, 2)
			fixture := &scheduledFixture{}
			fixture.run = func(ctx context.Context, _ validator.Input, h validator.Hooks) (validator.Result, error) {
				<-source.started
				if workers.Add(1) == 2 {
					stopped <- time.Now()
					cancel()
					time.AfterFunc(30*time.Second, cancel)
				}
				<-ctx.Done()
				cleanup, release := h.CleanupContext()
				defer release()
				<-cleanup.Done()
				finished <- time.Now()
				return validator.Result{
					Outcome: state.Inconclusive, ReasonCode: "cancelled", FailureClass: state.Terminal,
					CleanupProven: true, Removed: true,
				}, ctx.Err()
			}
			a.Validator = fixture
			var finalDeadline time.Time
			a.Finalize = func(ctx context.Context, _ error) error {
				var ok bool
				finalDeadline, ok = ctx.Deadline()
				require.True(t, ok)
				return nil
			}
			var summaries []app.RunSummary
			err := a.Run(ctx, func(s app.RunSummary) error { summaries = append(summaries, s); return nil })
			require.ErrorIs(t, err, context.Canceled)
			stop := <-stopped
			for range 2 {
				require.Equal(t, 60*time.Second, (<-finished).Sub(stop))
			}
			require.Equal(t, stop.Add(65*time.Second), finalDeadline)
			require.LessOrEqual(t, time.Since(stop), 65*time.Second)
			require.Equal(t, int32(1), source.calls.Load())
			require.Equal(t, int32(2), workers.Load())
			require.Len(t, summaries, 2)
			for _, summary := range summaries {
				if summary.Crawl != nil {
					require.NotEqual(t, state.Running, summary.Crawl.Run.Status)
				} else {
					require.NotEqual(t, state.Running, summary.Validation.Run.Status)
				}
			}
			saved, err := a.ShowSchedule(t.Context(), 0)
			require.NoError(t, err)
			require.Zero(t, saved.Candidates.Active)
		},
	)
}

type cancelledScheduledSource struct {
	started chan struct{}
	calls   atomic.Int32
}

func (s *cancelledScheduledSource) FetchPage(ctx context.Context, _ string, _ *int64) (telegram.Page, error) {
	if s.calls.Add(1) == 1 {
		close(s.started)
	}
	<-ctx.Done()
	return telegram.Page{}, ctx.Err()
}

// covers: spec 0007 AC-5, AC-7. A backward clock pauses due work across restart;
// a forward jump admits one new run rather than replaying missed occurrences.
func TestRun_ClockRepairCoalescesMissedCrawls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := crawlApp(t)
		scheduledSettings(a)
		a.Settings.CrawlEnabled, a.Settings.ValidationEnabled = true, false
		at := time.Now().UTC()
		now := at
		a.Clock = func() time.Time { return now }
		a.Source = emptyScheduledSource{clock: a.Clock}
		runOnce := func() int64 {
			t.Helper()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			var id int64
			err := a.Run(ctx, func(summary app.RunSummary) error {
				require.Zero(t, id)
				id = summary.Crawl.Run.ID
				cancel()
				return nil
			})
			require.ErrorIs(t, err, context.Canceled)
			return id
		}
		first := runOnce()
		now = at.Add(-time.Hour)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		err := a.Run(ctx, func(app.RunSummary) error { t.Error("clock regression admitted work"); return nil })
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
		saved, err := a.ShowSchedule(t.Context(), 0)
		require.NoError(t, err)
		require.True(t, saved.ClockRegressed)
		require.Equal(t, first, *saved.Crawl.BasisRunID)
		require.Equal(t, at.Add(time.Hour), saved.Crawl.DueAt)
		now = at.Add(24 * time.Hour)
		second := runOnce()
		require.Greater(t, second, first)
		saved, err = a.ShowSchedule(t.Context(), 0)
		require.NoError(t, err)
		require.Equal(t, now.Add(time.Hour), saved.Crawl.DueAt)
		ctx, cancel = context.WithTimeout(t.Context(), time.Minute)
		err = a.Run(ctx, func(app.RunSummary) error { t.Error("missed interval replayed"); return nil })
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

type probeFixture struct {
	batchFixture
	acquired int
	mu       sync.Mutex
	seen     []validator.Input
}

func (f *probeFixture) Acquire(context.Context) (validator.BatchOwner, error) {
	f.acquired++
	return f, nil
}
func (f *probeFixture) AcquireScheduled(context.Context, func() time.Time, time.Duration) (
	validator.Acquisition, error,
) {
	f.acquired++
	return validator.Acquisition{Owner: f, Probe: true, CleanupInspected: true}, nil
}
func (*probeFixture) Admission(time.Time) (bool, error)       { return true, nil }
func (*probeFixture) BeforeMutation(time.Time) error          { return nil }
func (*probeFixture) Begin(time.Time, time.Duration) error    { return nil }
func (*probeFixture) Throttle(time.Time, time.Duration) error { return nil }
func (*probeFixture) Resolve(time.Time, bool, bool) error     { return nil }
func (f *probeFixture) Run(_ context.Context, in validator.Input, _ validator.Hooks) (validator.Result, error) {
	f.mu.Lock()
	f.seen = append(f.seen, in)
	f.mu.Unlock()
	return validator.Result{
		Outcome: state.Inconclusive, ReasonCode: "controlled_terminal", FailureClass: state.Terminal,
		CleanupProven: true, Removed: true,
	}, nil
}

// covers: spec 0007 AC-3, AC-9. Scheduled probes reduce selection, workers, and
// attempts to one; manual probes still validate the entire requested selection.
func TestRun_ProbePolicyAndManualSelection(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		name := "manual"
		if scheduled {
			name = "scheduled"
		}
		t.Run(name, func(t *testing.T) {
			a := batchApp(t, 3)
			scheduledSettings(a)
			a.Settings.CheckPolicy.Workers = 4
			a.Settings.CheckPolicy.MaxAttempts = 5
			fixture := &probeFixture{}
			a.Validator = fixture
			var report state.ValidationReport
			if scheduled {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				err := a.Run(ctx, func(summary app.RunSummary) error {
					report = *summary.Validation
					cancel()
					return nil
				})
				require.ErrorIs(t, err, context.Canceled)
			} else {
				_, err := a.CheckBatch(t.Context(), []int64{1, 999}, nil)
				require.ErrorIs(t, err, store.ErrNotFound)
				require.Zero(t, fixture.acquired)
				_, err = a.CheckBatch(t.Context(), []int64{1, 2}, nil)
				require.EqualError(t, err, "validator_probe_required")
				r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
				require.NoError(t, err)
				runs, err := r.ListRuns(t.Context(), state.Validation, state.PageRequest{})
				require.NoError(t, err)
				require.Empty(t, runs.Items)
				require.NoError(t, r.Close())
				report, err = a.CheckBatch(t.Context(), []int64{1, 1}, nil)
				require.EqualError(t, err, "validation_batch_inconclusive")
			}
			require.Equal(t, 1, report.Totals.Selected)
			require.Equal(t, 1, report.Policy.Workers)
			require.Equal(t, 1, report.Policy.MaxAttempts)
			require.Len(t, fixture.seen, 1)
			require.Equal(t, int64(1), fixture.seen[0].CandidateID)
		})
	}
}

type acquisitionFixture struct {
	scheduledFixture
	acquire func(context.Context, func() time.Time, time.Duration) (validator.Acquisition, error)
}

func (f *acquisitionFixture) AcquireScheduled(
	ctx context.Context, clock func() time.Time, interval time.Duration,
) (validator.Acquisition, error) {
	f.acquisitions.Add(1)
	return f.acquire(ctx, clock, interval)
}

// covers: spec 0007 AC-5, AC-7, AC-8. Empty profiles retain recovery obligations
// and wait across restarts without fabricating candidate attempts.
func TestRun_EmptyProfileRecoveryHonorsSavedWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := crawlApp(t)
		scheduledSettings(a)
		at := time.Now().UTC()
		now := at
		a.Clock = func() time.Time { return now }
		fixture := &acquisitionFixture{}
		fixture.acquire = func(context.Context, func() time.Time, time.Duration) (validator.Acquisition, error) {
			return validator.Acquisition{Wait: &state.ValidationWait{
				Reason: "backend_unavailable", BasisAt: at, ObservedAt: at,
				RetryAt: at.Add(time.Hour), Interval: time.Hour,
			}}, &validator.Error{Code: "validator_backend_unavailable"}
		}
		a.Validator = fixture
		runBriefly := func() {
			t.Helper()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			err := a.Run(ctx, func(app.RunSummary) error {
				t.Error("recovery without candidates emitted a run")
				return nil
			})
			require.ErrorIs(t, err, context.DeadlineExceeded)
		}
		runBriefly()
		require.Equal(t, int64(1), fixture.acquisitions.Load())
		now = at.Add(time.Minute)
		a.Settings.FailureInterval = 30 * time.Minute
		runBriefly()
		require.Equal(t, int64(1), fixture.acquisitions.Load())
		saved, err := a.ShowSchedule(t.Context(), 0)
		require.NoError(t, err)
		require.True(t, saved.ValidationRecoveryPending)
		require.Equal(t, at, saved.ValidationWait.BasisAt)
		require.Equal(t, at.Add(30*time.Minute), saved.ValidationWait.RetryAt)
		fixture.acquire = func(context.Context, func() time.Time, time.Duration) (validator.Acquisition, error) {
			return validator.Acquisition{Owner: fixture, CleanupInspected: true}, nil
		}
		now = saved.ValidationWait.RetryAt
		runBriefly()
		require.Equal(t, int64(2), fixture.acquisitions.Load())
		saved, err = a.ShowSchedule(t.Context(), 0)
		require.NoError(t, err)
		require.False(t, saved.ValidationRecoveryPending)
		require.Nil(t, saved.ValidationWait)
		require.Zero(t, saved.Candidates.Total)
		r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
		require.NoError(t, err)
		defer func() { require.NoError(t, r.Close()) }()
		runs, err := r.ListRuns(t.Context(), state.Validation, state.PageRequest{})
		require.NoError(t, err)
		require.Empty(t, runs.Items)
	})
}

// covers: spec 0007 AC-8, AC-12. Permanent acquisition errors stop after one attempt.
func TestRun_PermanentBackendFailureDoesNotLoop(t *testing.T) {
	a := crawlApp(t)
	scheduledSettings(a)
	fixture := &acquisitionFixture{}
	fixture.acquire = func(context.Context, func() time.Time, time.Duration) (validator.Acquisition, error) {
		return validator.Acquisition{}, &validator.Error{Code: "validator_state_error"}
	}
	a.Validator = fixture
	err := a.Run(t.Context(), nil)
	require.EqualError(t, err, "validator_state_error")
	require.Equal(t, int64(1), fixture.acquisitions.Load())
	saved, err := a.ShowSchedule(t.Context(), 0)
	require.NoError(t, err)
	require.True(t, saved.ValidationRecoveryPending)
	require.Nil(t, saved.ValidationWait)
}

// covers: spec 0007 AC-12, AC-14. A failed summary cancels the other lane and
// never causes a second delivery, while both saved runs remain inspectable.
func TestRun_OutputFailureCancelsHeldValidation(t *testing.T) {
	a := batchApp(t, 1)
	scheduledSettings(a)
	a.Settings.CrawlEnabled = true
	started, crawled := make(chan struct{}), make(chan struct{})
	a.Source = overlapScheduledSource{started: started, crawled: crawled, clock: a.Clock}
	fixture := &scheduledFixture{}
	var cancelled atomic.Bool
	fixture.run = func(ctx context.Context, _ validator.Input, _ validator.Hooks) (validator.Result, error) {
		close(started)
		<-ctx.Done()
		cancelled.Store(true)
		return validator.Result{
			Outcome: state.Inconclusive, ReasonCode: "cancelled", FailureClass: state.Terminal,
			CleanupProven: true, Removed: true,
		}, ctx.Err()
	}
	a.Validator = fixture
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	count := 0
	err := a.Run(ctx, func(s app.RunSummary) error {
		count++
		require.NotNil(t, s.Crawl)
		require.Equal(t, state.Completed, s.Crawl.Run.Status)
		return errors.New("private_output_failure")
	})
	require.EqualError(t, err, "output_error")
	require.Equal(t, 1, count)
	require.True(t, cancelled.Load())
	require.NoError(t, ctx.Err())
	r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	for _, kind := range []state.RunKind{state.Crawl, state.Validation} {
		runs, err := r.ListRuns(t.Context(), kind, state.PageRequest{})
		require.NoError(t, err)
		require.NotEmpty(t, runs.Items)
		for _, run := range runs.Items {
			require.NotEqual(t, state.Running, run.Status)
		}
	}
}

type scheduledFixture struct {
	batchFixture
	acquisitions atomic.Int64
}

func (f *scheduledFixture) AcquireScheduled(context.Context, func() time.Time, time.Duration) (
	validator.Acquisition,
	error,
) {
	f.acquisitions.Add(1)
	return validator.Acquisition{Owner: f, CleanupInspected: true}, nil
}
func scheduledSettings(a *app.App) {
	a.Settings.CrawlInterval = time.Hour
	a.Settings.RecheckInterval = 6 * time.Hour
	a.Settings.FailureInterval = time.Hour
	a.Settings.ValidationEnabled = true
	a.Settings.Validator = "docker"
	a.Settings.ValidatorImage = "sha256:" + strings.Repeat("a", 64)
}

// Spec 0007 AC-3, AC-4, AC-10, AC-11: every due row is selected once in bounded batches,
// readers remain available, and competing writers cannot enter while the runner owns state.
func TestRunTraversesAll338Candidates(t *testing.T) {
	a := batchApp(t, 338)
	scheduledSettings(a)
	at := time.Now().UTC().Truncate(time.Microsecond)
	a.Clock = func() time.Time { return at }
	fixture := &scheduledFixture{}
	fixture.run = func(context.Context, validator.Input, validator.Hooks) (validator.Result, error) {
		return validator.Result{
			Outcome: state.Inconclusive, ReasonCode: "controlled_terminal", FailureClass: state.Terminal,
			CleanupProven: true, Removed: true,
		}, nil
	}
	a.Validator = fixture
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	selected, batches := 0, 0
	err := a.Run(
		ctx, func(result app.RunSummary) error {
			require.NotNil(t, result.Validation)
			r := result.Validation
			require.Equal(t, "scheduled", r.Run.Origin)
			require.LessOrEqual(t, r.Totals.Selected, 4)
			selected += r.Totals.Selected
			batches++
			saved, e := a.ShowSchedule(t.Context(), 0)
			require.NoError(t, e)
			require.Equal(t, int64(338), saved.Candidates.Total)
			require.Equal(t, int64(selected), saved.Candidates.Waiting)
			if batches == 1 {
				w, e := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
				require.ErrorIs(t, e, store.ErrBusy)
				require.Nil(t, w)
			}
			if selected == 338 {
				cancel()
			}
			return nil
		},
	)
	require.ErrorIs(t, err, context.Canceled,
		"selected=%d batches=%d acquisitions=%d", selected, batches, fixture.acquisitions.Load())
	require.Equal(t, 338, selected)
	require.Equal(t, 85, batches)
	saved, err := a.ShowSchedule(t.Context(), 338)
	require.NoError(t, err)
	require.Equal(t, at.Add(time.Hour), saved.Candidate.DueAt)
	require.Equal(t, "failure", saved.Candidate.Reason)
}

// Spec 0007 AC-1, AC-6, AC-12: the first scheduled crawl is persisted and a restart
// retains its future deadline instead of replaying missed interval jobs.
func TestRunCrawlPersistsAndRestartWaits(t *testing.T) {
	a := crawlApp(t)
	a.Source = emptyScheduledSource{clock: a.Clock}
	a.Settings.CrawlEnabled = true
	a.Settings.ValidationEnabled = false
	a.Settings.CrawlInterval = time.Hour
	a.Settings.FailureInterval = time.Hour
	at := a.Clock()
	ctx, cancel := context.WithCancel(t.Context())
	count := 0
	err := a.Run(
		ctx, func(result app.RunSummary) error {
			count++
			require.NotNil(t, result.Crawl)
			require.Equal(t, "scheduled", result.Crawl.Run.Origin)
			cancel()
			return nil
		},
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, count)
	saved, err := a.ShowSchedule(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, at.Add(time.Hour).UTC().Truncate(time.Microsecond), saved.Crawl.DueAt)
	ctx, cancel = context.WithTimeout(t.Context(), 80*time.Millisecond)
	defer cancel()
	err = a.Run(
		ctx,
		func(app.RunSummary) error { t.Error("future crawl dispatched early"); return errors.New("unexpected") },
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

type emptyScheduledSource struct{ clock func() time.Time }

func (s emptyScheduledSource) FetchPage(context.Context, string, *int64) (telegram.Page, error) {
	return telegram.Page{Facts: state.CrawlPage{FetchedAt: s.clock()}}, nil
}

// Spec 0007 AC-2: source retrieval progresses while a validation worker is held open.
func TestRunCrawlOverlapsValidation(t *testing.T) {
	a := batchApp(t, 1)
	scheduledSettings(a)
	a.Settings.CrawlEnabled = true
	started := make(chan struct{})
	crawled := make(chan struct{})
	a.Source = overlapScheduledSource{started: started, crawled: crawled, clock: a.Clock}
	fixture := &scheduledFixture{}
	fixture.run = func(ctx context.Context, _ validator.Input, _ validator.Hooks) (validator.Result, error) {
		close(started)
		select {
		case <-crawled:
		case <-ctx.Done():
			return validator.Result{}, ctx.Err()
		}
		return validator.Result{
			Outcome: state.Inconclusive, ReasonCode: "controlled_terminal", FailureClass: state.Terminal,
			CleanupProven: true, Removed: true,
		}, nil
	}
	a.Validator = fixture
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	count := 0
	err := a.Run(
		ctx, func(app.RunSummary) error {
			count++
			if count == 2 {
				cancel()
			}
			return nil
		},
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 2, count)
}

type overlapScheduledSource struct {
	started, crawled chan struct{}
	clock            func() time.Time
}

func (s overlapScheduledSource) FetchPage(ctx context.Context, _ string, _ *int64) (telegram.Page, error) {
	select {
	case <-s.started:
	case <-ctx.Done():
		return telegram.Page{}, ctx.Err()
	}
	close(s.crawled)
	return telegram.Page{Facts: state.CrawlPage{FetchedAt: s.clock()}}, nil
}
