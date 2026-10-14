package app

import (
	"context"
	"crypto/rand"
	"errors"
	randv2 "math/rand/v2"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/diagnostic"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
)

type checkJob struct {
	candidate         state.Candidate
	number            int
	attempt           state.Attempt
	deadline, timeDue time.Time
	activeDeadline    time.Time
	cancel            context.CancelFunc
	running, done     bool
	eligible          *validator.Result
	completion        *state.BatchAttemptCompletion
	session           string
	admitted          time.Time
}

type checkEvent struct {
	job             *checkJob
	kind, operation string
	session         string
	ctx             context.Context
	commit          func() error
	result          validator.Result
	err             error
	reply           chan error
}

type batchCoordinator struct {
	healthy            bool
	app                *App
	writer             *operationWriter
	owner              validator.BatchOwner
	policy             state.CheckPolicy
	report             state.ValidationReport
	jobs               []*checkJob
	events             chan checkEvent
	admissions         []checkEvent
	mutation           *checkJob
	nextMutation       time.Time
	mutationStarted    time.Time
	pause              time.Time
	now                func() time.Time
	emit               func(state.ValidationItem) error
	ctx                context.Context
	cleanup            context.Context
	cancelCleanup      context.CancelFunc
	stopErr            error
	stopCode           string
	reliable, outputOK bool
	active             int
	preferRetry        bool
}

// CheckBatch saves selection before checking and emits only committed final items.
func (a *App) CheckBatch(
	ctx context.Context,
	ids []int64,
	emit func(state.ValidationItem) error,
) (report state.ValidationReport, err error) {
	if len(ids) == 0 {
		return report, &Error{Code: "state_invalid_input"}
	}
	policy := a.Settings.CheckPolicy
	if policy == (state.CheckPolicy{}) {
		policy = state.DefaultCheckPolicy()
	}
	if !policy.Valid() {
		return report, &Error{Code: "state_invalid_input"}
	}
	for _, id := range ids {
		if id <= 0 {
			return report, &Error{Code: "state_invalid_input"}
		}
	}
	w, err := store.OpenWriterWithPolicy(ctx, a.Settings.StateDir, a.Clock, a.Settings.SchedulePolicy())
	if err != nil {
		return report, classified(err)
	}
	defer closeStore(&w.Reader, &err)
	mutations := newMutationExecutor(nil)
	defer mutations.close()
	return a.checkOwned(ctx, &operationWriter{Writer: w, mutations: mutations}, ids, emit, "manual", nil)
}

func (a *App) checkOwned(
	ctx context.Context, w *operationWriter, ids []int64, emit func(state.ValidationItem) error,
	origin string, acquired validator.BatchOwner,
) (report state.ValidationReport, err error) {
	if acquired != nil {
		defer func() { err = errors.Join(err, classified(acquired.Close())) }()
	}
	entered := false
	defer func() {
		if a.shutdown != nil && !entered {
			a.shutdown.finishRemote()
		}
	}()

	policy := a.Settings.CheckPolicy
	if policy == (state.CheckPolicy{}) {
		policy = state.DefaultCheckPolicy()
	}
	b := &batchCoordinator{
		app: a, writer: w, policy: policy, emit: emit, ctx: ctx, reliable: true, outputOK: true,
		events: make(chan checkEvent, policy.Workers), now: time.Now,
	}
	if a.Monotonic != nil {
		b.now = a.Monotonic
	}
	if a.shutdown != nil {
		b.nextMutation = a.nextMutation
		defer func() { a.nextMutation = b.nextMutation }()
	}
	seen := map[int64]bool{}
	selection := []int64{}
	evaluation := a.evaluation()
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		candidate, err := w.GetCandidate(ctx, id, evaluation)
		if err != nil {
			return report, classified(err)
		}
		b.jobs = append(b.jobs, &checkJob{candidate: candidate})
		selection = append(selection, id)
	}
	if acquired != nil {
		b.owner = acquired
	} else if backend, ok := a.Validator.(validator.ScheduledBackend); ok {
		acquisition, e := acquireValidator(ctx, backend, a, w.mutations)
		if e != nil {
			return report, classified(e)
		}
		b.owner = acquisition.Owner
	} else if backend, ok := a.Validator.(validator.BatchBackend); ok {
		b.owner, err = backend.Acquire(ctx)
		if err != nil {
			return report, classified(err)
		}
	}
	if b.owner != nil && acquired == nil {
		defer func() { err = errors.Join(err, classified(b.owner.Close())) }()
	}
	if gate, ok := b.owner.(validator.GateOwner); ok {
		probe, e := gate.Admission(a.Clock())
		if e != nil {
			return report, classified(e)
		}
		if probe {
			if len(selection) != 1 {
				return report, &Error{Code: "validator_probe_required"}
			}
			policy.Workers = 1
			policy.MaxAttempts = 1
			b.policy = policy
		}
	}
	backend, version := a.Validator.Identity()
	report, err = w.StartValidationBatch(
		ctx, state.ValidationBatchInput{
			Origin: origin, RequestID: rand.Text(), StartedAt: a.Clock(),
			CandidateIDs: selection, BackendID: backend, BackendVersion: version, Policy: policy,
		},
	)
	if err != nil {
		return report, classified(err)
	}
	b.report = report
	if gate, ok := b.owner.(validator.GateOwner); ok {
		if err = w.mutations.call(
			false,
			func() error { return gate.Begin(a.Clock(), a.Settings.SchedulePolicy().Failure) },
		); err != nil {
			return report, classified(err)
		}
	}
	cleanupParent := context.Background()
	if a.shutdown != nil {
		cleanupParent = a.shutdown.cleanup
	}
	b.cleanup, b.cancelCleanup = context.WithCancel(cleanupParent)
	defer b.cancelCleanup()
	diagnostic.Emit(
		a.Logger, "work_diagnostic", diagnostic.Int("run_id", report.Run.ID),
		diagnostic.String("code", "batch_admitted"), diagnostic.String("result", "batch_admitted"),
		diagnostic.String("origin", report.Run.Origin), diagnostic.Int("selected", int64(report.Totals.Selected)),
	)
	entered = true
	return b.run()
}

func (b *batchCoordinator) stop(err error, code string) {
	if b.stopErr != nil {
		return
	}
	b.stopErr = err
	b.stopCode = code
	for _, j := range b.jobs {
		if j.cancel != nil {
			j.cancel()
		}
	}
	time.AfterFunc(time.Duration(b.policy.ShutdownTimeoutUS)*time.Microsecond, b.cancelCleanup)
	b.app.log(b.report.Run.ID, code)
}

func (b *batchCoordinator) storeFailed(err error) {
	b.reliable = false
	b.stop(classified(err), classified(err).(*Error).Code)
}

func (b *batchCoordinator) send(event checkEvent) error {
	event.reply = make(chan error, 1)
	select {
	case b.events <- event:
	case <-b.cleanup.Done():
		return b.cleanup.Err()
	}
	select {
	case err := <-event.reply:
		return err
	case <-b.cleanup.Done():
		return b.cleanup.Err()
	}
}

func (b *batchCoordinator) start(j *checkJob) {
	now := b.now()
	if j.deadline.IsZero() {
		j.deadline = now.Add(time.Duration(b.policy.CandidateTimeoutUS) * time.Microsecond)
	}
	j.number++
	attempt, err := b.writer.BeginBatchAttempt(
		b.ctx, state.BatchAttemptInput{
			AttemptInput: state.AttemptInput{
				RequestID: rand.Text(), RunID: b.report.Run.ID, CandidateID: j.candidate.ID,
				BackendID:      b.report.BackendID,
				BackendVersion: b.report.BackendVersion, StartedAt: b.app.Clock(),
			}, Number: j.number,
		},
	)
	if err != nil {
		b.storeFailed(err)
		return
	}
	j.attempt = attempt.Attempt
	j.admitted = now
	j.eligible = nil
	j.running = true
	b.active++
	j.activeDeadline = now.Add(time.Duration(b.policy.AttemptTimeoutUS) * time.Microsecond)
	if j.deadline.Before(j.activeDeadline) {
		j.activeDeadline = j.deadline
	}
	active, cancel := context.WithTimeout(b.ctx, j.activeDeadline.Sub(b.now()))
	j.cancel = cancel
	diagnostic.Emit(b.app.Logger, "attempt_admitted", b.attemptFields(j)...)
	go func() {
		input := validator.Input{
			Key: j.candidate.Key, StateDir: b.app.Settings.StateDir, CandidateID: j.candidate.ID,
			RunID: j.attempt.RunID, AttemptID: j.attempt.ID,
		}
		hooks := validator.Hooks{
			Session: func(id string) error { return b.send(checkEvent{kind: "session", job: j, session: id}) },
			Commit:  func(fn func() error) error { return b.send(checkEvent{kind: "commit", job: j, commit: fn}) },
			Admit: func(ctx context.Context, op string) error {
				return b.send(
					checkEvent{
						kind: "admit", job: j, ctx: ctx, operation: op,
					},
				)
			},
			MutationDone: func(r validator.Result, err error) error {
				return b.send(
					checkEvent{
						kind: "mutation", job: j, result: r, err: err,
					},
				)
			},
			Observe: func(r validator.Result) error {
				return b.send(
					checkEvent{
						kind: "observe", job: j, result: r,
					},
				)
			},
			CleanupContext: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(b.cleanup, time.Duration(b.policy.CleanupTimeoutUS)*time.Microsecond)
			},
		}
		var result validator.Result
		var err error
		if b.owner != nil {
			result, err = b.owner.Run(active, input, hooks)
		} else {
			result, err = b.app.Validator.Check(active, input)
			// Legacy adapters have no retained external session. Docker cannot use this path.
			result.CleanupProven = true
			result.Removed = true
		}
		cancel()
		b.events <- checkEvent{kind: "finished", job: j, result: result, err: err}
	}()
}

func (b *batchCoordinator) admission() {
	for len(b.admissions) > 0 {
		e := b.admissions[0]
		if b.stopErr != nil || e.ctx.Err() != nil {
			err := e.ctx.Err()
			if err == nil {
				err = b.stopErr
			}
			e.reply <- err
			b.admissions = b.admissions[1:]
			continue
		}
		now := b.now()
		if b.mutation != nil || now.Before(b.nextMutation) || now.Before(b.pause) {
			return
		}
		if gate, ok := b.owner.(validator.GateOwner); ok {
			// Active phase belongs to this owner; clock checks remain mandatory.
			if b.report.Run.Origin == "scheduled" {
				if err := witnessClock(b.app, b.writer, b.ctx); err != nil {
					b.stop(err, "clock_paused")
					continue
				}
			}
			if err := b.writer.mutations.call(
				false,
				func() error { return gate.BeforeMutation(b.app.Clock()) },
			); err != nil {
				b.stop(classified(err), classified(err).(*Error).Code)
				continue
			}
		}
		b.admissions = b.admissions[1:]
		b.mutation = e.job
		b.mutationStarted = now
		diagnostic.Emit(
			b.app.Logger, "mutation_admitted",
			diagnostic.Int("run_id", b.report.Run.ID), diagnostic.Int("candidate_id", e.job.candidate.ID),
			diagnostic.Int("attempt_id", e.job.attempt.ID), diagnostic.String("operation", e.operation),
		)
		e.reply <- nil
		return
	}
}

func (b *batchCoordinator) classifyMutation(r validator.Result, err error) {
	if r.FailureClass == state.Throttled {
		if gate, ok := b.owner.(validator.GateOwner); ok {
			e := b.writer.mutations.call(false, func() error { return gate.Throttle(b.app.Clock(), r.ServerDelay) })
			if e != nil {
				b.stop(classified(e), "validator_state_error")
				return
			}
			b.stop(&Error{Code: "validation_rate_limited"}, "validation_rate_limited")
			return
		}
	}
	if r.FailureClass == state.Throttled && r.ServerDelay > 0 {
		until := b.now().Add(r.ServerDelay)
		if until.After(b.pause) {
			b.pause = until
		}
	}
	if err != nil || r.FailureClass == state.Unsafe {
		code := r.ReasonCode
		if code == "" {
			code = "validation_batch_stopped"
		}
		if err == nil {
			err = &Error{Code: code}
		} else if r.ReasonCode == "" {
			code = classified(err).(*Error).Code
		}
		b.stop(classified(err), code)
	} else if r.FailureClass == state.Throttled && r.ServerDelay <= 0 {
		b.stop(&Error{Code: "validation_rate_limited"}, "validation_rate_limited")
	}
}

func (b *batchCoordinator) observe(j *checkJob, r validator.Result) {
	if b.stopErr == nil && b.ctx.Err() == nil && b.now().Before(j.activeDeadline) &&
		(r.Outcome == state.Confirmed || r.Outcome == state.Invalid) {
		copy := r
		if r.Evidence != nil {
			e := *r.Evidence
			copy.Evidence = &e
		}
		j.eligible = &copy
	}
}

func (b *batchCoordinator) finish(e checkEvent) {
	j := e.job
	j.running = false
	b.active--
	fields := append(b.attemptFields(j), diagnostic.String("elapsed", b.now().Sub(j.admitted).String()))
	diagnostic.Emit(b.app.Logger, "attempt_completed", append(fields, observationFields(e.result, e.err)...)...)
	r := e.result
	if r.ProviderResponsive && r.CleanupProven && r.Removed && e.err == nil {
		b.healthy = true
	}
	b.observe(j, r)
	b.classifyMutation(r, e.err)
	if b.mutation == j {
		b.stop(&Error{Code: "validation_batch_stopped"}, "validation_batch_stopped")
	}
	if !r.CleanupProven || !r.Removed {
		b.stop(&Error{Code: "validation_cleanup_pending"}, "validation_cleanup_pending")
	}
	if !b.reliable {
		return
	}
	if j.eligible != nil && r.CleanupProven {
		saved := *j.eligible
		saved.CleanupProven = r.CleanupProven
		saved.Removed = r.Removed
		r = saved
	} else {
		r.Outcome = state.Inconclusive
		r.Evidence = nil
		if r.ReasonCode == "" {
			r.ReasonCode = b.stopCode
			if r.ReasonCode == "" {
				r.ReasonCode = "validation_batch_stopped"
			}
		}
		if r.FailureClass == "" {
			r.FailureClass = state.Terminal
		}
	}
	completion := state.BatchAttemptCompletion{
		AttemptCompletion: state.AttemptCompletion{
			SchedulePolicy: b.app.Settings.SchedulePolicy(),
			AttemptID:      j.attempt.ID,
			FinishedAt:     b.app.Clock(), Outcome: r.Outcome, ReasonCode: r.ReasonCode, Evidence: r.Evidence,
		}, FailureClass: r.FailureClass,
	}
	if r.Outcome == state.Inconclusive {
		completion.StopReason = "nonretryable"
		switch {
		case b.stopErr != nil:
			completion.StopReason = b.stopCode
		case !b.now().Before(j.deadline) && j.number >= b.policy.MaxAttempts &&
			(r.FailureClass == state.Temporary || r.FailureClass == state.Throttled):
			completion.StopReason = "attempt_limit"
		case !b.now().Before(j.deadline):
			completion.StopReason = "candidate_timeout"
		case r.FailureClass != state.Temporary && r.FailureClass != state.Throttled:
			completion.StopReason = "nonretryable"
		case j.number >= b.policy.MaxAttempts:
			completion.StopReason = "attempt_limit"
		case (r.FailureClass == state.Temporary || r.FailureClass == state.Throttled) && r.CleanupProven && r.Removed:
			low := time.Duration(b.policy.RetryBaseUS) * time.Microsecond * (1 << uint(j.number-1))
			cap := time.Duration(b.policy.RetryCapUS) * time.Microsecond
			high := 2 * low
			if low > cap {
				low = cap
			}
			if high > cap {
				high = cap
			}
			wait := low + time.Duration(randv2.Int64N(int64(high-low)+1))
			if b.app.RetryDraw != nil {
				wait = b.app.RetryDraw(low, high)
			}
			if wait < low || wait > high {
				b.stop(&Error{Code: "validation_batch_stopped"}, "validation_batch_stopped")
				break
			}
			if pause := b.pause.Sub(b.now()); pause > wait {
				wait = pause
			}
			if !b.now().Add(wait).Before(j.deadline) {
				completion.StopReason = "candidate_timeout"
				break
			}
			wait = ((wait + time.Microsecond - 1) / time.Microsecond) * time.Microsecond
			if !b.now().Add(wait).Before(j.deadline) {
				completion.StopReason = "candidate_timeout"
				break
			}
			due := b.app.Clock().Add(wait)
			completion.RetryWait = wait
			completion.RetryEligibleAt = &due
			completion.StopReason = ""
			j.timeDue = b.now().Add(wait)
		}
	}
	if b.stopErr != nil {
		j.completion = &completion
		return
	}
	ctx := b.ctx
	_, err := b.writer.CompleteBatchAttempt(ctx, completion)
	if err != nil {
		b.storeFailed(err)
		return
	}
	if completion.RetryWait == 0 {
		j.done = true
		b.output(ctx, j)
	} else {
		fields := []diagnostic.Field{
			diagnostic.Int("run_id", b.report.Run.ID),
			diagnostic.Int("candidate_id", j.candidate.ID), diagnostic.Int("attempt_id", j.attempt.ID),
			diagnostic.Int("attempt_number", int64(j.number)), diagnostic.String("wait", completion.RetryWait.String()),
		}
		if completion.FailureClass != "" {
			fields = append(fields, diagnostic.String("failure_class", string(completion.FailureClass)))
		}
		if completion.ReasonCode != "" {
			fields = append(fields, diagnostic.String("code", completion.ReasonCode))
		}
		diagnostic.Emit(b.app.Logger, "retry_planned", fields...)
	}
}

func (b *batchCoordinator) output(ctx context.Context, j *checkJob) {
	if !b.outputOK || b.emit == nil {
		return
	}
	item, err := b.writer.GetValidationItem(ctx, b.report.Run.ID, j.candidate.ID)
	if err != nil {
		b.stop(classified(err), "state_error")
		return
	}
	if err = b.emit(item); err != nil {
		b.outputOK = false
		b.stop(&Error{Code: "output_error"}, "output_error")
	}
}

func (b *batchCoordinator) dispatch() {
	for b.stopErr == nil && b.active < b.policy.Workers {
		if err := b.ctx.Err(); err != nil {
			b.stop(classified(err), "cancelled")
			return
		}
		var pending, retry *checkJob
		for _, j := range b.jobs {
			if j.done || j.running {
				continue
			}
			if j.number == 0 {
				if pending == nil {
					pending = j
				}
				continue
			}
			if !b.now().Before(j.deadline) {
				_, err := b.writer.FinishValidationItem(
					b.ctx, state.ValidationItemCompletion{
						SchedulePolicy: b.app.Settings.SchedulePolicy(),
						RunID:          b.report.Run.ID,
						CandidateID:    j.candidate.ID, FinishedAt: b.app.Clock(), StopReason: "candidate_timeout",
					},
				)
				if err != nil {
					b.storeFailed(err)
					return
				}
				j.done = true
				b.output(b.ctx, j)
				continue
			}
			if !b.now().Before(j.timeDue) && (retry == nil || j.timeDue.Before(retry.timeDue)) {
				retry = j
			}
		}
		if b.stopErr != nil {
			return
		}
		selected := pending
		if retry != nil && (pending == nil || b.preferRetry) {
			selected = retry
		}
		if selected == nil {
			return
		}
		b.preferRetry = selected == pending
		b.start(selected)
	}
}

func (b *batchCoordinator) run() (state.ValidationReport, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	cancellation := b.ctx.Done()
	cleanupDone := b.cleanup.Done()
	for {
		if err := b.ctx.Err(); err != nil {
			b.stop(classified(err), "cancelled")
		}
		b.dispatch()
		b.admission()
		done := true
		for _, j := range b.jobs {
			if !j.done {
				done = false
				break
			}
		}
		if b.active == 0 && (done || b.stopErr != nil) {
			break
		}
		select {
		case <-cancellation:
			b.stop(classified(b.ctx.Err()), "cancelled")
			cancellation = nil
		case <-cleanupDone:
			cleanupDone = nil
		case <-ticker.C:
		case e := <-b.events:
			if err := b.ctx.Err(); err != nil {
				b.stop(classified(err), "cancelled")
			}
			switch e.kind {
			case "session":
				e.job.session = e.session
				e.reply <- nil
			case "admit":
				b.admissions = append(b.admissions, e)
			case "commit":
				if err := b.cleanup.Err(); err != nil {
					e.reply <- err
					continue
				}
				err := b.writer.mutations.call(false, e.commit)
				if err != nil {
					b.stop(classified(err), "validator_state_error")
				}
				e.reply <- err
			case "mutation":
				b.classifyMutation(e.result, e.err)
				if b.mutation == e.job {
					fields := []diagnostic.Field{
						diagnostic.Int("run_id", b.report.Run.ID),
						diagnostic.Int("candidate_id", e.job.candidate.ID),
						diagnostic.Int("attempt_id", e.job.attempt.ID),
						diagnostic.String("elapsed", b.now().Sub(b.mutationStarted).String()),
					}
					diagnostic.Emit(
						b.app.Logger,
						"mutation_completed",
						append(fields, observationFields(e.result, e.err)...)...,
					)
					b.mutation = nil
					b.nextMutation = b.now().Add(time.Duration(b.policy.MutationIntervalUS) * time.Microsecond)
				}
				e.reply <- nil
			case "observe":
				b.observe(e.job, e.result)
				e.reply <- nil
			case "finished":
				b.finish(e)
			}
		}
	}
	return b.finalize()
}

func (b *batchCoordinator) finalize() (state.ValidationReport, error) {
	var ctx context.Context
	var cancel context.CancelFunc
	if b.app.shutdown != nil {
		b.app.shutdown.finishRemote()
		ctx, cancel = b.app.shutdown.localContext()
	} else {
		ctx, cancel = context.WithTimeout(
			context.Background(),
			time.Duration(b.policy.FinalizeTimeoutUS)*time.Microsecond,
		)
	}
	defer cancel()
	if b.reliable {
		for _, j := range b.jobs {
			if j.attempt.ID == 0 || j.done {
				continue
			}
			item, err := b.writer.GetValidationItem(ctx, b.report.Run.ID, j.candidate.ID)
			if err != nil {
				b.stop(classified(err), "state_error")
				break
			}
			if item.LatestAttempt != nil && item.LatestAttempt.Outcome == "" {
				completion := state.BatchAttemptCompletion{
					AttemptCompletion: state.AttemptCompletion{
						SchedulePolicy: b.app.Settings.SchedulePolicy(),
						AttemptID:      j.attempt.ID, FinishedAt: b.app.Clock(), Outcome: state.Inconclusive,
						ReasonCode: b.stopCode,
					},
					FailureClass: state.Unsafe, StopReason: b.stopCode,
				}
				if j.completion != nil {
					completion = *j.completion
				}
				_, err = b.writer.CompleteBatchAttempt(ctx, completion)
				if err != nil {
					b.storeFailed(err)
					break
				}
			}
		}
	}
	if b.reliable {
		in := state.RunCompletion{
			SchedulePolicy: b.app.Settings.SchedulePolicy(), RunID: b.report.Run.ID, FinishedAt: b.app.Clock(),
			Status: state.Completed,
		}
		var err error
		if b.stopErr != nil {
			in.Status = state.Failed
			if errors.Is(b.ctx.Err(), context.Canceled) {
				in.Status = state.Cancelled
			}
			in.ReasonCode = b.stopCode
			report, e := b.writer.StopValidationBatch(ctx, in)
			err = e
			if e == nil {
				b.report = report
			}
		} else {
			report, e := b.writer.FinishValidationBatch(ctx, in)
			err = e
			if e == nil {
				b.report = report
			}
		}
		if err != nil {
			b.storeFailed(err)
		} else {
			b.app.logValidation(b.report)
			for _, j := range b.jobs {
				if !j.done {
					j.done = true
					b.output(ctx, j)
				}
			}
		}
	}
	if report, err := b.writer.GetValidationReport(ctx, b.report.Run.ID); err == nil {
		b.report = report
	}
	if gate, ok := b.owner.(validator.GateOwner); ok {
		acknowledged := b.reliable && b.report.Run.Status != state.Running
		if err := b.writer.mutations.call(
			false, func() error {
				return gate.Resolve(
					b.app.Clock(), acknowledged,
					b.healthy && b.stopErr == nil,
				)
			},
		); err != nil {
			return b.report, classified(err)
		}
	}
	if b.stopErr != nil {
		return b.report, b.stopErr
	}
	if b.report.Totals.Inconclusive+b.report.Totals.Unstarted+b.report.Totals.Unresolved > 0 {
		return b.report, &Error{Code: "validation_batch_inconclusive"}
	}
	return b.report, nil
}

func (b *batchCoordinator) attemptFields(j *checkJob) []diagnostic.Field {
	return []diagnostic.Field{
		diagnostic.Int("run_id", b.report.Run.ID), diagnostic.Int("candidate_id", j.candidate.ID),
		diagnostic.Int("attempt_id", j.attempt.ID), diagnostic.Int("attempt_number", int64(j.number)),
		diagnostic.Int("active", int64(b.active)), diagnostic.Int("ceiling", int64(b.policy.Workers)),
	}
}
func observationFields(r validator.Result, err error) []diagnostic.Field {
	if r.ReasonCode == "" && err != nil {
		r.ReasonCode = classified(err).(*Error).Code
	}
	fields := []diagnostic.Field{
		diagnostic.Bool("cleanup_proven", r.CleanupProven), diagnostic.Bool("removed", r.Removed),
	}
	if r.Outcome != "" {
		fields = append(fields, diagnostic.String("outcome", string(r.Outcome)))
	}
	if r.FailureClass != "" {
		fields = append(fields, diagnostic.String("failure_class", string(r.FailureClass)))
	}
	if r.ReasonCode != "" {
		fields = append(fields, diagnostic.String("code", r.ReasonCode))
	}
	return fields
}
func (a *App) logValidation(r state.ValidationReport) {
	t := r.Totals
	fields := []diagnostic.Field{
		diagnostic.Int("run_id", r.Run.ID), diagnostic.String("origin", r.Run.Origin),
		diagnostic.String("status", string(r.Run.Status)), diagnostic.Int("selected", int64(t.Selected)),
		diagnostic.Int("pending", int64(t.Pending)), diagnostic.Int("active", int64(t.Active)),
		diagnostic.Int("completed", int64(t.Completed)),
		diagnostic.Int("skipped", int64(t.Skipped)), diagnostic.Int("interrupted", int64(t.Interrupted)),
		diagnostic.Int("confirmed", int64(t.Confirmed)), diagnostic.Int("invalid", int64(t.Invalid)),
		diagnostic.Int("inconclusive", int64(t.Inconclusive)), diagnostic.Int("unresolved", int64(t.Unresolved)),
		diagnostic.Int("unstarted", int64(t.Unstarted)),
	}
	if r.Run.ReasonCode != nil {
		fields = append(fields, diagnostic.String("code", *r.Run.ReasonCode))
	}
	diagnostic.Emit(a.Logger, "run_completed", fields...)
}
