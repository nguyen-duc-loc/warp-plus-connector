// Package app coordinates source, validator, and store work without CLI types.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/diagnostic"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/rs/zerolog"
)

// Source retrieves one fully parsed bounded page.
type Source interface {
	FetchPage(context.Context, string, *int64) (telegram.Page, error)
}

// Error carries a stable diagnostic and preserves only known safe causes.
type Error struct {
	Code  string
	cause error
}

func (e *Error) Error() string { return e.Code }
func (e *Error) Unwrap() error { return e.cause }

// App uses a concrete store and small interfaces at external boundaries.
type App struct {
	shutdown     *shutdownWindow
	workActivity func([]WorkerActivity, int, time.Time)
	workCleanup  func(bool)
	workRetained *validator.CleanupBinding
	workResolved func()
	workOwner    func(*validator.CleanupBinding)
	workStop     func(string)
	workClosed   func(error)
	// Only the single active scheduled batch accesses this monotonic deadline.
	nextMutation time.Time
	SourceNames  []string
	PageLimit    *int
	Settings     config.Settings
	Source       Source
	Validator    validator.Backend
	Clock        func() time.Time
	Monotonic    func() time.Time
	RetryDraw    func(time.Duration, time.Duration) time.Duration
	Logger       *zerolog.Logger
	// Observe runs only in the scheduler coordinator.
	Observe func(RunnerState) error
	// Finalize runs after entered work closes, under the shared local cleanup deadline.
	Finalize func(context.Context, error) error
	// OverviewService supplies the account observation boundary without a service import cycle.
	OverviewService func(context.Context) state.OverviewService
}

func (a *App) evaluation() state.Evaluation {
	return state.Evaluation{AsOf: a.Clock().UTC(), MaxAge: a.Settings.RecheckInterval}
}

func classified(err error) error {
	if err == nil {
		return nil
	}
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	var source *telegram.Error
	if errors.As(err, &source) {
		return &Error{Code: source.Reason, cause: source}
	}
	var validation *validator.Error
	if errors.As(err, &validation) {
		if validator.AdmissionWait(err) != nil {
			return &Error{Code: validation.Code, cause: err}
		}
		return &Error{Code: validation.Code, cause: validation}
	}
	for _, c := range []struct {
		err  error
		code string
	}{
		{context.Canceled, "cancelled"}, {context.DeadlineExceeded, "deadline_exceeded"},
		{
			store.ErrCollectionChanged,
			"ui_collection_changed",
		}, {store.ErrBusy, "state_busy"}, {store.ErrNotInitialized, "state_not_initialized"},
		{store.ErrSchemaTooNew, "state_schema_too_new"}, {store.ErrMigrationRequired, "state_migration_required"},
		{store.ErrNotFound, "state_record_not_found"}, {store.ErrInvalidInput, "state_invalid_input"},
	} {
		if errors.Is(err, c.err) {
			return &Error{Code: c.code, cause: c.err}
		}
	}
	return &Error{Code: "state_error", cause: store.ErrStateAccess}
}

func closeStore(r *store.Reader, err *error) { *err = classified(errors.Join(*err, r.Close())) }

func (a *App) log(runID int64, code string) {
	diagnostic.Emit(
		a.Logger, "work_diagnostic", diagnostic.Int("run_id", runID),
		diagnostic.String("code", code), diagnostic.String("result", code),
	)
}

// Check uses the batch engine for compatibility with single candidate callers.
func (a *App) Check(ctx context.Context, id int64) (attempt state.Attempt, err error) {
	_, err = a.CheckBatch(
		ctx, []int64{id}, func(item state.ValidationItem) error {
			if item.LatestAttempt != nil {
				attempt = *item.LatestAttempt
			}
			return nil
		},
	)
	return attempt, err
}

// ShowCrawl opens only a reader and retrieves persisted progress after restart.
func (a *App) ShowCrawl(ctx context.Context, id int64) (report state.CrawlReport, err error) {
	r, err := store.OpenReader(ctx, a.Settings.StateDir)
	if err != nil {
		return report, classified(err)
	}
	defer closeStore(r, &err)
	report, err = r.GetCrawlReport(ctx, id)
	return report, classified(err)
}

// List visits every candidate from one snapshot at a fixed evaluation.
func (a *App) List(ctx context.Context, emit func(state.Candidate) error) error {
	_, err := a.QueryCandidates(
		ctx, CandidateQuery{}, func(c state.Candidate) error {
			if err := emit(c); err != nil {
				return &Error{Code: "output_error"}
			}
			return nil
		},
	)
	return err
}
