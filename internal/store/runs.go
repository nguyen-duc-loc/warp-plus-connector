package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// StartRun starts work or returns the current record for identical creation input.
func (w *Writer) StartRun(ctx context.Context, in state.RunInput) (state.Run, error) {
	return transact(ctx, &w.Reader, true, "start run", func(tx *sql.Tx) (state.Run, error) {
		var zero state.Run
		at, err := normalizeTime(in.StartedAt)
		if err != nil || !validText(in.RequestID) || (in.Kind != state.Crawl && in.Kind != state.Validation) {
			return zero, ErrInvalidInput
		}
		existing, err := scanRun(tx.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE request_id=?", in.RequestID))
		if err == nil {
			if existing.Kind != in.Kind || !existing.StartedAt.Equal(at) {
				return zero, ErrConflict
			}
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return zero, err
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO runs(request_id,kind,status,started_at) VALUES(?,?,'running',?)", in.RequestID, in.Kind, at.UnixMicro())
		if err != nil {
			return zero, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return zero, err
		}
		return getRun(ctx, tx, id)
	})
}

// FinishRun completes execution only after every validation attempt is resolved.
func (w *Writer) FinishRun(ctx context.Context, in state.RunCompletion) (state.Run, error) {
	return transact(ctx, &w.Reader, true, "finish run", func(tx *sql.Tx) (state.Run, error) {
		var zero state.Run
		at, err := normalizeTime(in.FinishedAt)
		if err != nil || in.RunID <= 0 {
			return zero, ErrInvalidInput
		}
		switch in.Status {
		case state.Completed:
			if in.ReasonCode != "" {
				return zero, ErrInvalidInput
			}
		case state.Failed, state.Cancelled:
			if !reasonPattern.MatchString(in.ReasonCode) {
				return zero, ErrInvalidInput
			}
		default:
			return zero, ErrInvalidInput
		}
		r, err := getRun(ctx, tx, in.RunID)
		if err != nil {
			return zero, err
		}
		if r.Status != state.Running {
			reason := ""
			if r.ReasonCode != nil {
				reason = *r.ReasonCode
			}
			if r.Status == in.Status && r.FinishedAt != nil && r.FinishedAt.Equal(at) && reason == in.ReasonCode {
				return r, nil
			}
			return zero, ErrConflict
		}
		var unresolved int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM validation_attempts WHERE run_id=? AND outcome IS NULL", in.RunID).Scan(&unresolved); err != nil {
			return zero, err
		}
		if unresolved != 0 {
			return zero, ErrAttemptActive
		}
		if _, err = tx.ExecContext(ctx, "UPDATE runs SET status=?,finished_at=?,reason_code=? WHERE id=?", in.Status, at.UnixMicro(), optionalString(in.ReasonCode), in.RunID); err != nil {
			return zero, err
		}
		return getRun(ctx, tx, in.RunID)
	})
}
