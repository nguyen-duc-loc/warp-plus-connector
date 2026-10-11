// Package app coordinates source, validator, and store work without CLI types.
package app

import (
	"context"
	"crypto/rand"
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

// Check records one deliberate attempt. Unknown IDs create no run or attempt.
func (a *App) Check(ctx context.Context, id int64) (attempt state.Attempt, err error) {
	w, err := store.OpenWriter(ctx, a.Settings.StateDir, a.Clock)
	if err != nil {
		return attempt, classified(err)
	}
	defer closeStore(&w.Reader, &err)
	candidate, err := w.GetCandidate(ctx, id, a.evaluation())
	if err != nil {
		return attempt, classified(err)
	}
	run, err := w.StartRun(ctx, state.RunInput{RequestID: rand.Text(), Kind: state.Validation, StartedAt: a.Clock()})
	if err != nil {
		return attempt, classified(err)
	}
	completionStarted := false
	defer func() {
		if err != nil && !completionStarted {
			if saved := a.cleanup(w, run.ID, attempt.ID, err); saved != nil {
				attempt = *saved
			}
		}
		a.log(run.ID, string(attempt.Outcome))
	}()
	backend, version := a.Validator.Identity()
	attempt, err = w.BeginAttempt(
		ctx,
		state.AttemptInput{
			RequestID: rand.Text(), RunID: run.ID, CandidateID: id, BackendID: backend, BackendVersion: version,
			StartedAt: a.Clock(),
		},
	)
	if err != nil {
		return attempt, classified(err)
	}
	result, err := a.Validator.Check(
		ctx,
		validator.Input{
			Key: candidate.Key, StateDir: a.Settings.StateDir, CandidateID: id, RunID: run.ID, AttemptID: attempt.ID,
		},
	)
	if err != nil {
		return attempt, classified(err)
	}
	if err = ctx.Err(); err != nil {
		return attempt, classified(err)
	}
	// Once completion begins, an uncertain commit is left for recovery. Never
	// replace it with a conflicting cancellation payload.
	completionStarted = true
	attempt, err = w.CompleteAttempt(
		ctx,
		state.AttemptCompletion{
			AttemptID: attempt.ID, FinishedAt: a.Clock(), Outcome: result.Outcome, ReasonCode: result.ReasonCode,
			Evidence: result.Evidence,
		},
	)
	if err != nil {
		return attempt, classified(err)
	}
	_, err = w.FinishRun(ctx, state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: a.Clock()})
	if err != nil {
		return attempt, classified(err)
	}
	if attempt.Outcome == state.Inconclusive {
		return attempt, &Error{Code: result.ReasonCode}
	}
	return attempt, nil
}

func (a *App) cleanup(w *store.Writer, runID, attemptID int64, cause error) *state.Attempt {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code := classified(cause).(*Error).Code
	status := state.Failed
	if errors.Is(cause, context.Canceled) {
		code, status = "cancelled", state.Cancelled
	}
	var saved *state.Attempt
	if attemptID != 0 {
		completed, err := w.CompleteAttempt(
			ctx,
			state.AttemptCompletion{
				AttemptID: attemptID, FinishedAt: a.Clock(), Outcome: state.Inconclusive, ReasonCode: code,
			},
		)
		if err != nil {
			return nil
		}
		saved = &completed
	}
	_, _ = w.FinishRun(ctx, state.RunCompletion{RunID: runID, Status: status, FinishedAt: a.Clock(), ReasonCode: code})
	return saved
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
