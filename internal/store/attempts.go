package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// BeginAttempt records one invocation. Reusing its token with identical creation
// input returns its current state, even after the run has finished.
func (w *Writer) BeginAttempt(ctx context.Context, in state.AttemptInput) (state.Attempt, error) {
	return transact(
		ctx, &w.Reader, true, "begin attempt", func(tx *sql.Tx) (state.Attempt, error) {
			runID := in.RunID
			batch, err := hasBatch(ctx, tx, runID)
			if err != nil {
				return state.Attempt{}, err
			}
			if batch {
				return state.Attempt{}, ErrInvalidInput
			}
			return beginAttempt(ctx, tx, in)
		},
	)
}

func beginAttempt(ctx context.Context, tx *sql.Tx, in state.AttemptInput) (state.Attempt, error) {
	var zero state.Attempt
	at, err := normalizeTime(in.StartedAt)
	if err != nil || in.RunID <= 0 || in.CandidateID <= 0 || !validText(in.RequestID) ||
		!validText(in.BackendID) || !validText(in.BackendVersion) {
		return zero, ErrInvalidInput
	}
	existing, err := scanAttempt(
		tx.QueryRowContext(
			ctx,
			"SELECT "+attemptColumns+" FROM validation_attempts WHERE request_id=?",
			in.RequestID,
		),
	)
	if err == nil {
		if existing.RunID != in.RunID || existing.CandidateID != in.CandidateID ||
			existing.BackendID != in.BackendID ||
			existing.BackendVersion != in.BackendVersion || !existing.StartedAt.Equal(at) {
			return zero, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	run, err := getRun(ctx, tx, in.RunID)
	if err != nil {
		return zero, err
	}
	if run.Kind != state.Validation {
		return zero, ErrInvalidInput
	}
	if err = requireCandidate(ctx, tx, in.CandidateID); err != nil {
		return zero, err
	}
	if run.Status != state.Running {
		return zero, ErrRunClosed
	}
	var count int
	if err = tx.QueryRowContext(
		ctx,
		"SELECT count(*) FROM validation_attempts WHERE candidate_id=? AND outcome IS NULL",
		in.CandidateID,
	).Scan(&count); err != nil {
		return zero, err
	}
	if count > 0 {
		return zero, ErrAttemptActive
	}
	result, err := tx.ExecContext(
		ctx,
		"INSERT INTO validation_attempts(request_id,run_id,candidate_id,"+
			"backend_id,backend_version,started_at)\n   VALUES(?,?,?,?,?,?)",
		in.RequestID,
		in.RunID,
		in.CandidateID,
		in.BackendID,
		in.BackendVersion,
		at.UnixMicro(),
	)
	if err != nil {
		return zero, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return zero, err
	}
	return getAttempt(ctx, tx, id)
}

// CompleteAttempt records one terminal result. Definitive evidence is a closed
// type, and inconclusive outcomes never overwrite earlier definitive attempts.
func (w *Writer) CompleteAttempt(ctx context.Context, in state.AttemptCompletion) (state.Attempt, error) {
	if in.Evidence != nil {
		value := *in.Evidence
		in.Evidence = &value
	}
	return transact(
		ctx, &w.Reader, true, "complete attempt", func(tx *sql.Tx) (state.Attempt, error) {
			a, err := getAttempt(ctx, tx, in.AttemptID)
			if err != nil {
				return state.Attempt{}, err
			}
			runID := a.RunID
			batch, err := hasBatch(ctx, tx, runID)
			if err != nil {
				return state.Attempt{}, err
			}
			if batch {
				return state.Attempt{}, ErrInvalidInput
			}
			saved, err := completeAttempt(ctx, tx, in)
			if err != nil {
				return saved, err
			}
			if a.Outcome == "" {
				err = advanceCandidate(ctx, tx, a.CandidateID, completionPolicy(in.SchedulePolicy))
			}
			return saved, err
		},
	)
}

func completeAttempt(ctx context.Context, tx *sql.Tx, in state.AttemptCompletion) (state.Attempt, error) {
	var zero state.Attempt
	if err := saveCompletionPolicy(ctx, tx, "validation_attempts", in.AttemptID, 0, in.SchedulePolicy); err != nil {
		return zero, err
	}
	at, err := normalizeTime(in.FinishedAt)
	if err != nil || in.AttemptID <= 0 {
		return zero, ErrInvalidInput
	}
	encoded, err := encodeEvidence(in.Outcome, in.ReasonCode, in.Evidence)
	if err != nil {
		return zero, err
	}
	a, err := getAttempt(ctx, tx, in.AttemptID)
	if err != nil {
		return zero, err
	}
	if a.Outcome != "" {
		reason := ""
		if a.ReasonCode != nil {
			reason = *a.ReasonCode
		}
		saved, err := encodeEvidence(a.Outcome, reason, a.Evidence)
		if err != nil {
			return zero, ErrStateAccess
		}
		if a.Outcome == in.Outcome && reason == in.ReasonCode && a.FinishedAt != nil &&
			a.FinishedAt.Equal(at) && reflect.DeepEqual(
			saved,
			encoded,
		) {
			return a, nil
		}
		return zero, ErrConflict
	}
	run, err := getRun(ctx, tx, a.RunID)
	if err != nil {
		return zero, err
	}
	if run.Status != state.Running {
		return zero, ErrRunClosed
	}
	if _, err = tx.ExecContext(
		ctx,
		"UPDATE validation_attempts SET outcome=?,finished_at=?,reason_code=?,evidence_json=? WHERE id=?",
		in.Outcome,
		at.UnixMicro(),
		optionalString(in.ReasonCode),
		encoded,
		in.AttemptID,
	); err != nil {
		return zero, err
	}
	return getAttempt(ctx, tx, in.AttemptID)
}
