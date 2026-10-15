package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// ReportHeader contains facts from one established saved run snapshot.
type ReportHeader struct {
	AsOf  time.Time
	Run   state.OperationalRun
	Crawl *state.CrawlReport
	Check *state.ValidationReport
}

// VisitReport streams a complete run in bounded batches within one read transaction.
// Callbacks are synchronous; the live reader can close before activating the result.
func (r *Reader) VisitReport(
	ctx context.Context, id int64, identity CollectionIdentity,
	path string, clock func() time.Time, header func(ReportHeader) error,
	emit func(section, parent string, value any) error,
) error {
	var callbackErr error
	_, err := transactOwned(
		ctx, r, false, "report snapshot", true, func(tx *sql.Tx) (struct{}, error) {
			run, err := operationalRun(ctx, tx, id)
			if err != nil {
				return struct{}{}, err
			}
			if !identity.Same(path) {
				return struct{}{}, ErrCollectionChanged
			}
			h := ReportHeader{Run: run, AsOf: clock().UTC().Truncate(time.Microsecond)}
			if run.Run.Kind == state.Crawl {
				c, e := crawlReport(ctx, tx, id)
				if e != nil {
					return struct{}{}, e
				}
				h.Crawl = &c
				if callbackErr = header(h); callbackErr != nil {
					return struct{}{}, nil
				}
				for _, source := range c.Sources {
					pages := source.Pages
					if callbackErr = emit("sources", "", source); callbackErr != nil {
						return struct{}{}, nil
					}
					for _, p := range pages {
						if callbackErr = emit("pages", source.SourceID, p); callbackErr != nil {
							return struct{}{}, nil
						}
					}
				}
				return struct{}{}, ctx.Err()
			}
			c, err := getValidationReport(ctx, tx, id)
			if err != nil {
				return struct{}{}, err
			}
			h.Check = &c
			if callbackErr = header(h); callbackErr != nil {
				return struct{}{}, nil
			}
			if c.Policy != nil {
				var ordinal int64
				for {
					rows, e := tx.QueryContext(
						ctx,
						"SELECT candidate_id,ordinal FROM validation_items WHERE run_id=? AND ordinal>=? ORDER BY ordinal LIMIT 50",
						id,
						ordinal,
					)
					if e != nil {
						return struct{}{}, e
					}
					ids := [][2]int64{}
					for rows.Next() {
						var pair [2]int64
						if e = rows.Scan(&pair[0], &pair[1]); e != nil {
							break
						}
						ids = append(ids, pair)
					}
					if e = errors.Join(e, rows.Err(), rows.Close()); e != nil {
						return struct{}{}, e
					}
					if len(ids) == 0 {
						break
					}
					for _, pair := range ids {
						item, e := getValidationItem(ctx, tx, id, pair[0])
						if e != nil {
							return struct{}{}, e
						}
						if callbackErr = emit("items", "", item); callbackErr != nil {
							return struct{}{}, nil
						}
						ordinal = pair[1] + 1
					}
				}
			}
			var after int64
			for {
				rows, e := tx.QueryContext(
					ctx,
					"SELECT id FROM validation_attempts WHERE run_id=? AND id>? ORDER BY id LIMIT 50", id, after,
				)
				if e != nil {
					return struct{}{}, e
				}
				ids := []int64{}
				for rows.Next() {
					var n int64
					if e = rows.Scan(&n); e != nil {
						break
					}
					ids = append(ids, n)
				}
				if e = errors.Join(e, rows.Err(), rows.Close()); e != nil {
					return struct{}{}, e
				}
				if len(ids) == 0 {
					break
				}
				for _, n := range ids {
					a, e := getAttempt(ctx, tx, n)
					if e != nil {
						return struct{}{}, e
					}
					d := DetailAttempt{Attempt: a}
					parent := ""
					if c.Policy != nil {
						b, e := getBatchAttempt(ctx, tx, n)
						if e != nil {
							return struct{}{}, e
						}
						d.Retry = &b
						parent = strconv.FormatInt(a.CandidateID, 10)
					}
					if callbackErr = emit("attempts", parent, d); callbackErr != nil {
						return struct{}{}, nil
					}
					after = n
				}
			}
			return struct{}{}, ctx.Err()
		},
	)
	if callbackErr != nil {
		return callbackErr
	}
	return err
}
