package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
)

// WorkReview freezes a plan and its private identity before mutation admission.
type WorkReview struct {
	ID          int64
	Kind        string
	CreatedAt   time.Time
	Binding     store.CollectionBinding
	Sources     []state.SourcePlan
	Selection   string
	CandidateID *int64
	View        *ResultsView
	Candidates  []state.Candidate
	Policy      state.CheckPolicy
}

// WorkerActivity contains only coordinator identities and an approved phase.
type WorkerActivity struct {
	CandidateID   int64
	AttemptNumber int
	Phase         string
}

// WorkState is a coherent copy of the latest acknowledged operational facts.
type WorkState struct {
	ID, Revision                                     int64
	Kind, State, Phase                               string
	ObservedAt                                       time.Time
	Elapsed                                          time.Duration
	StopElapsed                                      *time.Duration
	RunID                                            int64
	RunCreated, Persistence, Cleanup, ErrorCode      string
	RunStatus                                        state.RunStatus
	CancellationRequested                            bool
	Crawl                                            *state.CrawlReport
	Check                                            *state.ValidationReport
	CurrentChannel                                   string
	CurrentPage                                      int
	AttemptsStarted, AttemptsCompleted, RetryWaiting int
	NextRetry                                        *time.Duration
	Workers                                          []WorkerActivity
	Unresolved                                       *WorkState
	Guidance                                         string
	RetryAt, ServiceObservedAt                       *time.Time
}

// WorkSession owns one operation until all resources close and its outcome is acknowledged.
// Its observations never wait for a store, provider, or terminal write.
type WorkSession struct {
	mu                        sync.Mutex
	app                       *App
	next                      int64
	current                   *WorkState
	started, stopped, retryAt time.Time
	cancel                    context.CancelFunc
	done                      chan struct{}
	unsafe                    bool
	closed                    bool
	retained                  *WorkState
	binding                   *validator.CleanupBinding
	observationErr            error
	Observe                   func(WorkState)
	Guard                     *OperationGuard
	finished                  time.Time
	checkItems                map[int64]state.ValidationItem
}

// NewWorkSession allocates only memory. Reviews and idle sessions own no writer.
func (a *App) NewWorkSession() *WorkSession { return &WorkSession{app: a, Guard: &OperationGuard{}} }

// ReviewCrawl captures the configured ordered plan without contacting sources.
func (s *WorkSession) ReviewCrawl(channels []string, limit *int) (*WorkReview, error) {
	if len(channels) == 0 {
		return nil, &Error{Code: "state_invalid_input"}
	}
	seen := map[string]bool{}
	for _, c := range channels {
		if seen[c] {
			return nil, &Error{Code: "state_invalid_input"}
		}
		normalized, valid := state.NormalizeChannel(c)
		if !valid || (normalized != c && seen[normalized]) {
			return nil, &Error{Code: "state_invalid_input"}
		}
		seen[normalized] = true
	}
	plan, err := s.app.Settings.SelectSources(channels, limit)
	if err != nil {
		return nil, &Error{Code: "state_invalid_input"}
	}
	binding, err := store.BindCollection(s.app.Settings.StateDir)
	if err != nil {
		return nil, classified(err)
	}
	return &WorkReview{Kind: "crawl", CreatedAt: s.app.Clock(), Binding: binding, Sources: plan}, nil
}

// ReviewCheck captures complete selected identities from the fixed Results view.
func (s *WorkSession) ReviewCheck(
	ctx context.Context, results *ResultsSession,
	selection string, candidate *int64,
) (*WorkReview, error) {
	if s.app.Settings.Validator != "docker" {
		return nil, &Error{Code: "validation_unsupported"}
	}
	if results.View == nil {
		return nil, &Error{Code: "ui_handle_stale"}
	}
	if !results.identity.Same(s.app.Settings.StateDir) {
		return nil, &Error{Code: "ui_collection_changed"}
	}
	v := *results.View
	r := &WorkReview{
		Kind: "check", CreatedAt: s.app.Clock(), Binding: store.CollectionBinding{Identity: results.identity},
		Selection: selection, CandidateID: candidate, View: &v, Policy: s.app.Settings.CheckPolicy,
	}
	if selection == "focused" {
		if candidate == nil {
			return nil, &Error{Code: "state_invalid_input"}
		}
		row, err := results.Candidate(ctx, *candidate)
		if err != nil {
			return nil, err
		}
		r.Candidates = []state.Candidate{row.Candidate}
	} else {
		if selection != "marked" && selection != "matching" {
			return nil, &Error{Code: "state_invalid_input"}
		}
		if err := results.VisitExport(
			ctx, selection == "marked", func(c state.Candidate) error {
				r.Candidates = append(r.Candidates, c)
				return nil
			},
		); err != nil {
			return nil, err
		}
	}
	if len(r.Candidates) == 0 {
		return nil, &Error{Code: "ui_work_selection_empty"}
	}
	return r, nil
}

// Busy includes a completed operation whose outcome has not been acknowledged.
func (s *WorkSession) Busy() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.current != nil }

func (s *WorkSession) changed() {
	s.current.Revision++
	s.current.ObservedAt = s.app.Clock()
	if s.Observe != nil {
		s.Observe(s.copyLocked())
	}
}

func (s *WorkSession) copyLocked() WorkState {
	v := *s.current
	now := time.Now()
	if !s.finished.IsZero() {
		now = s.finished
	}
	v.Elapsed = now.Sub(s.started)
	if !s.stopped.IsZero() {
		d := now.Sub(s.stopped)
		v.StopElapsed = &d
	}
	if !s.retryAt.IsZero() {
		d := max(time.Duration(0), time.Until(s.retryAt))
		v.NextRetry = &d
	}
	v.Unresolved = s.retained
	v.Workers = append([]WorkerActivity{}, v.Workers...)
	return v
}

// Start reserves the slot before dispatch. The caller consumes the review once.
func (s *WorkSession) Start(ctx context.Context, review *WorkReview) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.unsafe || (review.Kind == "check" && s.retained != nil && s.binding == nil) {
		return 0, &Error{Code: "ui_work_recovery_required"}
	}
	if s.current != nil {
		return 0, &Error{Code: "ui_busy"}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !s.Guard.Arm() {
		return 0, &Error{Code: "ui_busy"}
	}
	s.next++
	ctx, s.cancel = context.WithCancel(ctx)
	s.started, s.stopped, s.retryAt = time.Now(), time.Time{}, time.Time{}
	s.done = make(chan struct{})
	s.observationErr = nil
	s.checkItems = map[int64]state.ValidationItem{}
	s.finished = time.Time{}
	s.current = &WorkState{
		ID: s.next, Kind: review.Kind, State: "running", Phase: "admission",
		RunCreated: "false", Persistence: "known", Cleanup: "not_needed", Workers: []WorkerActivity{},
	}
	s.changed()
	cancel := s.cancel
	go func() { defer cancel(); s.execute(ctx, review) }()
	return s.next, nil
}

// Poll returns an immutable observation. Cancel records the first stop only once.
func (s *WorkSession) Poll(id int64, cancel bool) (WorkState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || s.current.ID != id {
		return WorkState{}, &Error{Code: "ui_handle_stale"}
	}
	if cancel && (s.current.State == "running" || s.current.State == "cancelling") && !s.current.CancellationRequested {
		s.Guard.Stop()
		if s.stopped.IsZero() {
			s.stopped = time.Now()
		}
		s.current.State, s.current.CancellationRequested = "cancelling", true
		s.current.Phase = "cleanup"
		s.cancel()
		s.changed()
	}
	return s.copyLocked(), nil
}

// Acknowledge releases membership only after execution and owned closure finish.
func (s *WorkSession) Acknowledge(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || s.current.ID != id {
		return &Error{Code: "ui_handle_stale"}
	}
	select {
	case <-s.done:
	default:
		return &Error{Code: "ui_busy"}
	}
	s.current = nil
	return nil
}

// Close cancels execution and waits for entered cleanup before returning.
func (s *WorkSession) Close() error {
	s.mu.Lock()
	s.closed = true
	id := int64(0)
	if s.current != nil {
		id = s.current.ID
	}
	done := s.done
	s.mu.Unlock()
	if id != 0 {
		_, _ = s.Poll(id, true)
	}
	if done != nil {
		<-done
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unsafe || s.retained != nil {
		return &Error{Code: "ui_work_recovery_required"}
	}
	if s.current != nil && s.current.State == "failed" {
		return &Error{Code: s.current.ErrorCode}
	}
	return nil
}

func (s *WorkSession) execute(ctx context.Context, review *WorkReview) {
	a := *s.app
	a.shutdown = newShutdownWindow(ctx)
	stopWindow := context.AfterFunc(ctx, a.shutdown.stop)
	defer stopWindow()
	defer a.shutdown.cancelCleanup()
	err := s.run(ctx, &a, review)
	if err != nil && classified(err).(*Error).Code == "state_busy" {
		s.observeOwner(&a)
	}
	if wait := validator.AdmissionWait(err); wait != nil {
		s.mu.Lock()
		s.current.RetryAt = &wait.RetryAt
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.current
	if s.observationErr != nil {
		err = s.observationErr
	}
	s.finished = time.Now()
	v.Phase = "complete"
	v.State = "succeeded"
	if err != nil {
		v.ErrorCode = classified(err).(*Error).Code
		v.State = "failed"
		if errors.Is(err, context.Canceled) && !s.unsafe && v.Cleanup != "unresolved" {
			v.State, v.ErrorCode = "cancelled", "cancelled"
		}
	}
	if v.Cleanup == "pending" {
		v.Cleanup = "complete"
	}
	if s.unsafe {
		v.State = "failed"
		if v.ErrorCode == "" {
			v.ErrorCode = "ui_work_recovery_required"
		}
	}

	if v.Cleanup == "unresolved" {
		if s.retained == nil {
			receipt := *v
			receipt.Unresolved = nil
			s.retained = &receipt
		} else if s.retained.ID != v.ID {
			s.unsafe = true
		}
	}
	s.Guard.Disarm()
	s.changed()
	close(s.done)
}

func (s *WorkSession) run(ctx context.Context, a *App, review *WorkReview) (err error) {
	w, err := store.OpenReviewedWriter(ctx, a.Settings.StateDir, a.Clock, a.Settings.SchedulePolicy(), review.Binding)
	if err != nil {
		var uncertain *store.AdmissionUncertain
		if errors.As(err, &uncertain) {
			s.uncertain()
		}
		return classified(err)
	}
	defer func() {
		if e := w.Close(); e != nil {
			s.closureFailed(e)
			err = errors.Join(err, classified(e))
		}
	}()
	m := newMutationExecutor(nil)
	defer m.close()
	ow := &operationWriter{Writer: w, mutations: m}
	ow.creating = func() { s.mu.Lock(); defer s.mu.Unlock(); s.current.RunCreated = "unknown"; s.changed() }
	ow.observe = func(value any, e error) { s.observeMutation(ctx, ow, review.Kind, value, e) }
	a.workClosed = s.closureFailed
	a.workStop = func(code string) {
		a.shutdown.stop()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.stopped.IsZero() {
			s.stopped = time.Now()
		}
		s.Guard.Stop()
		s.current.State, s.current.Phase, s.current.ErrorCode = "cancelling", "cleanup", code
		s.changed()
	}
	a.workActivity = func(workers []WorkerActivity, waiting int, due time.Time) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.current.Workers = append([]WorkerActivity{}, workers...)
		s.current.RetryWaiting, s.retryAt = waiting, due
		if s.current.State == "running" {
			if waiting > 0 && len(workers) == 0 {
				s.current.Phase = "retry_wait"
			} else {
				s.current.Phase = "check"
			}
		}
		s.changed()
	}
	a.workCleanup = func(unresolved bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if unresolved {
			s.current.Cleanup = "unresolved"
		}
		s.changed()
	}

	a.workRetained = s.binding
	a.workResolved = func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.binding, s.retained = nil, nil
		s.changed()
	}
	a.workOwner = func(binding *validator.CleanupBinding) {
		if binding == nil {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.binding = binding
		s.current.Cleanup = "unresolved"
		s.changed()
	}
	if review.Kind == "crawl" {
		full, e := a.Settings.SelectSources(nil, nil)
		if e != nil {
			return e
		}
		if _, e = w.PrepareSchedule(ctx, a.Clock(), a.Settings.SchedulePolicy(), full, false); e != nil {
			s.uncertain()
			return classified(e)
		}
		_, err = a.crawlOwned(ctx, ow, review.Sources, "manual")
		return err
	}
	ids := make([]int64, len(review.Candidates))
	for i, expected := range review.Candidates {
		candidate, e := w.GetCandidate(ctx, expected.ID, a.evaluation())
		if errors.Is(e, store.ErrNotFound) {
			return &Error{Code: "ui_candidate_unavailable"}
		}
		if e != nil {
			return classified(e)
		}
		if candidate.Key != expected.Key {
			return &Error{Code: "ui_candidate_unavailable"}
		}
		ids[i] = expected.ID
	}
	a.Settings.CheckPolicy = review.Policy
	a.shutdown.startRemote()
	_, err = a.checkOwned(ctx, ow, ids, nil, "manual", nil)
	return err
}

func (s *WorkSession) uncertain() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unsafe = true
	s.current.Persistence = "uncertain"
	s.Guard.Stop()
	s.cancel()
	s.changed()
}

func (s *WorkSession) observeMutation(ctx context.Context, w *operationWriter, kind string, value any, err error) {
	if err != nil {
		s.uncertain()
		return
	}
	if kind == "check" {
		s.observeCheck(ctx, w, value)
		return
	}
	var report state.CrawlReport
	if saved, ok := value.(state.CrawlReport); ok {
		report = saved
	} else {
		s.mu.Lock()
		id := s.current.RunID
		s.mu.Unlock()
		if id == 0 {
			return
		}
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		report, err = w.GetCrawlReport(readCtx, id)
		cancel()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.current
	if err != nil {
		s.observationErr = classified(err)
		s.Guard.Stop()
		s.cancel()
	} else {
		v.Crawl = &report
		v.RunID, v.RunStatus, v.RunCreated = report.Run.ID, report.Run.Status, "true"
		if page, ok := value.(state.CrawlPageRecord); ok {
			v.CurrentChannel, v.CurrentPage = strings.TrimPrefix(page.SourceID, "telegram:"), page.PageNumber
		}
	}
	if v.RunStatus != "" && v.RunStatus != state.Running {
		v.Phase = "finalization"
	} else if v.State == "running" {
		v.Phase = "crawl"
	}
	s.changed()
}

// Unresolved returns the original cleanup receipt without exposing recovery identities.
func (s *WorkSession) Unresolved() *WorkState { s.mu.Lock(); defer s.mu.Unlock(); return s.retained }

func (s *WorkSession) observeOwner(a *App) {
	if a.OverviewService == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observed := a.OverviewService(ctx)
	yes := func(v *bool) bool { return v != nil && *v }
	no := func(v *bool) bool { return v != nil && !*v }
	manager, runner := observed.Manager, observed.Runner
	if ctx.Err() != nil || observed.IdentityState != "verified" || observed.InstallationID == nil ||
		observed.CollectionMatch != "match" || manager.Availability != "available" || runner.Availability != "available" ||
		manager.ClockAnomaly || runner.ClockAnomaly || manager.Data == nil || runner.Data == nil ||
		!yes(manager.Data.Running) || !yes(runner.Data.LockHeld) || !yes(runner.Data.InstallationMatches) ||
		runner.Data.OwnerPID == nil || *runner.Data.OwnerPID <= 0 || !no(runner.Data.Stale) || runner.Data.ClockAnomaly {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current.Guidance = "stop_service"
	s.current.ServiceObservedAt = &runner.ObservedAt
	s.changed()
}

func (s *WorkSession) observeCheck(ctx context.Context, w *operationWriter, value any) {
	var item *state.ValidationItem
	var err error
	switch v := value.(type) {
	case state.BatchAttempt:
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		saved, e := w.GetValidationItem(readCtx, v.Attempt.RunID, v.Attempt.CandidateID)
		cancel()
		item, err = &saved, e
	case state.ValidationItem:
		item = &v
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.current
	if err != nil {
		s.observationErr = classified(err)
		s.Guard.Stop()
		s.cancel()
	} else if header, ok := value.(state.ValidationReport); ok {
		v.Check = &header
		v.RunID, v.RunStatus, v.RunCreated = header.Run.ID, header.Run.Status, "true"
	} else if item != nil && v.Check != nil {
		report := *v.Check
		old, ok := s.checkItems[item.CandidateID]
		if !ok {
			old = state.ValidationItem{Status: "pending"}
		}
		adjustCheckTotals(&report.Totals, old, -1)
		adjustCheckTotals(&report.Totals, *item, 1)
		s.checkItems[item.CandidateID] = *item
		v.Check = &report
		if attempt, ok := value.(state.BatchAttempt); ok {
			if attempt.Attempt.Outcome == "" {
				v.AttemptsStarted++
				if v.Cleanup != "unresolved" {
					v.Cleanup = "pending"
				}
			} else {
				v.AttemptsCompleted++
			}
		}
	}
	if v.RunStatus != "" && v.RunStatus != state.Running {
		v.Phase = "finalization"
	} else if v.State == "running" {
		v.Phase = "check"
	}
	s.changed()
}

func adjustCheckTotals(t *state.ValidationTotals, item state.ValidationItem, delta int) {
	switch item.Status {
	case "pending":
		t.Pending += delta
	case "active":
		t.Active += delta
	case "completed":
		t.Completed += delta
	case "skipped":
		t.Skipped += delta
	case "interrupted":
		t.Interrupted += delta
	}
	if item.LatestAttempt == nil {
		t.Unstarted += delta
		return
	}
	switch item.LatestAttempt.Outcome {
	case "":
		t.Unresolved += delta
	case state.Confirmed:
		t.Confirmed += delta
	case state.Invalid:
		t.Invalid += delta
	case state.Inconclusive:
		t.Inconclusive += delta
	}
}

func (s *WorkSession) closureFailed(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unsafe = true
	s.current.Guidance = "restart_session"
	s.Guard.Stop()
	s.changed()
}
