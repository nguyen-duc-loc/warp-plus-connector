package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
)

// RunSummary contains only an acknowledged terminal child report.
type RunSummary struct {
	Crawl      *state.CrawlReport
	Validation *state.ValidationReport
}

// RunnerState describes current admission and lanes after schedule preparation.
type RunnerState struct {
	Phase, CrawlPhase, ValidationPhase string
}

type laneResult struct {
	acknowledged bool
	summary      RunSummary
	acquisition  *validator.Acquisition
	err          error
}

// ShowSchedule reads saved scheduling facts without applying current settings.
func (a *App) ShowSchedule(ctx context.Context, id int64) (out state.ScheduleSummary, err error) {
	r, err := store.OpenReader(ctx, a.Settings.StateDir)
	if err != nil {
		return out, classified(err)
	}
	defer closeStore(r, &err)
	out, err = r.GetSchedule(ctx, id, a.Clock)
	return out, classified(err)
}

func witnessClock(a *App, w *operationWriter, ctx context.Context) error {
	safe, err := mutation(w.mutations, func() (bool, error) { return w.ObserveSchedule(ctx, a.Clock()) })
	if err != nil {
		return classified(err)
	}
	if !safe {
		return &Error{Code: "clock_paused"}
	}
	return nil
}

// Run owns one writer until both independently progressing lanes have stopped.
func (a *App) Run(parent context.Context, emit func(RunSummary) error) (err error) {
	copy := *a
	a = &copy
	a.nextMutation = time.Time{}
	if a.Settings.CheckPolicy == (state.CheckPolicy{}) {
		a.Settings.CheckPolicy = state.DefaultCheckPolicy()
	}
	if e := a.Settings.ValidateRun(); e != nil {
		return e
	}
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	a.shutdown = newShutdownWindow(ctx)
	defer a.shutdown.cancelCleanup()
	defer func() {
		if a.Finalize != nil {
			final, release := a.shutdown.localContext()
			defer release()
			err = errors.Join(err, a.Finalize(final, err))
		}
	}()
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			a.shutdown.stop()
		case <-watchDone:
		}
	}()
	raw, err := store.OpenWriterWithPolicy(ctx, a.Settings.StateDir, a.Clock, a.Settings.SchedulePolicy())
	if err != nil {
		return classified(err)
	}
	defer closeStore(&raw.Reader, &err)
	m := newMutationExecutor(cancel)
	defer m.close()
	w := &operationWriter{Writer: raw, mutations: m}
	var coverage []state.SourcePlan
	if a.Settings.CrawlEnabled {
		coverage, err = a.Settings.SelectSources(nil, nil)
		if err != nil {
			return err
		}
	}
	reconcile := func() (bool, error) {
		return mutation(
			m, func() (bool, error) {
				return raw.PrepareSchedule(
					ctx, a.Clock(),
					a.Settings.SchedulePolicy(), coverage, a.Settings.ValidationEnabled,
				)
			},
		)
	}
	reconciled, err := reconcile()
	if err != nil {
		return classified(err)
	}
	events := make(chan laneResult, 2)
	activeCrawl, activeValidation := false, false
	laneCtx, laneCancel := context.WithCancel(ctx)
	defer func() { laneCancel() }()
	paused := !reconciled
	validationPhase := "idle"
	observe := func() error {
		if a.Observe == nil {
			return nil
		}
		s := RunnerState{Phase: "running", CrawlPhase: "idle", ValidationPhase: validationPhase}
		if activeCrawl {
			s.CrawlPhase = "crawling"
		}
		if paused {
			s.Phase, s.CrawlPhase, s.ValidationPhase = "clock_paused", "clock_paused", "clock_paused"
		}
		if ctx.Err() != nil {
			s.Phase, s.CrawlPhase, s.ValidationPhase = "stopping", "stopping", "stopping"
		}
		if !a.Settings.CrawlEnabled {
			s.CrawlPhase = "disabled"
		}
		if !a.Settings.ValidationEnabled {
			s.ValidationPhase = "disabled"
		}
		return a.Observe(s)
	}
	outputOK := true
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	wake := time.NewTimer(0)
	defer wake.Stop()
	stop := func(e error) {
		if err == nil {
			err = e
		}
		cancel(e)
		laneCancel()
	}
	emitSaved := func(result laneResult) {
		if !result.acknowledged {
			return
		}
		if result.err != nil {
			code := classified(result.err).(*Error).Code
			if strings.HasPrefix(code, "state_") {
				return
			}
		}
		terminal := false
		if r := result.summary.Crawl; r != nil {
			terminal = r.Run.ID != 0 && r.Run.Status != state.Running
		}
		if r := result.summary.Validation; r != nil {
			terminal = r.Run.ID != 0 && r.Run.Status != state.Running
		}
		if terminal && outputOK && emit != nil {
			if e := emit(result.summary); e != nil {
				outputOK = false
				stop(&Error{Code: "output_error"})
			}
		}
	}
	log := func(code string) {
		if a.Logger != nil {
			a.Logger.Info("scheduler", "code", code)
		}
	}
	log("schedule_starting")
	for {
		if e := observe(); e != nil && ctx.Err() == nil {
			stop(e)
		}
		if ctx.Err() != nil && !activeCrawl && !activeValidation {
			if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				err = classified(context.Cause(ctx))
			}
			return err
		}
		if ctx.Err() == nil {
			snapshot, e := raw.GetSchedule(ctx, 0, a.Clock)
			if e != nil {
				stop(classified(e))
				continue
			}
			if snapshot.ClockRegressed {
				if !paused {
					paused = true
					laneCancel()
					log("clock_paused")
				}
			} else if paused && !activeCrawl && !activeValidation {
				reconciled, e = reconcile()
				if e != nil {
					stop(classified(e))
					continue
				}
				if reconciled {
					paused = false
					laneCtx, laneCancel = renewLane(ctx, laneCancel)
					log("schedule_resumed")
				}
			}
			if !paused {
				if a.Settings.CrawlEnabled && !activeCrawl && snapshot.Crawl != nil && !snapshot.Crawl.DueAt.After(snapshot.AsOf) {
					if e := witnessClock(a, w, ctx); e != nil {
						if classified(e).(*Error).Code == "clock_paused" {
							paused = true
							laneCancel()
						} else {
							stop(e)
						}
						continue
					}
					activeCrawl = true
					if e := observe(); e != nil {
						activeCrawl = false
						stop(e)
						continue
					}
					go func(task context.Context) {
						taskWriter := &operationWriter{Writer: w.Writer, mutations: w.mutations}
						r, e := a.crawlOwned(task, taskWriter, coverage, "scheduled")
						events <- laneResult{
							summary: RunSummary{Crawl: &r}, err: e, acknowledged: !taskWriter.failed,
						}
					}(laneCtx)
				}
				wait := snapshot.ValidationWait
				if !activeValidation {
					validationPhase = "idle"
					if wait != nil && wait.RetryAt.After(snapshot.AsOf) {
						validationPhase = "waiting"
					}
				}
				needsValidation := snapshot.Candidates.Due > 0 || snapshot.ValidationRecoveryPending
				if a.Settings.ValidationEnabled && !activeValidation && needsValidation &&
					(wait == nil || !wait.RetryAt.After(snapshot.AsOf)) {
					if e := witnessClock(a, w, ctx); e != nil {
						if classified(e).(*Error).Code == "clock_paused" {
							paused = true
							laneCancel()
						} else {
							stop(e)
						}
						continue
					}
					e := m.call(true, func() error { return raw.SaveValidationWait(ctx, a.Clock(), wait, true) })
					if e != nil {
						stop(classified(e))
						continue
					}
					backend, ok := a.Validator.(validator.ScheduledBackend)
					if !ok {
						stop(&Error{Code: "validation_unsupported"})
						continue
					}
					activeValidation = true
					validationPhase = "acquiring"
					if e := observe(); e != nil {
						activeValidation = false
						stop(e)
						continue
					}
					a.shutdown.startRemote()
					go func(task context.Context) {
						acq, e := acquireValidator(task, backend, a, m)
						events <- laneResult{acquisition: &acq, err: e}
					}(laneCtx)
				}
			}
			delay := 30 * time.Second
			consider := func(at time.Time) {
				d := at.Sub(snapshot.AsOf)
				if d > 0 && d < delay {
					delay = d
				}
			}
			if !paused {
				if a.Settings.CrawlEnabled && !activeCrawl && snapshot.Crawl != nil {
					consider(snapshot.Crawl.DueAt)
				}
				if a.Settings.ValidationEnabled && !activeValidation {
					if snapshot.ValidationWait != nil {
						consider(snapshot.ValidationWait.RetryAt)
					} else if snapshot.Candidates.NextDueAt != nil {
						consider(*snapshot.Candidates.NextDueAt)
					}
				}
			}
			if e := observe(); e != nil {
				stop(e)
				continue
			}
			wake.Reset(delay)
		}
		select {
		case <-ctx.Done():
			laneCancel()
			// Drain events without spinning on an already closed cancellation channel.
			if activeCrawl || activeValidation {
				result := <-events
				a.handleLaneResult(
					ctx,
					laneCtx,
					w,
					result,
					events,
					&activeCrawl,
					&activeValidation,
					paused,
					&err,
					emitSaved,
					stop,
					log,
				)
			}
		case <-ticker.C:
			if ctx.Err() == nil && !paused {
				if e := witnessClock(a, w, ctx); e != nil && classified(e).(*Error).Code != "clock_paused" {
					stop(e)
				}
			}
		case <-wake.C:
		case result := <-events:
			if result.acquisition != nil {
				validationPhase = "checking"
			}
			a.handleLaneResult(
				ctx,
				laneCtx,
				w,
				result,
				events,
				&activeCrawl,
				&activeValidation,
				paused,
				&err,
				emitSaved,
				stop,
				log,
			)
		}
	}
}

func (a *App) handleLaneResult(
	ctx, task context.Context, w *operationWriter, result laneResult, events chan laneResult,
	crawl, batch *bool, paused bool, runErr *error, emit func(laneResult), stop func(error), log func(string),
) {
	if result.acquisition != nil {
		acq := result.acquisition
		handedOff := false
		defer func() {
			if !handedOff {
				a.shutdown.finishRemote()
			}
		}()
		*batch = false
		if ctx.Err() != nil || paused || task.Err() != nil {
			if acq.Owner != nil {
				_ = acq.Owner.Close()
			}
			return
		}
		if e := w.mutations.call(
			true,
			func() error { return w.SaveValidationWait(ctx, a.Clock(), acq.Wait, !acq.CleanupInspected) },
		); e != nil {
			if acq.Owner != nil {
				_ = acq.Owner.Close()
			}
			stop(classified(e))
			return
		}
		if result.err != nil {
			if acq.Wait != nil {
				log(acq.Wait.Reason)
				return
			}
			stop(classified(result.err))
			return
		}
		limit := 2 * a.Settings.CheckPolicy.Workers
		if acq.Probe {
			limit = 1
		}
		ids, e := w.DueCandidates(ctx, a.Clock(), limit)
		if e != nil {
			_ = acq.Owner.Close()
			stop(classified(e))
			return
		}
		if len(ids) == 0 {
			_ = acq.Owner.Close()
			return
		}
		*batch = true
		handedOff = true
		go func() {
			taskWriter := &operationWriter{Writer: w.Writer, mutations: w.mutations}
			report, e := a.checkOwned(task, taskWriter, ids, nil, "scheduled", acq.Owner)
			events <- laneResult{summary: RunSummary{Validation: &report}, err: e, acknowledged: !taskWriter.failed}
		}()
		return
	}
	if result.summary.Crawl != nil {
		*crawl = false
	} else {
		*batch = false
	}
	emit(result)
	if result.err == nil {
		return
	}
	var e *Error
	if !errors.As(result.err, &e) {
		stop(classified(result.err))
		return
	}
	if ctx.Err() != nil || paused {
		return
	}
	if wait := validator.AdmissionWait(result.err); wait != nil {
		if err := w.mutations.call(
			true,
			func() error { return w.SaveValidationWait(ctx, a.Clock(), wait, true) },
		); err != nil {
			stop(classified(err))
		}
		log(wait.Reason)
		return
	}
	if e.Code == "validation_cleanup_pending" {
		at := a.Clock().UTC().Truncate(time.Microsecond)
		interval := a.Settings.SchedulePolicy().Failure
		due, _ := state.ScheduleDue(at, interval)
		wait := &state.ValidationWait{
			Reason: "cleanup_pending", BasisAt: at, RetryAt: due, ObservedAt: at, Interval: interval,
		}
		if err := w.mutations.call(
			true,
			func() error { return w.SaveValidationWait(ctx, at, wait, true) },
		); err != nil {
			stop(classified(err))
		}
		log("cleanup_pending")
		return
	}
	if strings.HasPrefix(
		e.Code,
		"source_",
	) || e.Code == "validation_batch_inconclusive" || e.Code == "validation_rate_limited" {
		return
	}
	if e.Code == "clock_paused" {
		return
	}
	if *runErr == nil {
		stop(result.err)
	}
}

func renewLane(parent context.Context, previous context.CancelFunc) (context.Context, context.CancelFunc) {
	previous()
	return context.WithCancel(parent)
}
