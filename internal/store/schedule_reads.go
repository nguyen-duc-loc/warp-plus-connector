package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// GetSchedule establishes a snapshot before sampling the application's clock.
// A positive candidate ID selects the private candidate explanation.
func (r *Reader) GetSchedule(ctx context.Context, id int64, clock func() time.Time) (state.ScheduleSummary, error) {
	return transactOwned(
		ctx, r, false, "read schedule", true, func(tx *sql.Tx) (state.ScheduleSummary, error) {
			return scheduleSummary(ctx, tx, id, clock)
		},
	)
}

func optionalID(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func scheduleSummary(ctx context.Context, tx *sql.Tx, id int64, clock func() time.Time) (state.ScheduleSummary, error) {
	var out state.ScheduleSummary
	if id < 0 || clock == nil {
		return out, ErrInvalidInput
	}
	var checkpointAt int64
	var coverage, reason, waitReason sql.NullString
	var due, basis, interval, run, waitBasis, retry, waitInterval, observed sql.NullInt64
	err := tx.QueryRowContext(
		ctx, `SELECT last_observed_at,crawl_coverage_json,crawl_due_at,crawl_reason,
   crawl_basis_at,crawl_interval_ns,crawl_basis_run_id,validation_wait_reason,validation_wait_basis_at,
   validation_retry_at,validation_wait_interval_ns,validation_wait_observed_at,validation_recovery_pending
   FROM scheduler_state WHERE id=1`,
	).Scan(
		&checkpointAt, &coverage, &due, &reason, &basis, &interval, &run,
		&waitReason, &waitBasis, &retry, &waitInterval, &observed, &out.ValidationRecoveryPending,
	)
	if err != nil {
		return out, err
	}
	out.AsOf, err = normalizeTime(clock())
	if err != nil {
		return out, err
	}
	out.CheckpointAt = time.UnixMicro(checkpointAt).UTC()
	out.ClockRegressed = out.AsOf.Before(out.CheckpointAt)
	if id > 0 {
		c := &state.CandidateSchedule{CandidateID: id}
		var d, b, ns int64
		var rid, aid, active sql.NullInt64
		err = tx.QueryRowContext(
			ctx, `SELECT due_at,reason,basis_at,interval_ns,basis_run_id,basis_attempt_id,
    (SELECT i.run_id FROM validation_items i JOIN runs r ON r.id=i.run_id WHERE i.candidate_id=s.candidate_id
     AND i.status IN ('pending','active') AND r.status='running' LIMIT 1)
    FROM candidate_schedule s WHERE candidate_id=?`, id,
		).Scan(&d, &c.Reason, &b, &ns, &rid, &aid, &active)
		if err != nil {
			return out, err
		}
		c.DueAt, c.BasisAt = time.UnixMicro(d).UTC(), time.UnixMicro(b).UTC()
		c.Interval = time.Duration(ns)
		c.BasisRunID = optionalID(rid)
		c.BasisAttemptID = optionalID(aid)
		c.ActiveRunID = optionalID(active)
		c.EligibleByTime = d <= out.AsOf.UnixMicro() && !active.Valid
		out.Candidate = c
		return out, nil
	}
	if coverage.Valid {
		c := &state.SchedulePlan{
			DueAt: *nullTime(due), Reason: reason.String, BasisAt: *nullTime(basis),
			Interval: time.Duration(interval.Int64), BasisRunID: optionalID(run),
		}
		if json.Unmarshal([]byte(coverage.String), &c.Coverage) != nil {
			return out, ErrStateAccess
		}
		var active sql.NullInt64
		if err := tx.QueryRowContext(
			ctx,
			"SELECT max(id) FROM runs WHERE kind='crawl' AND status='running'",
		).Scan(&active); err != nil {
			return out, err
		}
		c.ActiveRunID = optionalID(active)
		out.Crawl = c
	}
	if waitReason.Valid {
		out.ValidationWait = &state.ValidationWait{
			Reason:  waitReason.String,
			BasisAt: *nullTime(waitBasis), RetryAt: *nullTime(retry), ObservedAt: *nullTime(observed),
			Interval: time.Duration(waitInterval.Int64),
		}
	}
	var next sql.NullInt64
	err = tx.QueryRowContext(
		ctx, `SELECT count(*),coalesce(sum(active),0),
   coalesce(sum(CASE WHEN NOT active AND due_at<=? THEN 1 ELSE 0 END),0),
   min(CASE WHEN NOT active THEN due_at END) FROM (SELECT due_at,`+activeCandidate+
			` AS active FROM candidate_schedule s)`, out.AsOf.UnixMicro(),
	).
		Scan(&out.Candidates.Total, &out.Candidates.Active, &out.Candidates.Due, &next)
	out.Candidates.Waiting = out.Candidates.Total - out.Candidates.Active - out.Candidates.Due
	out.Candidates.NextDueAt = nullTime(next)
	return out, err
}
