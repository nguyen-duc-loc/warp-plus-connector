package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// GetValidationReport reads the header and totals in one transaction.
func (r *Reader) GetValidationReport(ctx context.Context, id int64) (state.ValidationReport, error) {
	return transact(
		ctx, r, false, "get validation report", func(tx *sql.Tx) (state.ValidationReport, error) {
			return getValidationReport(ctx, tx, id)
		},
	)
}

func getValidationMetadata(ctx context.Context, tx *sql.Tx, id int64) (state.ValidationReport, error) {
	var out state.ValidationReport
	run, err := getRun(ctx, tx, id)
	if err != nil {
		return out, err
	}
	out.Run = run
	if run.Kind != state.Validation {
		return out, ErrInvalidInput
	}
	var encoded string
	err = tx.QueryRowContext(
		ctx, "SELECT backend_id,backend_version,policy_json FROM validation_batches WHERE run_id=?",
		id,
	).Scan(&out.BackendID, &out.BackendVersion, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var p state.CheckPolicy
	if json.Unmarshal([]byte(encoded), &p) != nil || !p.Valid() {
		return out, ErrStateAccess
	}
	out.Policy = &p
	return out, nil
}

func getValidationReport(ctx context.Context, tx *sql.Tx, id int64) (state.ValidationReport, error) {
	out, err := getValidationMetadata(ctx, tx, id)
	if err != nil || out.Policy == nil {
		return out, err
	}
	rows, err := tx.QueryContext(
		ctx, "SELECT i.status,a.id,a.outcome FROM validation_items i LEFT JOIN validation_attempts a "+
			"ON a.id=(SELECT max(v.id) FROM validation_attempts v WHERE v.run_id=i.run_id AND v.candidate_id=i.candidate_id) "+
			"WHERE i.run_id=?", id,
	)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var status string
		var attempt sql.NullInt64
		var outcome sql.NullString
		if err = rows.Scan(&status, &attempt, &outcome); err != nil {
			break
		}
		out.Totals.Selected++
		switch status {
		case "pending":
			out.Totals.Pending++
		case "active":
			out.Totals.Active++
		case "completed":
			out.Totals.Completed++
		case "skipped":
			out.Totals.Skipped++
		case "interrupted":
			out.Totals.Interrupted++
		}
		switch {
		case !attempt.Valid:
			out.Totals.Unstarted++
		case !outcome.Valid:
			out.Totals.Unresolved++
		case outcome.String == "confirmed":
			out.Totals.Confirmed++
		case outcome.String == "invalid":
			out.Totals.Invalid++
		case outcome.String == "inconclusive":
			out.Totals.Inconclusive++
		}
	}
	return out, errors.Join(err, rows.Err(), rows.Close())
}

func getValidationItem(ctx context.Context, tx *sql.Tx, runID, candidateID int64) (state.ValidationItem, error) {
	var out state.ValidationItem
	var start, finish, recovery, last sql.NullInt64
	var reason sql.NullString
	err := tx.QueryRowContext(
		ctx, "SELECT run_id,candidate_id,ordinal,status,stop_reason,started_at,finished_at,recovered_at,"+
			"(SELECT count(*) FROM validation_attempt_details d WHERE d.run_id=i.run_id AND d.candidate_id=i.candidate_id),"+
			"(SELECT max(attempt_id) FROM validation_attempt_details d WHERE d.run_id=i.run_id "+
			"AND d.candidate_id=i.candidate_id) "+
			"FROM validation_items i WHERE run_id=? AND candidate_id=?", runID, candidateID,
	).Scan(
		&out.RunID, &out.CandidateID,
		&out.Ordinal, &out.Status, &reason, &start, &finish, &recovery, &out.Attempts, &last,
	)
	if err != nil {
		return out, err
	}
	out.StopReason = reason.String
	out.StartedAt = nullTime(start)
	out.FinishedAt = nullTime(finish)
	out.RecoveredAt = nullTime(recovery)
	if last.Valid {
		a, err := getAttempt(ctx, tx, last.Int64)
		if err != nil {
			return out, err
		}
		out.LatestAttempt = &a
	}
	return out, nil
}

// GetValidationItem returns a saved item and its latest attempt.
func (r *Reader) GetValidationItem(ctx context.Context, runID, candidateID int64) (state.ValidationItem, error) {
	return transact(
		ctx, r, false, "get validation item", func(tx *sql.Tx) (state.ValidationItem, error) {
			return getValidationItem(ctx, tx, runID, candidateID)
		},
	)
}

// ListValidationItems pages the complete immutable selection in supplied order.
func (r *Reader) ListValidationItems(
	ctx context.Context,
	runID int64,
	p state.PageRequest,
) (state.Page[state.ValidationItem], error) {
	p = copyPageRequest(p)
	return transact(
		ctx, r, false, "list validation items", func(tx *sql.Tx) (state.Page[state.ValidationItem], error) {
			c := collection{
				name: "validation_items", table: "validation_items", idColumn: "ordinal", where: "run_id=?",
				args: []any{runID}, parent: runID,
			}
			return page(
				ctx, tx, p, c, func(ordinal int64) (state.ValidationItem, error) {
					var id int64
					if err := tx.QueryRowContext(
						ctx, "SELECT candidate_id FROM validation_items WHERE run_id=? AND ordinal=?",
						runID, ordinal,
					).Scan(&id); err != nil {
						return state.ValidationItem{}, err
					}
					return getValidationItem(ctx, tx, runID, id)
				},
			)
		},
	)
}

func getBatchAttempt(ctx context.Context, tx *sql.Tx, id int64) (state.BatchAttempt, error) {
	var out state.BatchAttempt
	a, err := getAttempt(ctx, tx, id)
	if err != nil {
		return out, err
	}
	out.Attempt = a
	var class sql.NullString
	var wait, due sql.NullInt64
	err = tx.QueryRowContext(
		ctx, "SELECT attempt_number,failure_class,retry_wait_us,retry_eligible_at "+
			"FROM validation_attempt_details WHERE attempt_id=?", id,
	).Scan(&out.Number, &class, &wait, &due)
	out.FailureClass = state.FailureClass(class.String)
	out.RetryWait = time.Duration(wait.Int64) * time.Microsecond
	out.RetryEligibleAt = nullTime(due)
	return out, err
}

// ListBatchAttempts pages the attempt history of a selected item in attempt order.
func (r *Reader) ListBatchAttempts(
	ctx context.Context,
	runID, candidateID int64,
	p state.PageRequest,
) (state.Page[state.BatchAttempt], error) {
	p = copyPageRequest(p)
	return transact(
		ctx, r, false, "list batch attempts", func(tx *sql.Tx) (state.Page[state.BatchAttempt], error) {
			c := collection{
				name: "batch_attempts", table: "validation_attempt_details", idColumn: "attempt_number",
				where: "run_id=? AND candidate_id=?", args: []any{runID, candidateID}, parent: runID,
				filter: strconv.FormatInt(candidateID, 10),
			}
			return page(
				ctx, tx, p, c, func(number int64) (state.BatchAttempt, error) {
					var id int64
					if err := tx.QueryRowContext(
						ctx, "SELECT attempt_id FROM validation_attempt_details "+
							"WHERE run_id=? AND candidate_id=? AND attempt_number=?", runID, candidateID, number,
					).Scan(&id); err != nil {
						return state.BatchAttempt{}, err
					}
					return getBatchAttempt(ctx, tx, id)
				},
			)
		},
	)
}
