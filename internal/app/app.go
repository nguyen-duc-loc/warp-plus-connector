// Package app coordinates source, validator, and store work without CLI types.
package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
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
	SourceNames []string
	PageLimit   *int
	Settings    config.Settings
	Source      Source
	Validator   validator.Backend
	Clock       func() time.Time
	Monotonic   func() time.Time
	RetryDraw   func(time.Duration, time.Duration) time.Duration
	Logger      *slog.Logger
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
		return &Error{Code: validation.Code, cause: validation}
	}
	for _, c := range []struct {
		err  error
		code string
	}{
		{context.Canceled, "cancelled"}, {context.DeadlineExceeded, "deadline_exceeded"},
		{store.ErrBusy, "state_busy"}, {store.ErrNotInitialized, "state_not_initialized"},
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
	if a.Logger != nil {
		a.Logger.Info("run", "run_id", runID, "result", code)
	}
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

// List streams every candidate through bounded pages at one fixed evaluation.
func (a *App) List(ctx context.Context, emit func(state.Candidate) error) (err error) {
	r, err := store.OpenReader(ctx, a.Settings.StateDir)
	if err != nil {
		return classified(err)
	}
	defer closeStore(r, &err)
	evaluation := a.evaluation()
	request := state.PageRequest{Size: 100}
	for {
		page, err := r.ListCandidates(ctx, request, evaluation)
		if err != nil {
			return classified(err)
		}
		for _, candidate := range page.Items {
			if err := ctx.Err(); err != nil {
				return classified(err)
			}
			if err := emit(candidate); err != nil {
				return &Error{Code: "output_error"}
			}
		}
		if page.Next == nil {
			return nil
		}
		request.Cursor = page.Next
	}
}
