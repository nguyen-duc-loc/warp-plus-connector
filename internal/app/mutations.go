package app

import (
	"context"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"sync"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

type mutationRequest struct {
	work  func() error
	reply chan error
	sql   bool
}

// One executor owns short durable mutations across both lanes. Once submitted,
// the caller waits for completion even if cancellation arrives.
type mutationExecutor struct {
	requests chan mutationRequest
	done     chan struct{}
	poison   error
	cancel   context.CancelCauseFunc
	once     sync.Once
}

func newMutationExecutor(cancel context.CancelCauseFunc) *mutationExecutor {
	m := &mutationExecutor{requests: make(chan mutationRequest, 6), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(m.done)
		for r := range m.requests {
			err := m.poison
			if !r.sql || err == nil {
				err = r.work()
			}
			if r.sql && err != nil && m.poison == nil {
				m.poison = err
				if m.cancel != nil {
					m.cancel(err)
				}
			}
			r.reply <- err
		}
	}()
	return m
}
func (m *mutationExecutor) close() { m.once.Do(func() { close(m.requests); <-m.done }) }
func (m *mutationExecutor) call(sql bool, work func() error) error {
	reply := make(chan error, 1)
	m.requests <- mutationRequest{work: work, reply: reply, sql: sql}
	return <-reply
}
func mutation[T any](m *mutationExecutor, fn func() (T, error)) (out T, err error) {
	err = m.call(true, func() error { var e error; out, e = fn(); return e })
	return
}

type operationWriter struct {
	failed bool
	*store.Writer
	mutations *mutationExecutor
	observe   func(any, error)
	creating  func()
}

func (w *operationWriter) StartCrawlPlan(ctx context.Context, in state.CrawlPlanInput) (state.CrawlReport, error) {
	if w.creating != nil {
		w.creating()
	}
	return operationMutation(w, func() (state.CrawlReport, error) { return w.Writer.StartCrawlPlan(ctx, in) })
}

func (w *operationWriter) StartCrawlRequest(ctx context.Context, in state.CrawlRequest) (state.CrawlPageRecord, error) {
	return operationMutation(w, func() (state.CrawlPageRecord, error) { return w.Writer.StartCrawlRequest(ctx, in) })
}

func (w *operationWriter) RecordCrawlParse(ctx context.Context, in state.CrawlParse) (state.CrawlPageRecord, error) {
	return operationMutation(w, func() (state.CrawlPageRecord, error) { return w.Writer.RecordCrawlParse(ctx, in) })
}

func (w *operationWriter) FinishCrawlPage(ctx context.Context, in state.CrawlPageCompletion) (
	state.CrawlPageRecord,
	error,
) {
	return operationMutation(w, func() (state.CrawlPageRecord, error) { return w.Writer.FinishCrawlPage(ctx, in) })
}

func (w *operationWriter) FinishCrawlSource(ctx context.Context, in state.CrawlSourceCompletion) (
	state.CrawlSource,
	error,
) {
	return operationMutation(w, func() (state.CrawlSource, error) { return w.Writer.FinishCrawlSource(ctx, in) })
}

func (w *operationWriter) FinishCrawlPlan(ctx context.Context, in state.RunCompletion) (state.CrawlReport, error) {
	return operationMutation(w, func() (state.CrawlReport, error) { return w.Writer.FinishCrawlPlan(ctx, in) })
}

func (w *operationWriter) StartValidationBatch(
	ctx context.Context,
	in state.ValidationBatchInput,
) (state.ValidationReport, error) {
	if w.creating != nil {
		w.creating()
	}
	return operationMutation(
		w,
		func() (state.ValidationReport, error) { return w.Writer.StartValidationBatch(ctx, in) },
	)
}

func (w *operationWriter) BeginBatchAttempt(ctx context.Context, in state.BatchAttemptInput) (
	state.BatchAttempt,
	error,
) {
	return operationMutation(w, func() (state.BatchAttempt, error) { return w.Writer.BeginBatchAttempt(ctx, in) })
}

func (w *operationWriter) CompleteBatchAttempt(
	ctx context.Context,
	in state.BatchAttemptCompletion,
) (state.BatchAttempt, error) {
	return operationMutation(w, func() (state.BatchAttempt, error) { return w.Writer.CompleteBatchAttempt(ctx, in) })
}

func (w *operationWriter) FinishValidationItem(
	ctx context.Context,
	in state.ValidationItemCompletion,
) (state.ValidationItem, error) {
	return operationMutation(w, func() (state.ValidationItem, error) { return w.Writer.FinishValidationItem(ctx, in) })
}

func (w *operationWriter) StopValidationBatch(ctx context.Context, in state.RunCompletion) (
	state.ValidationReport,
	error,
) {
	return operationMutation(
		w,
		func() (state.ValidationReport, error) { return w.Writer.StopValidationBatch(ctx, in) },
	)
}

func (w *operationWriter) FinishValidationBatch(ctx context.Context, in state.RunCompletion) (
	state.ValidationReport,
	error,
) {
	return operationMutation(
		w,
		func() (state.ValidationReport, error) { return w.Writer.FinishValidationBatch(ctx, in) },
	)
}

func (w *operationWriter) RecordCrawlMessage(
	ctx context.Context,
	page state.CrawlPageID,
	in state.MessageInput,
) (state.MessageResult, error) {
	return operationMutation(
		w,
		func() (state.MessageResult, error) { return w.Writer.RecordCrawlMessage(ctx, page, in) },
	)
}

func acquireValidator(
	ctx context.Context, backend validator.ScheduledBackend, a *App,
	m *mutationExecutor,
) (validator.Acquisition, error) {
	if reviewed, ok := backend.(interface {
		AcquireReviewed(
			context.Context, func() time.Time, time.Duration, func(func() error) error,
			*validator.CleanupBinding, func(),
		) (validator.Acquisition, error)
	}); ok && a.workResolved != nil {
		return reviewed.AcquireReviewed(
			ctx, a.Clock, a.Settings.SchedulePolicy().Failure,
			func(fn func() error) error { return m.call(false, fn) }, a.workRetained, a.workResolved,
		)
	}

	if coordinated, ok := backend.(interface {
		AcquireCoordinated(
			context.Context,
			func() time.Time,
			time.Duration,
			func(func() error) error,
		) (validator.Acquisition, error)
	}); ok {
		return coordinated.AcquireCoordinated(
			ctx, a.Clock, a.Settings.SchedulePolicy().Failure,
			func(fn func() error) error { return m.call(false, fn) },
		)
	}
	return backend.AcquireScheduled(ctx, a.Clock, a.Settings.SchedulePolicy().Failure)
}

func operationMutation[T any](w *operationWriter, fn func() (T, error)) (T, error) {
	value, err := mutation(w.mutations, fn)
	if err != nil {
		w.failed = true
	}
	if w.observe != nil {
		w.observe(value, err)
	}
	return value, err
}
