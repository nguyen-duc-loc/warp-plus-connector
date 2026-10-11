package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

func hasBatch(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, "SELECT count(*) FROM validation_batches WHERE run_id=?", id).Scan(&n)
	return n != 0, err
}

// StartValidationBatch atomically saves the run, immutable policy, and selection.
func (w *Writer) StartValidationBatch(ctx context.Context, in state.ValidationBatchInput) (
	state.ValidationReport,
	error,
) {
	in.CandidateIDs = append([]int64(nil), in.CandidateIDs...)
	return transact(
		ctx, &w.Reader, true, "start validation batch", func(tx *sql.Tx) (state.ValidationReport, error) {
			var zero state.ValidationReport
			at, err := normalizeTime(in.StartedAt)
			if err != nil || !validText(in.RequestID) || !validText(in.BackendID) || !validText(in.BackendVersion) ||
				!in.Policy.Valid() || len(in.CandidateIDs) == 0 {
				return zero, ErrInvalidInput
			}
			seen := map[int64]bool{}
			for _, id := range in.CandidateIDs {
				if id <= 0 || seen[id] {
					return zero, ErrInvalidInput
				}
				seen[id] = true
				if err := requireCandidate(ctx, tx, id); err != nil {
					return zero, err
				}
			}
			existing, err := scanRun(
				tx.QueryRowContext(
					ctx,
					"SELECT "+runColumns+" FROM runs WHERE request_id=?",
					in.RequestID,
				),
			)
			if err == nil {
				report, err := getValidationReport(ctx, tx, existing.ID)
				if err != nil {
					return zero, err
				}
				if report.Policy == nil || !reflect.DeepEqual(*report.Policy, in.Policy) ||
					report.BackendID != in.BackendID || report.BackendVersion != in.BackendVersion || !existing.StartedAt.Equal(at) {
					return zero, ErrConflict
				}
				rows, err := tx.QueryContext(
					ctx,
					"SELECT candidate_id FROM validation_items WHERE run_id=? ORDER BY ordinal",
					existing.ID,
				)
				if err != nil {
					return zero, err
				}
				var ids []int64
				for rows.Next() {
					var id int64
					if err = rows.Scan(&id); err != nil {
						break
					}
					ids = append(ids, id)
				}
				err = errors.Join(err, rows.Err(), rows.Close())
				if err != nil {
					return zero, err
				}
				if !reflect.DeepEqual(ids, in.CandidateIDs) {
					return zero, ErrConflict
				}
				return report, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return zero, err
			}
			result, err := tx.ExecContext(
				ctx, "INSERT INTO runs(request_id,kind,status,started_at) VALUES(?,'validation','running',?)",
				in.RequestID, at.UnixMicro(),
			)
			if err != nil {
				return zero, err
			}
			id, err := result.LastInsertId()
			if err != nil {
				return zero, err
			}
			policy, err := json.Marshal(in.Policy)
			if err != nil {
				return zero, ErrInvalidInput
			}
			if _, err = tx.ExecContext(
				ctx, "INSERT INTO validation_batches VALUES(?,?,?,?)", id, in.BackendID, in.BackendVersion,
				string(policy),
			); err != nil {
				return zero, err
			}
			for i, candidate := range in.CandidateIDs {
				if _, err = tx.ExecContext(
					ctx, "INSERT INTO validation_items(run_id,candidate_id,ordinal,status) VALUES(?,?,?,'pending')",
					id, candidate, i+1,
				); err != nil {
					return zero, err
				}
			}
			return getValidationReport(ctx, tx, id)
		},
	)
}

// BeginBatchAttempt records sequential intent and starts the item atomically.
func (w *Writer) BeginBatchAttempt(ctx context.Context, in state.BatchAttemptInput) (state.BatchAttempt, error) {
	return transact(
		ctx, &w.Reader, true, "begin batch attempt", func(tx *sql.Tx) (state.BatchAttempt, error) {
			var zero state.BatchAttempt
			report, err := getValidationMetadata(ctx, tx, in.RunID)
			if err != nil {
				return zero, err
			}
			if report.Policy == nil || in.Number < 1 || in.Number > report.Policy.MaxAttempts ||
				in.BackendID != report.BackendID || in.BackendVersion != report.BackendVersion {
				return zero, ErrInvalidInput
			}
			var existingID int64
			err = tx.QueryRowContext(
				ctx,
				"SELECT id FROM validation_attempts WHERE request_id=?",
				in.RequestID,
			).Scan(&existingID)
			if err == nil {
				a, err := beginAttempt(ctx, tx, in.AttemptInput)
				if err != nil {
					return zero, err
				}
				detail, err := getBatchAttempt(ctx, tx, a.ID)
				if err != nil {
					return zero, err
				}
				if detail.Number != in.Number {
					return zero, ErrConflict
				}
				return detail, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return zero, err
			}
			item, err := getValidationItem(ctx, tx, in.RunID, in.CandidateID)
			if err != nil {
				return zero, err
			}
			if (in.Number == 1 && item.Status != "pending") || (in.Number > 1 && item.Status != "active") ||
				item.Attempts+1 != in.Number {
				return zero, ErrConflict
			}
			if in.Number > 1 {
				previous, err := getBatchAttempt(ctx, tx, item.LatestAttempt.ID)
				if err != nil {
					return zero, err
				}
				if previous.RetryEligibleAt == nil || previous.Attempt.Outcome == "" {
					return zero, ErrConflict
				}
			}
			a, err := beginAttempt(ctx, tx, in.AttemptInput)
			if err != nil {
				return zero, err
			}
			if _, err = tx.ExecContext(
				ctx, "INSERT INTO validation_attempt_details(attempt_id,run_id,candidate_id,attempt_number) "+
					"VALUES(?,?,?,?)", a.ID, a.RunID, a.CandidateID, in.Number,
			); err != nil {
				return zero, err
			}
			if in.Number == 1 {
				if _, err = tx.ExecContext(
					ctx, "UPDATE validation_items SET status='active',started_at=? WHERE run_id=? AND candidate_id=?",
					a.StartedAt.UnixMicro(), a.RunID, a.CandidateID,
				); err != nil {
					return zero, err
				}
			}
			return getBatchAttempt(ctx, tx, a.ID)
		},
	)
}

// CompleteBatchAttempt saves the terminal result and retry plan in one commit.
func (w *Writer) CompleteBatchAttempt(ctx context.Context, in state.BatchAttemptCompletion) (
	state.BatchAttempt,
	error,
) {
	if in.Evidence != nil {
		v := *in.Evidence
		v.ObservedAt = v.ObservedAt.UTC().Truncate(time.Microsecond)
		in.Evidence = &v
	}
	if in.RetryEligibleAt != nil {
		v := in.RetryEligibleAt.UTC().Truncate(time.Microsecond)
		in.RetryEligibleAt = &v
	}
	in.FinishedAt = in.FinishedAt.UTC().Truncate(time.Microsecond)
	return transact(
		ctx, &w.Reader, true, "complete batch attempt", func(tx *sql.Tx) (state.BatchAttempt, error) {
			var zero state.BatchAttempt
			if in.RetryWait < 0 || in.RetryWait%time.Microsecond != 0 || (in.RetryWait > 0) != (in.RetryEligibleAt != nil) {
				return zero, ErrInvalidInput
			}
			if in.RetryEligibleAt != nil {
				if _, err := normalizeTime(*in.RetryEligibleAt); err != nil {
					return zero, err
				}
			}
			switch in.FailureClass {
			case "", state.Temporary, state.Throttled, state.Terminal, state.Unsafe:
			default:
				return zero, ErrInvalidInput
			}
			if in.Outcome == state.Inconclusive {
				if in.FailureClass == "" || (in.RetryWait == 0 && !reasonPattern.MatchString(in.StopReason)) {
					return zero, ErrInvalidInput
				}
			} else if in.FailureClass != "" || in.StopReason != "" || in.RetryWait != 0 {
				return zero, ErrInvalidInput
			}
			if in.RetryWait > 0 && (in.StopReason != "" ||
				(in.FailureClass != state.Temporary && in.FailureClass != state.Throttled)) {
				return zero, ErrInvalidInput
			}
			detail, err := getBatchAttempt(ctx, tx, in.AttemptID)
			if err != nil {
				return zero, err
			}
			encoded, err := json.Marshal(in)
			if err != nil {
				return zero, ErrInvalidInput
			}
			var saved sql.NullString
			if err = tx.QueryRowContext(
				ctx, "SELECT completion_json FROM validation_attempt_details WHERE attempt_id=?",
				in.AttemptID,
			).Scan(&saved); err != nil {
				return zero, err
			}
			if saved.Valid {
				if saved.String != string(encoded) {
					return zero, ErrConflict
				}
				return detail, nil
			}
			if in.RetryWait > 0 {
				report, err := getValidationMetadata(ctx, tx, detail.Attempt.RunID)
				if err != nil {
					return zero, err
				}
				if detail.Number >= report.Policy.MaxAttempts {
					return zero, ErrInvalidInput
				}
			}
			a, err := completeAttempt(ctx, tx, in.AttemptCompletion)
			if err != nil {
				return zero, err
			}
			var wait, due any
			if in.RetryEligibleAt != nil {
				wait = in.RetryWait.Microseconds()
				due = in.RetryEligibleAt.UnixMicro()
			}
			if _, err = tx.ExecContext(
				ctx,
				"UPDATE validation_attempt_details SET failure_class=?,retry_wait_us=?,"+
					"retry_eligible_at=?,completion_json=? WHERE attempt_id=?",
				optionalString(string(in.FailureClass)),
				wait,
				due,
				string(encoded),
				a.ID,
			); err != nil {
				return zero, err
			}
			if in.RetryWait == 0 {
				if _, err = tx.ExecContext(
					ctx,
					"UPDATE validation_items SET status='completed',finished_at=?,stop_reason=? "+
						"WHERE run_id=? AND candidate_id=?",
					a.FinishedAt.UnixMicro(),
					optionalString(in.StopReason),
					a.RunID,
					a.CandidateID,
				); err != nil {
					return zero, err
				}
			}
			return getBatchAttempt(ctx, tx, a.ID)
		},
	)
}

// FinishValidationItem closes queued or waiting work without inventing an attempt.
func (w *Writer) FinishValidationItem(ctx context.Context, in state.ValidationItemCompletion) (
	state.ValidationItem,
	error,
) {
	return transact(
		ctx, &w.Reader, true, "finish validation item", func(tx *sql.Tx) (state.ValidationItem, error) {
			return finishValidationItem(ctx, tx, in)
		},
	)
}

func finishValidationItem(ctx context.Context, tx *sql.Tx, in state.ValidationItemCompletion) (
	state.ValidationItem,
	error,
) {
	var zero state.ValidationItem
	at, err := normalizeTime(in.FinishedAt)
	if err != nil || !reasonPattern.MatchString(in.StopReason) {
		return zero, ErrInvalidInput
	}
	item, err := getValidationItem(ctx, tx, in.RunID, in.CandidateID)
	if err != nil {
		return zero, err
	}
	if item.Status != "pending" && item.Status != "active" {
		if item.StopReason == in.StopReason && item.FinishedAt != nil && item.FinishedAt.Equal(at) {
			return item, nil
		}
		return zero, ErrConflict
	}
	if item.LatestAttempt != nil && item.LatestAttempt.Outcome == "" {
		return zero, ErrAttemptActive
	}
	status := "completed"
	if item.Attempts == 0 {
		status = "skipped"
	}
	if _, err = tx.ExecContext(
		ctx, "UPDATE validation_items SET status=?,finished_at=?,stop_reason=? "+
			"WHERE run_id=? AND candidate_id=?", status, at.UnixMicro(), in.StopReason, in.RunID, in.CandidateID,
	); err != nil {
		return zero, err
	}
	return getValidationItem(ctx, tx, in.RunID, in.CandidateID)
}

// StopValidationBatch closes remaining eligible items and the run atomically.
func (w *Writer) StopValidationBatch(ctx context.Context, in state.RunCompletion) (state.ValidationReport, error) {
	return w.finishValidationBatch(ctx, in, true)
}

// FinishValidationBatch requires every selected item to be terminal.
func (w *Writer) FinishValidationBatch(ctx context.Context, in state.RunCompletion) (state.ValidationReport, error) {
	return w.finishValidationBatch(ctx, in, false)
}

func (w *Writer) finishValidationBatch(ctx context.Context, in state.RunCompletion, stop bool) (
	state.ValidationReport,
	error,
) {
	return transact(
		ctx, &w.Reader, true, "finish validation batch", func(tx *sql.Tx) (state.ValidationReport, error) {
			var zero state.ValidationReport
			at, err := normalizeTime(in.FinishedAt)
			if err != nil {
				return zero, err
			}
			if (stop && (in.Status != state.Failed && in.Status != state.Cancelled ||
				!reasonPattern.MatchString(in.ReasonCode))) ||
				(!stop && (in.Status != state.Completed || in.ReasonCode != "")) {
				return zero, ErrInvalidInput
			}
			report, err := getValidationReport(ctx, tx, in.RunID)
			if err != nil {
				return zero, err
			}
			if report.Policy == nil {
				return zero, ErrInvalidInput
			}
			if report.Run.Status != state.Running {
				reason := ""
				if report.Run.ReasonCode != nil {
					reason = *report.Run.ReasonCode
				}
				if report.Run.Status == in.Status && report.Run.FinishedAt != nil && report.Run.FinishedAt.Equal(at) &&
					reason == in.ReasonCode {
					return report, nil
				}
				return zero, ErrConflict
			}
			if report.Totals.Unresolved > 0 {
				return zero, ErrAttemptActive
			}
			if stop {
				if _, err = tx.ExecContext(
					ctx, "UPDATE validation_items SET status=CASE WHEN status='pending' THEN 'skipped' "+
						"ELSE 'completed' END,finished_at=?,stop_reason=? WHERE run_id=? AND status IN ('pending','active')",
					at.UnixMicro(), in.ReasonCode, in.RunID,
				); err != nil {
					return zero, err
				}
			} else if report.Totals.Pending+report.Totals.Active > 0 {
				return zero, ErrRunActive
			}
			if _, err = tx.ExecContext(
				ctx, "UPDATE runs SET status=?,finished_at=?,reason_code=? WHERE id=?",
				in.Status, at.UnixMicro(), optionalString(in.ReasonCode), in.RunID,
			); err != nil {
				return zero, err
			}
			return getValidationReport(ctx, tx, in.RunID)
		},
	)
}
