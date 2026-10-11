package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

type scanner interface{ Scan(...any) error }

const runColumns = "id,request_id,kind,status,started_at,finished_at,recovered_at,reason_code,origin"
const attemptColumns = "id,request_id,run_id,candidate_id,backend_id,backend_version," +
	"started_at,finished_at,recovered_at,outcome,reason_code," +
	"evidence_json"

func nullTime(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := time.UnixMicro(n.Int64).UTC()
	return &t
}
func nullString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}
func before(t *time.Time, start time.Time) bool { return t != nil && t.Before(start) }

func scanRun(row scanner) (state.Run, error) {
	var r state.Run
	var start int64
	var finish, recovered sql.NullInt64
	var reason sql.NullString
	err := row.Scan(&r.ID, &r.RequestID, &r.Kind, &r.Status, &start, &finish, &recovered, &reason, &r.Origin)
	r.StartedAt = time.UnixMicro(start).UTC()
	r.FinishedAt, r.RecoveredAt, r.ReasonCode = nullTime(finish), nullTime(recovered), nullString(reason)
	r.ClockAnomaly = before(r.FinishedAt, r.StartedAt) || before(r.RecoveredAt, r.StartedAt)
	return r, err
}

func getRun(ctx context.Context, tx *sql.Tx, id int64) (state.Run, error) {
	return scanRun(tx.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE id=?", id))
}

func scanAttempt(row scanner) (state.Attempt, error) {
	var a state.Attempt
	var start int64
	var finish, recovered sql.NullInt64
	var outcome, reason, evidence sql.NullString
	err := row.Scan(
		&a.ID,
		&a.RequestID,
		&a.RunID,
		&a.CandidateID,
		&a.BackendID,
		&a.BackendVersion,
		&start,
		&finish,
		&recovered,
		&outcome,
		&reason,
		&evidence,
	)
	if err != nil {
		return a, err
	}
	a.StartedAt = time.UnixMicro(start).UTC()
	a.FinishedAt, a.RecoveredAt, a.ReasonCode = nullTime(finish), nullTime(recovered), nullString(reason)
	a.Outcome = state.Outcome(outcome.String)
	a.ClockAnomaly = before(a.FinishedAt, a.StartedAt) || before(a.RecoveredAt, a.StartedAt)
	if evidence.Valid {
		a.Evidence, err = decodeEvidence(evidence.String)
		if err != nil {
			return a, err
		}
		at := a.Evidence.ObservedAt
		a.ClockAnomaly = a.ClockAnomaly || at.Before(a.StartedAt) || (a.FinishedAt != nil && at.After(*a.FinishedAt))
	}
	return a, nil
}

func getAttempt(ctx context.Context, tx *sql.Tx, id int64) (state.Attempt, error) {
	return scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM validation_attempts WHERE id=?", id))
}

func requireCandidate(ctx context.Context, tx *sql.Tx, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	var found int64
	return tx.QueryRowContext(ctx, "SELECT id FROM candidates WHERE id=?", id).Scan(&found)
}

func normalizedOrigin(origin string) string {
	if origin == "" {
		return "manual"
	}
	return origin
}
