package app_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/stretchr/testify/require"
)

type cancelRetryHandler struct {
	slog.Handler
	cancel context.CancelFunc
}

func (h cancelRetryHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "retry planned" {
		h.cancel()
	}
	return h.Handler.Handle(ctx, record)
}

// Spec 0005 AC-4 and AC-7: cancellation after a committed retry plan preserves the plan without retrying.
func TestBatchCancellationDuringSavedRetryWait(t *testing.T) {
	a := batchApp(t, 2)
	a.Settings.CheckPolicy.Workers = 1
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	a.Logger = slog.New(cancelRetryHandler{Handler: slog.NewTextHandler(io.Discard, nil), cancel: cancel})
	a.RetryDraw = func(low, _ time.Duration) time.Duration { return low }
	var calls atomic.Int32
	a.Validator = &batchFixture{run: func(context.Context, validator.Input, validator.Hooks) (validator.Result, error) {
		calls.Add(1)
		return validator.Result{
			Outcome: state.Inconclusive, ReasonCode: "validation_timeout", FailureClass: state.Temporary,
			CleanupProven: true, Removed: true,
		}, nil
	}}
	report, err := a.CheckBatch(ctx, []int64{1, 2}, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, state.Cancelled, report.Run.Status)
	require.Equal(t, 1, report.Totals.Completed)
	require.Equal(t, 1, report.Totals.Skipped)
	var attempts int
	require.NoError(t, a.ShowCheck(t.Context(), report.Run.ID, func(record state.CheckRecord) error {
		if record.Attempt != nil {
			attempts++
			require.Equal(t, 5*time.Second, record.Attempt.RetryWait)
			require.NotNil(t, record.Attempt.RetryEligibleAt)
		}
		if record.Item != nil {
			require.Equal(t, "cancelled", record.Item.StopReason)
		}
		return nil
	}))
	require.Equal(t, 1, attempts)
}

// Spec 0005 AC-3, AC-5, AC-7: the completion marker stops queued mutations before cleanup finishes.
func TestBatchThrottleCompletionStopsCompetingAdmission(t *testing.T) {
	a := batchApp(t, 3)
	var started, admitted, cleaned atomic.Int32
	bothStarted := make(chan struct{})
	requesting := make(chan struct{}, 2)
	a.Validator = &batchFixture{run: func(ctx context.Context, _ validator.Input, h validator.Hooks) (
		validator.Result, error,
	) {
		if started.Add(1) == 2 {
			close(bothStarted)
		}
		<-bothStarted
		requesting <- struct{}{}
		result := validator.Result{
			Outcome: state.Inconclusive, ReasonCode: "cancelled", FailureClass: state.Terminal,
		}
		if err := h.Admit(ctx, "register"); err == nil {
			admitted.Add(1)
			<-requesting
			<-requesting
			result.ReasonCode, result.FailureClass = "validation_rate_limited", state.Throttled
			if err := h.MutationDone(result, nil); err != nil {
				return result, err
			}
			// Classification must cancel active work before the final cleanup response exists.
			if ctx.Err() == nil {
				return validator.Result{}, context.DeadlineExceeded
			}
		}
		cleanup, cancel := h.CleanupContext()
		defer cancel()
		if err := cleanup.Err(); err != nil {
			return result, err
		}
		if err := h.Commit(func() error { cleaned.Add(1); return nil }); err != nil {
			return result, err
		}
		result.CleanupProven, result.Removed = true, true
		return result, nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	report, err := a.CheckBatch(ctx, []int64{1, 2, 3}, nil)
	require.EqualError(t, err, "validation_rate_limited")
	require.NoError(t, ctx.Err())
	require.Equal(t, int32(2), started.Load())
	require.Equal(t, int32(1), admitted.Load())
	require.Equal(t, int32(2), cleaned.Load())
	require.Equal(t, state.Failed, report.Run.Status)
	require.Equal(t, 2, report.Totals.Completed)
	require.Equal(t, 1, report.Totals.Skipped)
	require.Zero(t, report.Totals.Unresolved)
}

// Spec 0005 AC-4 and AC-7: an active deadline, not explicit cancellation, expires a queued mutation.
func TestBatchCandidateDeadlineWhileAwaitingMutation(t *testing.T) {
	a := batchApp(t, 1)
	a.Settings.CheckPolicy.CandidateTimeoutUS = int64(time.Minute / time.Microsecond)
	var reads atomic.Int32
	// Account for time consumed by preparation, then let the actual context timer expire.
	// The saved policy remains a valid one minute candidate budget.
	a.Monotonic = func() time.Time {
		if reads.Add(1) >= 2 {
			return time.Now().Add(time.Minute - 250*time.Millisecond)
		}
		return time.Now()
	}
	var admitted atomic.Int32
	a.Validator = &batchFixture{run: func(ctx context.Context, _ validator.Input, h validator.Hooks) (
		validator.Result, error,
	) {
		if err := h.Admit(ctx, "register"); err != nil {
			return validator.Result{}, err
		}
		admitted.Add(1)
		pause := validator.Result{FailureClass: state.Throttled, ServerDelay: time.Minute}
		if err := h.MutationDone(pause, nil); err != nil {
			return validator.Result{}, err
		}
		err := h.Admit(ctx, "attach")
		if err == nil {
			admitted.Add(1)
		}
		result := validator.Result{
			Outcome: state.Inconclusive, ReasonCode: "validation_timeout", FailureClass: state.Temporary,
			CleanupProven: true, Removed: true,
		}
		if ctx.Err() != context.DeadlineExceeded {
			return result, context.Canceled
		}
		return result, nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	report, err := a.CheckBatch(ctx, []int64{1}, nil)
	require.EqualError(t, err, "validation_batch_inconclusive")
	require.NoError(t, ctx.Err())
	require.Equal(t, int32(1), admitted.Load())
	require.Equal(t, 1, report.Totals.Inconclusive)
	require.NoError(t, a.ShowCheck(t.Context(), report.Run.ID, func(record state.CheckRecord) error {
		if record.Item != nil {
			require.Equal(t, "candidate_timeout", record.Item.StopReason)
			require.Equal(t, 1, record.Item.Attempts)
		}
		return nil
	}))
}
