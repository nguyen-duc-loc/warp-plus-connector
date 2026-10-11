package app_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/stretchr/testify/require"
)

type batchFixture struct {
	run func(context.Context, validator.Input, validator.Hooks) (validator.Result, error)
}

func (b *batchFixture) Identity() (string, string) { return "controlled_batch", "1" }
func (b *batchFixture) Check(context.Context, validator.Input) (validator.Result, error) {
	panic("batch bypass")
}
func (b *batchFixture) Acquire(context.Context) (validator.BatchOwner, error) { return b, nil }
func (b *batchFixture) Close() error                                          { return nil }
func (b *batchFixture) Run(ctx context.Context, in validator.Input, h validator.Hooks) (validator.Result, error) {
	return b.run(ctx, in, h)
}

func batchApp(t *testing.T, n int) *app.App {
	t.Helper()
	a := crawlApp(t)
	a.Clock = time.Now
	a.Settings.CheckPolicy = state.DefaultCheckPolicy()
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, time.Now)
	require.NoError(t, err)
	run, err := w.StartRun(
		t.Context(),
		state.RunInput{RequestID: "batch_seed", Kind: state.Crawl, StartedAt: time.Now()},
	)
	require.NoError(t, err)
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("%08d-12345678-AbCdEf01", i)
	}
	_, err = w.RecordMessage(
		t.Context(), state.MessageInput{
			RunID: run.ID, SourceID: "fixture", MessageID: "1",
			ObservedAt: time.Now(), Keys: keys,
		},
	)
	require.NoError(t, err)
	_, err = w.FinishRun(
		t.Context(),
		state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: time.Now()},
	)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return a
}

// AC-1, AC-6, AC-9: sealed order, deduplication, no unknown selection, and all pages after restart.
func TestBatchSelectionAndCompleteSavedHistory(t *testing.T) {
	a := batchApp(t, 338)
	a.Validator = validator.Unsupported{}
	_, err := a.CheckBatch(t.Context(), []int64{1, 999}, nil)
	require.ErrorIs(t, err, store.ErrNotFound)
	ids := []int64{338, 1, 338}
	for i := 2; i < 338; i++ {
		ids = append(ids, int64(i))
	}
	emitted := 0
	report, err := a.CheckBatch(
		t.Context(), ids, func(item state.ValidationItem) error {
			require.Equal(t, "completed", item.Status)
			require.Equal(t, 1, item.Attempts)
			emitted++
			return nil
		},
	)
	require.EqualError(t, err, "validation_batch_inconclusive")
	require.Equal(t, 338, emitted)
	require.Equal(t, 338, report.Totals.Selected)
	require.Equal(t, 338, report.Totals.Inconclusive)
	var selected []int64
	attempts := 0
	require.NoError(
		t, a.ShowCheck(
			t.Context(), report.Run.ID, func(record state.CheckRecord) error {
				if record.Item != nil {
					selected = append(selected, record.Item.CandidateID)
				}
				if record.Attempt != nil {
					attempts++
					require.Equal(t, state.Terminal, record.Attempt.FailureClass)
				}
				return nil
			},
		),
	)
	require.Len(t, selected, 338)
	require.Equal(t, []int64{338, 1, 2}, selected[:3])
	require.Equal(t, 338, attempts)
	r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	runs, err := r.ListRuns(t.Context(), state.Validation, state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, runs.Items, 1)
}

// AC-2, AC-3: attempts overlap while every mutation waits after prior completion.
func TestBatchOverlapAndCompletionPacing(t *testing.T) {
	a := batchApp(t, 2)
	var active, maximum atomic.Int32
	var mu sync.Mutex
	var starts, ends []time.Time
	b := &batchFixture{}
	b.run = func(ctx context.Context, _ validator.Input, h validator.Hooks) (validator.Result, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		for _, op := range []string{"register", "attach"} {
			if err := h.Admit(ctx, op); err != nil {
				return validator.Result{}, err
			}
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			ends = append(ends, time.Now())
			mu.Unlock()
			if err := h.MutationDone(validator.Result{}, nil); err != nil {
				return validator.Result{}, err
			}
		}
		result := validator.Result{
			Outcome: state.Confirmed, CleanupProven: true, Removed: true,
			Evidence: &state.Evidence{Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: time.Now()},
		}
		if err := h.Observe(result); err != nil {
			return result, err
		}
		time.Sleep(50 * time.Millisecond)
		return result, nil
	}
	a.Validator = b
	report, err := a.CheckBatch(t.Context(), []int64{1, 2, 1}, nil)
	require.NoError(t, err)
	require.Equal(t, int32(2), maximum.Load())
	require.Equal(t, 2, report.Totals.Confirmed)
	require.Len(t, starts, 4)
	for i := 1; i < len(starts); i++ {
		require.GreaterOrEqual(t, starts[i].Sub(ends[i-1]), time.Second)
	}
}

// AC-4, AC-6: a retry releases its slot, preserves its saved draw, and stops at the finite limit.
func TestBatchRetryReleasesCapacityAndRetainsPlan(t *testing.T) {
	a := batchApp(t, 2)
	a.Settings.CheckPolicy.Workers = 1
	a.Settings.CheckPolicy.MaxAttempts = 2
	var offset atomic.Int64
	a.Monotonic = func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
	a.RetryDraw = func(low, high time.Duration) time.Duration { return low }
	var mu sync.Mutex
	var order []int64
	planned := make(chan struct{})
	a.Validator = &batchFixture{
		run: func(_ context.Context, in validator.Input, _ validator.Hooks) (validator.Result, error) {
			mu.Lock()
			order = append(order, in.CandidateID)
			count := len(order)
			mu.Unlock()
			if count == 2 {
				close(planned)
			}
			return validator.Result{
				Outcome: state.Inconclusive, ReasonCode: "validation_timeout", FailureClass: state.Temporary,
				CleanupProven: true, Removed: true,
			}, nil
		},
	}
	go func() { <-planned; time.Sleep(50 * time.Millisecond); offset.Add(int64(11 * time.Second)) }()
	report, err := a.CheckBatch(t.Context(), []int64{1, 2}, nil)
	require.EqualError(t, err, "validation_batch_inconclusive")
	require.Equal(t, []int64{1, 2, 1, 2}, order)
	plans, attempts := 0, 0
	require.NoError(
		t, a.ShowCheck(
			t.Context(), report.Run.ID, func(record state.CheckRecord) error {
				if record.Attempt != nil {
					attempts++
					if record.Attempt.RetryEligibleAt != nil {
						plans++
						require.Equal(t, 5*time.Second, record.Attempt.RetryWait)
					}
				}
				if record.Item != nil {
					require.Equal(t, "attempt_limit", record.Item.StopReason)
				}
				return nil
			},
		),
	)
	require.Equal(t, 2, plans)
	require.Equal(t, 4, attempts)
}

// AC-7, AC-9: an output failure stops callbacks and safely closes the remaining selection.
func TestBatchOutputFailureStopsWork(t *testing.T) {
	a := batchApp(t, 10)
	a.Settings.CheckPolicy.Workers = 1
	a.Validator = validator.Unsupported{}
	calls := 0
	report, err := a.CheckBatch(
		t.Context(), []int64{1, 2, 3, 4, 5}, func(state.ValidationItem) error {
			calls++
			return errors.New("sink stopped")
		},
	)
	require.EqualError(t, err, "output_error")
	require.Equal(t, 1, calls)
	require.Equal(t, state.Failed, report.Run.Status)
	require.Equal(t, 4, report.Totals.Skipped)
}

// AC-5: missing server delay stops the batch; a trusted delay cannot be shortened to fit a candidate budget.
func TestBatchThrottlePolicy(t *testing.T) {
	for _, delay := range []time.Duration{0, 20 * time.Minute} {
		t.Run(
			delay.String(), func(t *testing.T) {
				a := batchApp(t, 2)
				a.Settings.CheckPolicy.Workers = 1
				calls := 0
				a.Validator = &batchFixture{
					run: func(ctx context.Context, _ validator.Input, h validator.Hooks) (validator.Result, error) {
						calls++
						if err := h.Admit(ctx, "register"); err != nil {
							return validator.Result{}, err
						}
						result := validator.Result{
							Outcome: state.Inconclusive, ReasonCode: "validation_rate_limited",
							FailureClass: state.Throttled, ServerDelay: delay, CleanupProven: true, Removed: true,
						}
						if err := h.MutationDone(result, nil); err != nil {
							return result, err
						}
						return result, nil
					},
				}
				ids := []int64{1}
				if delay == 0 {
					ids = append(ids, 2)
				}
				report, err := a.CheckBatch(t.Context(), ids, nil)
				require.Error(t, err)
				require.Equal(t, 1, calls)
				if delay == 0 {
					require.Equal(t, state.Failed, report.Run.Status)
					require.Equal(t, 1, report.Totals.Skipped)
				} else {
					require.Equal(t, state.Completed, report.Run.Status)
					require.NoError(
						t, a.ShowCheck(
							t.Context(), report.Run.ID, func(record state.CheckRecord) error {
								if record.Item != nil {
									require.Equal(t, "candidate_timeout", record.Item.StopReason)
								}
								if record.Attempt != nil {
									require.Nil(t, record.Attempt.RetryEligibleAt)
								}
								return nil
							},
						),
					)
				}
			},
		)
	}
}

// AC-6, AC-7: evidence received before a stop survives proven cleanup; late evidence does not.
func TestBatchEvidenceEligibilityAcrossCancellation(t *testing.T) {
	for _, early := range []bool{true, false} {
		t.Run(
			fmt.Sprint(early), func(t *testing.T) {
				a := batchApp(t, 1)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				a.Validator = &batchFixture{
					run: func(_ context.Context, _ validator.Input, h validator.Hooks) (validator.Result, error) {
						result := validator.Result{
							Outcome: state.Confirmed, CleanupProven: true, Removed: true,
							Evidence: &state.Evidence{
								Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: time.Now(),
							},
						}
						if early {
							if err := h.Observe(result); err != nil {
								return result, err
							}
						}
						cancel()
						if !early {
							if err := h.Observe(result); err != nil {
								return result, err
							}
						}
						return result, nil
					},
				}
				report, err := a.CheckBatch(ctx, []int64{1}, nil)
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, state.Cancelled, report.Run.Status)
				if early {
					require.Equal(t, 1, report.Totals.Confirmed)
				} else {
					require.Equal(t, 1, report.Totals.Inconclusive)
				}
			},
		)
	}
}

type retryClockHandler struct {
	slog.Handler
	advance func(time.Duration)
}

func (h retryClockHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "retry planned" {
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "wait" {
				h.advance(attr.Value.Duration())
			}
			return true
		})
	}
	return h.Handler.Handle(ctx, record)
}

// Move the injected clock only after the retry plan has committed.
func advanceBatchRetryClock(a *app.App) {
	base := time.Now().UTC().Truncate(time.Microsecond)
	var elapsed atomic.Int64
	a.Clock = func() time.Time { return base.Add(time.Duration(elapsed.Load())) }
	a.Monotonic = a.Clock
	a.Logger = slog.New(retryClockHandler{
		Handler: slog.NewTextHandler(io.Discard, nil),
		advance: func(wait time.Duration) { elapsed.Add(int64(wait)) },
	})
}

// covers: spec 0005 AC-6. Definitive results stop immediately, including a capable backend's rejection.
func TestCheckBatch_DefinitiveResultsNeverRetry(t *testing.T) {
	for _, outcome := range []state.Outcome{state.Confirmed, state.Invalid} {
		t.Run(string(outcome), func(t *testing.T) {
			a := batchApp(t, 1)
			calls := 0
			a.Validator = &batchFixture{run: func(context.Context, validator.Input, validator.Hooks) (
				validator.Result, error,
			) {
				calls++
				result := validator.Result{
					Outcome: outcome, CleanupProven: true, Removed: true,
					Evidence: &state.Evidence{
						Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: time.Now(),
					},
				}
				if outcome == state.Invalid {
					result.ReasonCode = "license_rejected"
					result.Evidence.Kind = state.Rejection
					result.Evidence.AccountType = ""
					result.Evidence.RejectionCode = "license_rejected"
				}
				return result, nil
			}}
			report, err := a.CheckBatch(t.Context(), []int64{1}, nil)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.NoError(t, a.ShowCheck(t.Context(), report.Run.ID, func(record state.CheckRecord) error {
				if record.Item != nil {
					require.Empty(t, record.Item.StopReason)
					require.Equal(t, 1, record.Item.Attempts)
				}
				if record.Attempt != nil {
					require.Equal(t, outcome, record.Attempt.Attempt.Outcome)
					require.Empty(t, record.Attempt.FailureClass)
					require.Nil(t, record.Attempt.RetryEligibleAt)
				}
				return nil
			}))
		})
	}
}

// covers: spec 0005 AC-9, AC-10. Reader failures stop at the failed record without changing history.
func TestShowCheck_OutputFailureStopsAtEveryRecordKind(t *testing.T) {
	a := batchApp(t, 2)
	a.Validator = validator.Unsupported{}
	report, err := a.CheckBatch(t.Context(), []int64{1, 2}, nil)
	require.EqualError(t, err, "validation_batch_inconclusive")
	for _, failAt := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			calls := 0
			err := a.ShowCheck(t.Context(), report.Run.ID, func(state.CheckRecord) error {
				calls++
				if calls == failAt {
					return errors.New("private sink detail")
				}
				return nil
			})
			require.EqualError(t, err, "output_error")
			require.Equal(t, failAt, calls)
		})
	}
	require.NoError(t, a.ShowCheck(t.Context(), report.Run.ID, func(record state.CheckRecord) error {
		if record.Header != nil {
			require.Equal(t, report, *record.Header)
		}
		return nil
	}))
}

// covers: spec 0005 AC-4, AC-5. Server delays exceed the jitter cap and never shrink to fit a budget.
func TestCheckBatch_ServerDelayAndExactBudgetBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		delay time.Duration
		limit time.Duration
		calls int
	}{
		{"delay exceeds jitter cap", 90 * time.Second, 15 * time.Minute, 2},
		{"delay equals candidate budget", time.Minute, time.Minute, 1},
		{"delay exceeds candidate budget", 61 * time.Second, time.Minute, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := batchApp(t, 1)
			a.Settings.CheckPolicy.CandidateTimeoutUS = tc.limit.Microseconds()
			a.Settings.CheckPolicy.MaxAttempts = 2
			advanceBatchRetryClock(a)
			a.RetryDraw = func(low, _ time.Duration) time.Duration { return low }
			calls := 0
			a.Validator = &batchFixture{run: func(context.Context, validator.Input, validator.Hooks) (
				validator.Result, error,
			) {
				calls++
				return validator.Result{
					Outcome: state.Inconclusive, ReasonCode: "validation_rate_limited", FailureClass: state.Throttled,
					ServerDelay: tc.delay, CleanupProven: true, Removed: true,
				}, nil
			}}
			report, err := a.CheckBatch(t.Context(), []int64{1}, nil)
			require.EqualError(t, err, "validation_batch_inconclusive")
			require.Equal(t, tc.calls, calls)
			require.NoError(t, a.ShowCheck(t.Context(), report.Run.ID, func(record state.CheckRecord) error {
				if record.Item != nil {
					want := "candidate_timeout"
					if tc.calls == 2 {
						want = "attempt_limit"
					}
					require.Equal(t, want, record.Item.StopReason)
				}
				if record.Attempt != nil {
					if tc.calls == 2 && record.Attempt.Number == 1 {
						require.Equal(t, tc.delay, record.Attempt.RetryWait)
					} else {
						require.Zero(t, record.Attempt.RetryWait)
						require.Nil(t, record.Attempt.RetryEligibleAt)
					}
				}
				return nil
			}))
		})
	}
}

// covers: spec 0005 AC-4, AC-6. Expiration creates no retry, with attempt exhaustion taking precedence.
func TestCheckBatch_ExpiredBudgetStopReason(t *testing.T) {
	for _, limit := range []int{1, 2} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			a := batchApp(t, 1)
			a.Settings.CheckPolicy.CandidateTimeoutUS = time.Minute.Microseconds()
			a.Settings.CheckPolicy.MaxAttempts = limit
			base := time.Now()
			var elapsed atomic.Int64
			a.Monotonic = func() time.Time { return base.Add(time.Duration(elapsed.Load())) }
			calls := 0
			a.Validator = &batchFixture{run: func(context.Context, validator.Input, validator.Hooks) (
				validator.Result, error,
			) {
				calls++
				elapsed.Store(int64(time.Minute))
				return validator.Result{
					Outcome: state.Inconclusive, ReasonCode: "validation_timeout", FailureClass: state.Temporary,
					CleanupProven: true, Removed: true,
				}, nil
			}}
			report, err := a.CheckBatch(t.Context(), []int64{1}, nil)
			require.EqualError(t, err, "validation_batch_inconclusive")
			require.Equal(t, 1, calls)
			require.NoError(t, a.ShowCheck(t.Context(), report.Run.ID, func(record state.CheckRecord) error {
				if record.Item != nil {
					want := "candidate_timeout"
					if limit == 1 {
						want = "attempt_limit"
					}
					require.Equal(t, want, record.Item.StopReason)
				}
				return nil
			}))
		})
	}
}

// covers: spec 0005 AC-6. Exhausted retries cannot erase or refresh a previous confirmation.
func TestCheckBatch_ExhaustionPreservesDefinitiveEvidence(t *testing.T) {
	a := batchApp(t, 1)
	advanceBatchRetryClock(a)
	observed := a.Clock()
	a.Validator = &batchFixture{run: func(context.Context, validator.Input, validator.Hooks) (
		validator.Result, error,
	) {
		return validator.Result{
			Outcome: state.Confirmed, CleanupProven: true, Removed: true,
			Evidence: &state.Evidence{Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: observed},
		}, nil
	}}
	confirmed, err := a.Check(t.Context(), 1)
	require.NoError(t, err)
	a.RetryDraw = func(low, _ time.Duration) time.Duration { return low }
	a.Validator = &batchFixture{run: func(context.Context, validator.Input, validator.Hooks) (
		validator.Result, error,
	) {
		return validator.Result{
			Outcome: state.Inconclusive, ReasonCode: "validation_timeout", FailureClass: state.Temporary,
			CleanupProven: true, Removed: true,
		}, nil
	}}
	latest, err := a.Check(t.Context(), 1)
	require.EqualError(t, err, "validation_batch_inconclusive")
	r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	candidate, err := r.GetCandidate(t.Context(), 1, state.Evaluation{AsOf: a.Clock(), MaxAge: 10 * time.Second})
	require.NoError(t, err)
	require.Equal(t, confirmed, *candidate.LastDefinitiveAttempt)
	require.Equal(t, latest, *candidate.LatestAttempt)
	require.Equal(t, state.Stale, candidate.Freshness)
	history, err := r.ListAttempts(t.Context(), state.AttemptFilter{CandidateID: 1}, state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, history.Items, 4)
}

// covers: spec 0005 AC-1, AC-9. The backend and callbacks see committed selection, policy, and results.
func TestCheckBatch_StreamsCommittedResultsInCompletionOrder(t *testing.T) {
	a := batchApp(t, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	secondEmitted := make(chan struct{})
	snapshots := make(chan state.ValidationReport, 2)
	a.Validator = &batchFixture{run: func(ctx context.Context, in validator.Input, _ validator.Hooks) (
		validator.Result, error,
	) {
		r, err := store.OpenReader(ctx, a.Settings.StateDir)
		if err != nil {
			return validator.Result{}, err
		}
		snapshot, readErr := r.GetValidationReport(ctx, in.RunID)
		if err = errors.Join(readErr, r.Close()); err != nil {
			return validator.Result{}, err
		}
		snapshots <- snapshot
		if in.CandidateID == 1 {
			select {
			case <-secondEmitted:
			case <-ctx.Done():
				return validator.Result{}, ctx.Err()
			}
		}
		return validator.Result{
			Outcome: state.Confirmed, CleanupProven: true, Removed: true,
			Evidence: &state.Evidence{Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: time.Now()},
		}, nil
	}}
	var emitted []int64
	report, err := a.CheckBatch(ctx, []int64{1, 2}, func(item state.ValidationItem) error {
		r, err := store.OpenReader(ctx, a.Settings.StateDir)
		if err != nil {
			return err
		}
		saved, readErr := r.GetValidationItem(ctx, item.RunID, item.CandidateID)
		if err = errors.Join(readErr, r.Close()); err != nil {
			return err
		}
		require.Equal(t, item, saved)
		require.Equal(t, "completed", saved.Status)
		emitted = append(emitted, item.CandidateID)
		if item.CandidateID == 2 {
			close(secondEmitted)
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []int64{2, 1}, emitted)
	for range 2 {
		snapshot := <-snapshots
		require.Equal(t, 2, snapshot.Totals.Selected)
		require.Equal(t, a.Settings.CheckPolicy, *snapshot.Policy)
	}
	// A fresh application reads the stored policy even after current settings change.
	savedPolicy := a.Settings.CheckPolicy
	restarted := &app.App{Settings: a.Settings}
	restarted.Settings.CheckPolicy.Workers = 4
	var selection []int64
	require.NoError(t, restarted.ShowCheck(t.Context(), report.Run.ID, func(record state.CheckRecord) error {
		if record.Header != nil {
			require.Equal(t, savedPolicy, *record.Header.Policy)
		}
		if record.Item != nil {
			selection = append(selection, record.Item.CandidateID)
		}
		return nil
	}))
	require.Equal(t, []int64{1, 2}, selection)
}

// covers: spec 0005 AC-2. Every configured ceiling fills, and duplicate IDs never run twice.
func TestCheckBatch_WorkerCeilingsAndUniqueCandidates(t *testing.T) {
	for _, workers := range []int{1, 2, 3, 4} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			a := batchApp(t, workers*3)
			a.Settings.CheckPolicy.Workers = workers
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			started := make(chan struct{}, workers*3)
			release := make(chan struct{})
			var active, maximum atomic.Int32
			var mu sync.Mutex
			calls := map[int64]int{}
			a.Validator = &batchFixture{run: func(ctx context.Context, in validator.Input, _ validator.Hooks) (
				validator.Result, error,
			) {
				n := active.Add(1)
				defer active.Add(-1)
				for old := maximum.Load(); n > old; old = maximum.Load() {
					if maximum.CompareAndSwap(old, n) {
						break
					}
				}
				mu.Lock()
				calls[in.CandidateID]++
				mu.Unlock()
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return validator.Result{}, ctx.Err()
				}
				return validator.Result{
					Outcome: state.Inconclusive, ReasonCode: "validation_unsupported", FailureClass: state.Terminal,
					CleanupProven: true, Removed: true,
				}, nil
			}}
			controllerDone := make(chan struct{})
			go func() {
				defer close(controllerDone)
				for wave := 0; wave < 3; wave++ {
					for i := 0; i < workers; i++ {
						select {
						case <-started:
						case <-ctx.Done():
							return
						}
					}
					for i := 0; i < workers; i++ {
						select {
						case release <- struct{}{}:
						case <-ctx.Done():
							return
						}
					}
				}
			}()
			var ids []int64
			for id := 1; id <= workers*3; id++ {
				ids = append(ids, int64(id), int64(id))
			}
			report, err := a.CheckBatch(ctx, ids, nil)
			cancel()
			<-controllerDone
			require.EqualError(t, err, "validation_batch_inconclusive")
			require.Equal(t, int32(workers), maximum.Load())
			require.Zero(t, active.Load())
			require.Equal(t, workers*3, report.Totals.Completed)
			require.Len(t, calls, workers*3)
			for _, count := range calls {
				require.Equal(t, 1, count)
			}
		})
	}
}

// covers: spec 0005 AC-4, AC-6, AC-9. Both jitter endpoints grow and cap at the saved attempt limit.
func TestCheckBatch_RetryBoundsAndAttemptLimits(t *testing.T) {
	for _, limit := range []int{1, 3, 5} {
		for _, upper := range []bool{false, true} {
			t.Run(fmt.Sprintf("attempts_%d_upper_%t", limit, upper), func(t *testing.T) {
				a := batchApp(t, 1)
				a.Settings.CheckPolicy.MaxAttempts = limit
				advanceBatchRetryClock(a)
				var ranges [][2]time.Duration
				a.RetryDraw = func(low, high time.Duration) time.Duration {
					ranges = append(ranges, [2]time.Duration{low, high})
					if upper {
						return high
					}
					return low
				}
				var attemptIDs []int64
				a.Validator = &batchFixture{run: func(_ context.Context, in validator.Input, _ validator.Hooks) (
					validator.Result, error,
				) {
					attemptIDs = append(attemptIDs, in.AttemptID)
					return validator.Result{
						Outcome: state.Inconclusive, ReasonCode: "validation_timeout", FailureClass: state.Temporary,
						CleanupProven: true, Removed: true,
					}, nil
				}}
				report, err := a.CheckBatch(t.Context(), []int64{1}, nil)
				require.EqualError(t, err, "validation_batch_inconclusive")
				require.Len(t, attemptIDs, limit)
				wantRanges := [][2]time.Duration{
					{5 * time.Second, 10 * time.Second}, {10 * time.Second, 20 * time.Second},
					{20 * time.Second, 40 * time.Second}, {40 * time.Second, 60 * time.Second},
				}
				require.Equal(t, wantRanges[:limit-1], append([][2]time.Duration{}, ranges...))
				seen := map[int64]bool{}
				for _, id := range attemptIDs {
					require.False(t, seen[id])
					seen[id] = true
				}
				attempts := 0
				require.NoError(t, a.ShowCheck(t.Context(), report.Run.ID, func(record state.CheckRecord) error {
					if record.Item != nil {
						require.Equal(t, "attempt_limit", record.Item.StopReason)
						require.Equal(t, limit, record.Item.Attempts)
					}
					if record.Attempt != nil {
						detail := record.Attempt
						attempts++
						require.Equal(t, attempts, detail.Number)
						require.Equal(t, state.Inconclusive, detail.Attempt.Outcome)
						if attempts == limit {
							require.Zero(t, detail.RetryWait)
							require.Nil(t, detail.RetryEligibleAt)
						} else {
							endpoint := 0
							if upper {
								endpoint = 1
							}
							require.Equal(t, wantRanges[attempts-1][endpoint], detail.RetryWait)
							require.NotNil(t, detail.RetryEligibleAt)
							require.Equal(t, detail.Attempt.FinishedAt.Add(detail.RetryWait), *detail.RetryEligibleAt)
						}
					}
					return nil
				}))
				require.Equal(t, limit, attempts)
			})
		}
	}
}

// covers: spec 0005 AC-4, AC-6. Diagnostic wording cannot grant permission to retry.
func TestCheckBatch_TerminalAndUnclassifiedResultsDoNotRetry(t *testing.T) {
	for _, tc := range []struct {
		reason string
		class  state.FailureClass
	}{
		{"validation_free", state.Terminal}, {"validation_device_limit", state.Terminal},
		{"validation_unsupported", state.Terminal}, {"validation_provider_error", state.Terminal},
		{"validation_unattributed", state.Terminal}, {"validation_timeout", ""},
		{"validation_observation_timeout", ""},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			a := batchApp(t, 2)
			a.Settings.CheckPolicy.Workers = 1
			var calls atomic.Int32
			a.Validator = &batchFixture{run: func(context.Context, validator.Input, validator.Hooks) (
				validator.Result, error,
			) {
				calls.Add(1)
				return validator.Result{
					Outcome: state.Inconclusive, ReasonCode: tc.reason, FailureClass: tc.class,
					CleanupProven: true, Removed: true,
				}, nil
			}}
			report, err := a.CheckBatch(t.Context(), []int64{1, 2}, nil)
			require.EqualError(t, err, "validation_batch_inconclusive")
			require.Equal(t, state.Completed, report.Run.Status)
			require.Equal(t, int32(2), calls.Load())
			require.NoError(t, a.ShowCheck(t.Context(), report.Run.ID, func(record state.CheckRecord) error {
				if record.Item != nil {
					require.Equal(t, 1, record.Item.Attempts)
					require.Equal(t, "nonretryable", record.Item.StopReason)
				}
				if record.Attempt != nil {
					require.Nil(t, record.Attempt.RetryEligibleAt)
					require.Zero(t, record.Attempt.RetryWait)
				}
				return nil
			}))
		})
	}
}

// covers: spec 0005 AC-4, AC-7. A retry needs both remote cleanup proof and container removal.
func TestCheckBatch_IncompleteCleanupStopsRemainingSelection(t *testing.T) {
	for _, cleaned := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup_proven_%t", cleaned), func(t *testing.T) {
			a := batchApp(t, 2)
			a.Settings.CheckPolicy.Workers = 1
			calls := 0
			a.Validator = &batchFixture{run: func(context.Context, validator.Input, validator.Hooks) (
				validator.Result, error,
			) {
				calls++
				return validator.Result{
					Outcome: state.Inconclusive, ReasonCode: "validation_timeout", FailureClass: state.Temporary,
					CleanupProven: cleaned, Removed: !cleaned,
				}, nil
			}}
			report, err := a.CheckBatch(t.Context(), []int64{1, 2}, nil)
			require.EqualError(t, err, "validation_cleanup_pending")
			require.Equal(t, 1, calls)
			require.Equal(t, state.Failed, report.Run.Status)
			require.Equal(t, 1, report.Totals.Skipped)
			require.NoError(t, a.ShowCheck(t.Context(), report.Run.ID, func(record state.CheckRecord) error {
				if record.Attempt != nil {
					require.Nil(t, record.Attempt.RetryEligibleAt)
				}
				return nil
			}))
		})
	}
}
