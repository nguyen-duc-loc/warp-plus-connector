package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

func checkpoint(ctx context.Context, tx *sql.Tx, at time.Time) error {
	_, err := tx.ExecContext(
		ctx, "UPDATE scheduler_state SET last_observed_at=max(last_observed_at,?) WHERE id=1",
		at.UnixMicro(),
	)
	return err
}

func scheduleInterval(policy state.SchedulePolicy, reason string) time.Duration {
	if reason == "routine" {
		return policy.Routine
	}
	if reason == "failure" {
		return policy.Failure
	}
	return 0
}

func initializeSchedules(ctx context.Context, tx *sql.Tx, at time.Time, p state.SchedulePolicy, initial bool) error {
	if initial {
		if _, err := tx.ExecContext(
			ctx, "INSERT INTO scheduler_state(id,last_observed_at) VALUES(1,?)",
			at.UnixMicro(),
		); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM candidates ORDER BY id")
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, id := range ids {
		if initial {
			var first sql.NullInt64
			if err := tx.QueryRowContext(
				ctx, `SELECT min(o.observed_at) FROM sightings s
    JOIN crawl_observations o ON o.sighting_id=s.id WHERE s.candidate_id=?`, id,
			).Scan(&first); err != nil {
				return err
			}
			if !first.Valid {
				return ErrStateAccess
			}
			if err := firstSchedule(ctx, tx, id, time.UnixMicro(first.Int64).UTC()); err != nil {
				return err
			}
		}
		// Only initial backfill or an item actually recovered on this open may move a basis.
		var recovered int
		if err := tx.QueryRowContext(
			ctx, `SELECT count(*) FROM validation_attempts a
   LEFT JOIN validation_items i ON i.run_id=a.run_id AND i.candidate_id=a.candidate_id
   WHERE a.id=(SELECT max(id) FROM validation_attempts WHERE candidate_id=?)
   AND (a.recovered_at=? OR i.recovered_at=?)`, id, at.UnixMicro(), at.UnixMicro(),
		).Scan(&recovered); err != nil {
			return err
		}
		if initial || recovered != 0 {
			if err := advanceCandidate(ctx, tx, id, p); err != nil {
				return err
			}
		}
	}
	// Interrupted matching crawls are advanced in the same recovery transaction.
	var runID sql.NullInt64
	if err := tx.QueryRowContext(
		ctx, "SELECT max(id) FROM runs WHERE kind='crawl' AND recovered_at=?",
		at.UnixMicro(),
	).Scan(&runID); err != nil {
		return err
	}
	if runID.Valid {
		if err := advanceCrawl(ctx, tx, runID.Int64, p); err != nil {
			return err
		}
	}
	return checkpoint(ctx, tx, at)
}

func firstSchedule(ctx context.Context, tx *sql.Tx, id int64, at time.Time) error {
	_, err := tx.ExecContext(
		ctx, `INSERT INTO candidate_schedule(candidate_id,due_at,reason,basis_at,interval_ns)
  VALUES(?,?,'first_check',?,0) ON CONFLICT(candidate_id) DO NOTHING`, id, at.UnixMicro(), at.UnixMicro(),
	)
	if err != nil {
		return err
	}
	return checkpoint(ctx, tx, at)
}

func advanceCandidate(ctx context.Context, tx *sql.Tx, id int64, p state.SchedulePolicy) error {
	var aid, rid int64
	var outcome string
	var basis sql.NullInt64
	err := tx.QueryRowContext(
		ctx, `SELECT a.id,a.run_id,a.outcome,
  CASE WHEN i.run_id IS NULL THEN coalesce(a.finished_at,a.recovered_at)
   ELSE coalesce(i.finished_at,i.recovered_at) END
  FROM validation_attempts a LEFT JOIN validation_items i
   ON i.run_id=a.run_id AND i.candidate_id=a.candidate_id
  WHERE a.candidate_id=? ORDER BY a.id DESC LIMIT 1`, id,
	).Scan(&aid, &rid, &outcome, &basis)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !basis.Valid {
		return nil
	}
	reason := "failure"
	if outcome == "confirmed" || outcome == "invalid" {
		reason = "routine"
	}
	interval := scheduleInterval(p, reason)
	at := time.UnixMicro(basis.Int64).UTC()
	due, err := state.ScheduleDue(at, interval)
	if err != nil {
		return ErrInvalidInput
	}
	_, err = tx.ExecContext(
		ctx, `UPDATE candidate_schedule SET due_at=?,reason=?,basis_at=?,interval_ns=?,
  basis_run_id=?,basis_attempt_id=? WHERE candidate_id=? AND (basis_attempt_id IS NULL OR basis_attempt_id<=?)`,
		due.UnixMicro(), reason, basis.Int64, int64(interval), rid, aid, id, aid,
	)
	if err != nil {
		return err
	}
	return checkpoint(ctx, tx, at)
}

func savedCoverage(ctx context.Context, tx *sql.Tx, id int64) ([]state.SourcePlan, error) {
	report, err := crawlReport(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	var plan []state.SourcePlan
	for _, s := range report.Sources {
		plan = append(plan, state.SourcePlan{Channel: s.Channel, PageLimit: s.PageLimit})
	}
	if len(plan) == 0 && report.Coverage != nil && report.Coverage.PageLimit == 1 {
		channel, ok := state.NormalizeChannel(strings.TrimPrefix(report.Coverage.SourceID, "telegram:"))
		if ok && report.Coverage.SourceID == "telegram:"+channel {
			plan = []state.SourcePlan{{Channel: channel, PageLimit: 1}}
		}
	}
	return plan, nil
}

func advanceCrawl(ctx context.Context, tx *sql.Tx, id int64, policy state.SchedulePolicy) error {
	var raw sql.NullString
	if err := tx.QueryRowContext(
		ctx,
		"SELECT crawl_coverage_json FROM scheduler_state WHERE id=1",
	).Scan(&raw); err != nil {
		return err
	}
	if !raw.Valid {
		return nil
	}
	var expected []state.SourcePlan
	if json.Unmarshal([]byte(raw.String), &expected) != nil {
		return ErrStateAccess
	}
	plan, err := savedCoverage(ctx, tx, id)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(plan, expected) {
		return nil
	}
	run, err := getRun(ctx, tx, id)
	if err != nil {
		return err
	}
	at := run.FinishedAt
	if at == nil {
		at = run.RecoveredAt
	}
	if at == nil {
		return nil
	}
	due, err := state.ScheduleDue(*at, policy.Crawl)
	if err != nil {
		return ErrInvalidInput
	}
	_, err = tx.ExecContext(
		ctx, `UPDATE scheduler_state SET crawl_due_at=?,crawl_reason=?,crawl_basis_at=?,
  crawl_interval_ns=?,crawl_basis_run_id=? WHERE id=1`, due.UnixMicro(), string(run.Status), at.UnixMicro(),
		int64(policy.Crawl), id,
	)
	if err != nil {
		return err
	}
	return checkpoint(ctx, tx, *at)
}

// PrepareSchedule reconciles policy and enabled coverage without rewriting evidence.
func (w *Writer) PrepareSchedule(
	ctx context.Context, at time.Time, p state.SchedulePolicy,
	coverage []state.SourcePlan, validation bool,
) (bool, error) {
	coverage = append([]state.SourcePlan(nil), coverage...)
	return transact(
		ctx, &w.Reader, true, "prepare schedule", func(tx *sql.Tx) (bool, error) {
			at, err := normalizeTime(at)
			if err != nil || !p.Valid() {
				return false, ErrInvalidInput
			}
			var previous int64
			if err := tx.QueryRowContext(
				ctx,
				"SELECT last_observed_at FROM scheduler_state WHERE id=1",
			).Scan(&previous); err != nil {
				return false, err
			}
			if validation {
				if _, err := tx.ExecContext(
					ctx,
					"UPDATE scheduler_state SET validation_recovery_pending=1 WHERE id=1",
				); err != nil {
					return false, err
				}
			}
			if at.UnixMicro() < previous {
				return false, nil
			}
			rows, err := tx.QueryContext(ctx, "SELECT candidate_id,reason,basis_at FROM candidate_schedule")
			if err != nil {
				return false, err
			}
			type record struct {
				id, at int64
				reason string
			}
			var records []record
			for rows.Next() {
				var r record
				if err = rows.Scan(&r.id, &r.reason, &r.at); err != nil {
					break
				}
				records = append(records, r)
			}
			if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
				return false, err
			}
			for _, r := range records {
				interval := scheduleInterval(p, r.reason)
				due, e := state.ScheduleDue(time.UnixMicro(r.at), interval)
				if e != nil {
					return false, ErrInvalidInput
				}
				if _, err = tx.ExecContext(
					ctx, "UPDATE candidate_schedule SET due_at=?,interval_ns=? WHERE candidate_id=?",
					due.UnixMicro(), int64(interval), r.id,
				); err != nil {
					return false, err
				}
			}
			if len(coverage) > 0 {
				if err := prepareCoverage(ctx, tx, at, p, coverage); err != nil {
					return false, err
				}
			}
			var basis sql.NullInt64
			var reason sql.NullString
			if err := tx.QueryRowContext(ctx, "SELECT crawl_basis_at,crawl_reason FROM scheduler_state WHERE id=1").
				Scan(&basis, &reason); err != nil {
				return false, err
			}
			if basis.Valid && reason.String != "initial" && reason.String != "coverage_changed" {
				due, e := state.ScheduleDue(time.UnixMicro(basis.Int64), p.Crawl)
				if e != nil {
					return false, ErrInvalidInput
				}
				if _, err := tx.ExecContext(
					ctx, "UPDATE scheduler_state SET crawl_due_at=?,crawl_interval_ns=? WHERE id=1",
					due.UnixMicro(), int64(p.Crawl),
				); err != nil {
					return false, err
				}
			}
			if err := tx.QueryRowContext(
				ctx,
				"SELECT validation_wait_basis_at,validation_wait_reason FROM scheduler_state WHERE id=1",
			).
				Scan(&basis, &reason); err != nil {
				return false, err
			}
			if basis.Valid && (reason.String == "backend_unavailable" || reason.String == "cleanup_pending") {
				due, e := state.ScheduleDue(time.UnixMicro(basis.Int64), p.Failure)
				if e != nil {
					return false, ErrInvalidInput
				}
				if _, err := tx.ExecContext(
					ctx, "UPDATE scheduler_state SET validation_retry_at=?,validation_wait_interval_ns=? WHERE id=1",
					due.UnixMicro(), int64(p.Failure),
				); err != nil {
					return false, err
				}
			}
			return true, checkpoint(ctx, tx, at)
		},
	)
}

func prepareCoverage(
	ctx context.Context, tx *sql.Tx, at time.Time, p state.SchedulePolicy,
	plan []state.SourcePlan,
) error {
	seen := map[string]bool{}
	for _, s := range plan {
		ch, ok := state.NormalizeChannel(s.Channel)
		if !ok || ch != s.Channel || seen[ch] || s.PageLimit < 1 || s.PageLimit > 100 {
			return ErrInvalidInput
		}
		seen[ch] = true
	}
	if len(plan) > 32 {
		return ErrInvalidInput
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return ErrInvalidInput
	}
	var raw sql.NullString
	if err = tx.QueryRowContext(
		ctx,
		"SELECT crawl_coverage_json FROM scheduler_state WHERE id=1",
	).Scan(&raw); err != nil {
		return err
	}
	if raw.Valid && raw.String == string(encoded) {
		return nil
	}
	reason := "initial"
	if raw.Valid {
		reason = "coverage_changed"
	}
	_, err = tx.ExecContext(
		ctx, `UPDATE scheduler_state SET crawl_coverage_json=?,crawl_due_at=?,crawl_reason=?,
  crawl_basis_at=?,crawl_interval_ns=0,crawl_basis_run_id=NULL WHERE id=1`, string(encoded), at.UnixMicro(), reason,
		at.UnixMicro(),
	)
	if err != nil || raw.Valid {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM runs WHERE kind='crawl' AND status!='running' ORDER BY id DESC")
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, id := range ids {
		saved, err := savedCoverage(ctx, tx, id)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(saved, plan) {
			return advanceCrawl(ctx, tx, id, p)
		}
	}
	return nil
}

// ObserveSchedule saves a maximum checkpoint and reports whether admission is safe.
func (w *Writer) ObserveSchedule(ctx context.Context, at time.Time) (bool, error) {
	return transact(
		ctx, &w.Reader, true, "observe schedule", func(tx *sql.Tx) (bool, error) {
			at, err := normalizeTime(at)
			if err != nil {
				return false, err
			}
			var saved int64
			if err = tx.QueryRowContext(
				ctx,
				"SELECT last_observed_at FROM scheduler_state WHERE id=1",
			).Scan(&saved); err != nil {
				return false, err
			}
			if at.UnixMicro() < saved {
				return false, nil
			}
			return true, checkpoint(ctx, tx, at)
		},
	)
}

// SaveValidationWait records admission facts and cleanup obligations atomically.
func (w *Writer) SaveValidationWait(ctx context.Context, at time.Time, wait *state.ValidationWait, pending bool) error {
	if wait != nil {
		copy := *wait
		wait = &copy
	}
	_, err := transact(
		ctx, &w.Reader, true, "save validation wait", func(tx *sql.Tx) (bool, error) {
			at, err := normalizeTime(at)
			if err != nil {
				return false, err
			}
			var reason, basis, retry, interval, observed any
			if wait != nil {
				if !reasonPattern.MatchString(wait.Reason) || wait.Interval <= 0 {
					return false, ErrInvalidInput
				}
				for _, t := range []time.Time{wait.BasisAt, wait.RetryAt, wait.ObservedAt} {
					if _, err := normalizeTime(t); err != nil {
						return false, err
					}
				}
				reason, basis, retry, interval, observed = wait.Reason, wait.BasisAt.UnixMicro(), wait.RetryAt.UnixMicro(),
					int64(wait.Interval), wait.ObservedAt.UnixMicro()
			}
			_, err = tx.ExecContext(
				ctx, `UPDATE scheduler_state SET validation_wait_reason=?,validation_wait_basis_at=?,
   validation_retry_at=?,validation_wait_interval_ns=?,validation_wait_observed_at=?,validation_recovery_pending=?
   WHERE id=1`, reason, basis, retry, interval, observed, pending,
			)
			if err != nil {
				return false, err
			}
			return true, checkpoint(ctx, tx, at)
		},
	)
	return err
}

const activeCandidate = `EXISTS(SELECT 1 FROM validation_items i JOIN runs r ON r.id=i.run_id
 WHERE i.candidate_id=s.candidate_id AND i.status IN ('pending','active') AND r.status='running')`

// DueCandidates selects a bounded immutable batch from the current saved eligibility.
func (w *Writer) DueCandidates(ctx context.Context, at time.Time, limit int) ([]int64, error) {
	return transact(
		ctx, &w.Reader, false, "select due candidates", func(tx *sql.Tx) ([]int64, error) {
			if limit < 1 || limit > 8 {
				return nil, ErrInvalidInput
			}
			rows, err := tx.QueryContext(
				ctx, `SELECT s.candidate_id FROM candidate_schedule s WHERE due_at<=? AND NOT `+
					activeCandidate+` AND NOT EXISTS(SELECT 1 FROM validation_attempts a WHERE a.candidate_id=s.candidate_id
   AND a.outcome IS NULL) ORDER BY due_at,s.candidate_id LIMIT ?`, at.UnixMicro(), limit,
			)
			if err != nil {
				return nil, err
			}
			var ids []int64
			for rows.Next() {
				var id int64
				if err = rows.Scan(&id); err != nil {
					break
				}
				ids = append(ids, id)
			}
			return ids, errors.Join(err, rows.Err(), rows.Close())
		},
	)
}

func completionPolicy(p state.SchedulePolicy) state.SchedulePolicy {
	if p == (state.SchedulePolicy{}) {
		return state.DefaultSchedulePolicy()
	}
	return p
}

// saveCompletionPolicy rejects policy edits to an already acknowledged execution.
func saveCompletionPolicy(
	ctx context.Context, tx *sql.Tx, table string, id int64, candidate int64,
	p state.SchedulePolicy,
) error {
	p = completionPolicy(p)
	if !p.Valid() {
		return ErrInvalidInput
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		return ErrInvalidInput
	}
	where := "id=?"
	args := []any{id}
	if table == "validation_items" {
		where = "run_id=? AND candidate_id=?"
		args = append(args, candidate)
	}
	var saved sql.NullString
	if err := tx.QueryRowContext(
		ctx,
		"SELECT schedule_completion_json FROM "+table+" WHERE "+where,
		args...,
	).Scan(&saved); err != nil {
		return err
	}
	if saved.Valid {
		if saved.String != string(encoded) {
			return ErrConflict
		}
		return nil
	}
	_, err = tx.ExecContext(
		ctx, "UPDATE "+table+" SET schedule_completion_json=? WHERE "+where,
		append([]any{string(encoded)}, args...)...,
	)
	return err
}
