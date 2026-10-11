package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

const sourceColumns = "run_id,source_id,channel,position,page_limit,status,started_at," +
	"finished_at,recovered_at,stop_reason"
const pageColumns = "run_id,source_id,page_number,requested_url,requested_at,before_id," +
	"status,fetched_at,parsed_post_count,min_message_id,max_message_id," +
	"next_before_id,finished_at,recovered_at,reason_code"

func hasPlan(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM crawl_sources WHERE run_id=?)", id).Scan(&exists)
	return exists, err
}

func scanSource(row interface{ Scan(...any) error }) (state.CrawlSource, error) {
	var s state.CrawlSource
	var start, finish, recovered sql.NullInt64
	var reason sql.NullString
	err := row.Scan(
		&s.RunID,
		&s.SourceID,
		&s.Channel,
		&s.Position,
		&s.PageLimit,
		&s.Status,
		&start,
		&finish,
		&recovered,
		&reason,
	)
	s.StartedAt, s.FinishedAt, s.RecoveredAt, s.StopReason = nullTime(start), nullTime(finish),
		nullTime(recovered), nullString(reason)
	return s, err
}

func getSource(ctx context.Context, tx *sql.Tx, run int64, source string) (state.CrawlSource, error) {
	return scanSource(
		tx.QueryRowContext(
			ctx,
			"SELECT "+sourceColumns+" FROM crawl_sources WHERE run_id=? AND source_id=?",
			run,
			source,
		),
	)
}

func scanPage(row interface{ Scan(...any) error }) (state.CrawlPageRecord, error) {
	var p state.CrawlPageRecord
	var requested int64
	var before, fetched, count, minID, maxID, next, finish, recovered sql.NullInt64
	var reason sql.NullString
	err := row.Scan(
		&p.RunID,
		&p.SourceID,
		&p.PageNumber,
		&p.RequestedURL,
		&requested,
		&before,
		&p.Status,
		&fetched,
		&count,
		&minID,
		&maxID,
		&next,
		&finish,
		&recovered,
		&reason,
	)
	p.RequestedAt = time.UnixMicro(requested).UTC()
	p.FetchedAt, p.FinishedAt, p.RecoveredAt = nullTime(fetched), nullTime(finish), nullTime(recovered)
	p.BeforeID, p.MinMessageID, p.MaxMessageID, p.NextBeforeID = nullID(before), nullID(minID),
		nullID(maxID), nullID(next)
	p.ReasonCode = nullString(reason)
	if count.Valid {
		n := int(count.Int64)
		p.ParsedPostCount = &n
	}
	return p, err
}

func nullID(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}
func getPage(ctx context.Context, tx *sql.Tx, id state.CrawlPageID) (state.CrawlPageRecord, error) {
	return scanPage(
		tx.QueryRowContext(
			ctx,
			"SELECT "+pageColumns+" FROM crawl_pages WHERE run_id=? AND source_id=? AND page_number=?",
			id.RunID,
			id.SourceID,
			id.PageNumber,
		),
	)
}

func readSources(ctx context.Context, tx *sql.Tx, id int64) ([]state.CrawlSource, error) {
	rows, err := tx.QueryContext(
		ctx,
		"SELECT "+sourceColumns+" FROM crawl_sources WHERE run_id=? ORDER BY position",
		id,
	)
	if err != nil {
		return nil, err
	}
	var sources []state.CrawlSource
	for rows.Next() {
		s, err := scanSource(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		sources = append(sources, s)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range sources {
		s := &sources[i]
		rows, err = tx.QueryContext(
			ctx,
			"SELECT "+pageColumns+" FROM crawl_pages WHERE run_id=? AND source_id=? ORDER BY page_number",
			id,
			s.SourceID,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			p, err := scanPage(rows)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			s.Pages = append(s.Pages, p)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
		// Grouped counts keep reads bounded by sources, rather than one query per page.
		rows, err = tx.QueryContext(
			ctx,
			`SELECT l.page_number,count(DISTINCT s.candidate_id),count(*) FROM `+
				`crawl_page_observations l JOIN sightings s ON s.id=l.sighting_id `+
				`WHERE l.run_id=? AND l.source_id=? GROUP BY l.page_number`,
			id,
			s.SourceID,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var number int
			var candidates, sightings int64
			if err := rows.Scan(&number, &candidates, &sightings); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if number < 1 || number > len(s.Pages) {
				_ = rows.Close()
				return nil, ErrStateAccess
			}
			s.Pages[number-1].SavedCandidates, s.Pages[number-1].SavedSightings = candidates, sightings
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
		err = tx.QueryRowContext(
			ctx,
			`SELECT count(DISTINCT s.candidate_id),count(*) FROM `+
				`crawl_page_observations l JOIN sightings s ON s.id=l.sighting_id `+
				`WHERE l.run_id=? AND l.source_id=?`,
			id,
			s.SourceID,
		).Scan(&s.SavedCandidates, &s.SavedSightings)
		if err != nil {
			return nil, err
		}
	}
	return sources, nil
}
