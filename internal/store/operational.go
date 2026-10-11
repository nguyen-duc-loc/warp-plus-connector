package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// GetOperationalWork reads every service work fact from one established snapshot.
func (r *Reader) GetOperationalWork(ctx context.Context, clock func() time.Time) (state.OperationalWork, error) {
	return transactOwned(
		ctx, r, false, "read operational work", true, func(tx *sql.Tx) (state.OperationalWork, error) {
			var out state.OperationalWork
			var err error
			out.Schedule, err = scheduleSummary(ctx, tx, 0, clock)
			if err != nil {
				return out, err
			}
			out.AsOf = out.Schedule.AsOf
			c := &out.Outcomes
			err = tx.QueryRowContext(
				ctx, `SELECT count(*),
 coalesce(sum(a.outcome IS NULL),0), coalesce(sum(a.outcome='confirmed'),0),
 coalesce(sum(a.outcome='invalid'),0), coalesce(sum(a.outcome='inconclusive'),0),
 coalesce(sum(d.outcome='confirmed'),0), coalesce(sum(d.outcome='invalid'),0),
 coalesce(sum(d.outcome IS NULL),0)
 FROM candidates c LEFT JOIN validation_attempts a ON a.id=(SELECT max(id) FROM validation_attempts
 WHERE candidate_id=c.id AND outcome IS NOT NULL)
 LEFT JOIN validation_attempts d ON d.id=(SELECT max(id) FROM validation_attempts
 WHERE candidate_id=c.id AND outcome IN ('confirmed','invalid'))`,
			).Scan(
				&c.Total, &c.Unchecked, &c.Confirmed, &c.Invalid, &c.Inconclusive,
				&c.DefinitiveConfirmed, &c.DefinitiveInvalid, &c.DefinitiveNone,
			)
			if err != nil {
				return out, err
			}
			for _, selection := range []struct {
				kind        string
				terminal    bool
				destination **state.OperationalRun
			}{
				{"crawl", false, &out.LatestCrawl}, {"crawl", true, &out.LastTerminalCrawl},
				{"validation", false, &out.LatestValidation}, {"validation", true, &out.LastTerminalValidation},
			} {
				var id sql.NullInt64
				query := "SELECT max(id) FROM runs WHERE kind=?"
				if selection.terminal {
					query += " AND status<>'running'"
				}
				if err := tx.QueryRowContext(ctx, query, selection.kind).Scan(&id); err != nil {
					return out, err
				}
				if !id.Valid {
					continue
				}
				run, err := operationalRun(ctx, tx, id.Int64)
				if err != nil {
					return out, err
				}
				*selection.destination = &run
			}
			return out, nil
		},
	)
}

func operationalRun(ctx context.Context, tx *sql.Tx, id int64) (state.OperationalRun, error) {
	var out state.OperationalRun
	run, err := getRun(ctx, tx, id)
	if err != nil {
		return out, err
	}
	out.Run = run
	if run.Kind == state.Crawl {
		err := tx.QueryRowContext(
			ctx, `SELECT count(DISTINCT s.candidate_id),count(*)
 FROM crawl_observations o JOIN sightings s ON s.id=o.sighting_id WHERE o.run_id=?`, id,
		).
			Scan(&out.SavedCandidates, &out.SavedSightings)
		if err != nil {
			return out, err
		}
		rows, err := tx.QueryContext(
			ctx, `SELECT c.channel,c.status,c.stop_reason,
 (SELECT count(DISTINCT s.candidate_id) FROM crawl_page_observations o JOIN sightings s ON s.id=o.sighting_id
 WHERE o.run_id=c.run_id AND o.source_id=c.source_id),
 (SELECT count(*) FROM crawl_page_observations o WHERE o.run_id=c.run_id AND o.source_id=c.source_id)
 FROM crawl_sources c WHERE c.run_id=? ORDER BY position`, id,
		)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var s state.OperationalSource
			var reason sql.NullString
			if err = rows.Scan(&s.Channel, &s.Status, &reason, &s.SavedCandidates, &s.SavedSightings); err != nil {
				break
			}
			s.ReasonCode = nullString(reason)
			out.Sources = append(out.Sources, s)
			if len(out.Sources) > 32 {
				err = ErrStateAccess
				break
			}
		}
		return out, errors.Join(err, rows.Err(), rows.Close())
	}
	report, err := getValidationReport(ctx, tx, id)
	if err != nil || report.Policy == nil {
		return out, err
	}
	out.Totals = &report.Totals
	out.Failures = []state.FailureCount{}
	rows, err := tx.QueryContext(
		ctx, `SELECT reason,count(*) FROM (
 SELECT coalesce(i.stop_reason,a.reason_code) AS reason FROM validation_items i
 LEFT JOIN validation_attempts a ON a.id=(SELECT max(v.id) FROM validation_attempts v
 WHERE v.run_id=i.run_id AND v.candidate_id=i.candidate_id AND v.outcome IS NOT NULL)
 WHERE i.run_id=? AND i.status NOT IN ('pending','active'))
 WHERE reason IS NOT NULL GROUP BY reason ORDER BY reason`, id,
	)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var f state.FailureCount
		if err = rows.Scan(&f.Code, &f.Count); err != nil {
			break
		}
		out.Failures = append(out.Failures, f)
	}
	return out, errors.Join(err, rows.Err(), rows.Close())
}
