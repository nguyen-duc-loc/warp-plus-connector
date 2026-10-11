package app_test

import (
	"context"
	"errors"
	"fmt"
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
