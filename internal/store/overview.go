package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// GetOverview reads complete aggregates and bounded recent history in one snapshot.
func (r *Reader) GetOverview(
	ctx context.Context, clock func() time.Time, maxAge time.Duration,
) (state.OverviewCollection, error) {
	if clock == nil || maxAge <= 0 {
		return state.OverviewCollection{}, ErrInvalidInput
	}
	return transactOwned(
		ctx, r, false, "read overview", true, func(tx *sql.Tx) (state.OverviewCollection, error) {
			out := state.OverviewCollection{MaxAge: maxAge, RecentRuns: []state.OperationalRun{}}
			var err error
			out.Schedule, err = scheduleSummary(ctx, tx, 0, clock)
			if err != nil {
				return out, err
			}
			out.AsOf = out.Schedule.AsOf
			out.ClockAnomaly = out.Schedule.ClockRegressed
			if err = overviewCounts(ctx, tx, &out); err != nil {
				return out, err
			}
			rows, err := tx.QueryContext(ctx, "SELECT id FROM runs ORDER BY id DESC LIMIT 11")
			if err != nil {
				return out, err
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
				return out, err
			}
			out.OlderRunsAvailable = len(ids) > 10
			if len(ids) > 10 {
				ids = ids[:10]
			}
			for _, id := range ids {
				run, err := operationalRun(ctx, tx, id)
				if err != nil {
					return out, err
				}
				out.RecentRuns = append(out.RecentRuns, run)
				out.ClockAnomaly = out.ClockAnomaly || run.Run.ClockAnomaly
			}
			return out, ctx.Err()
		},
	)
}

func overviewCounts(ctx context.Context, tx *sql.Tx, out *state.OverviewCollection) error {
	c := &out.Outcomes
	err := tx.QueryRowContext(
		ctx, `SELECT count(*),
 coalesce(sum(a.outcome IS NULL),0),coalesce(sum(a.outcome='confirmed'),0),
 coalesce(sum(a.outcome='invalid'),0),coalesce(sum(a.outcome='inconclusive'),0)
 FROM candidates c LEFT JOIN validation_attempts a ON a.id=(SELECT max(id) FROM validation_attempts
 WHERE candidate_id=c.id AND outcome IS NOT NULL)`,
	).Scan(
		&c.Total, &c.Unchecked, &c.Confirmed, &c.Invalid, &c.Inconclusive,
	)
	if err != nil {
		return err
	}
	if c.Total != c.Unchecked+c.Confirmed+c.Invalid+c.Inconclusive {
		return ErrStateAccess
	}
	rows, err := tx.QueryContext(
		ctx, `SELECT a.evidence_json FROM candidates c
 JOIN validation_attempts a ON a.id=(SELECT max(id) FROM validation_attempts
 WHERE candidate_id=c.id AND outcome IN ('confirmed','invalid')) WHERE a.outcome='confirmed'`,
	)
	if err != nil {
		return err
	}
	for rows.Next() {
		var raw sql.NullString
		if err = rows.Scan(&raw); err != nil {
			break
		}
		if !raw.Valid {
			err = ErrStateAccess
			break
		}
		var evidence *state.Evidence
		evidence, err = decodeEvidence(raw.String)
		if err != nil {
			break
		}
		if _, err = encodeEvidence(state.Confirmed, "", evidence); err != nil {
			err = ErrStateAccess
			break
		}
		switch {
		case evidence.ObservedAt.After(out.AsOf):
			out.UnknownFreshness++
			out.ClockAnomaly = true
		case out.AsOf.Sub(evidence.ObservedAt) >= out.MaxAge:
			out.StaleConfirmed++
		default:
			out.FreshConfirmed++
		}
	}
	return errors.Join(err, rows.Err(), rows.Close())
}
