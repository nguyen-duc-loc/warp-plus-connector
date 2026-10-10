package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// StartCrawl creates a run and requested coverage in the same transaction.
func (w *Writer) StartCrawl(ctx context.Context, in state.CrawlInput) (state.CrawlReport, error) {
	return transact(ctx, &w.Reader, true, "start crawl", func(tx *sql.Tx) (state.CrawlReport, error) {
		var zero state.CrawlReport
		at, err := normalizeTime(in.StartedAt)
		if err != nil || !validText(in.RequestID) || in.SourceID != "telegram:warpplus" ||
			in.RequestedURL != "https://t.me/s/warpplus" || in.CoverageMode != "latest_preview" || in.PageLimit != 1 {
			return zero, ErrInvalidInput
		}
		run, err := scanRun(tx.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE request_id=?", in.RequestID))
		if err == nil {
			if run.Kind != state.Crawl || !run.StartedAt.Equal(at) {
				return zero, ErrConflict
			}
			report, err := crawlReport(ctx, tx, run.ID)
			if err != nil {
				return zero, err
			}
			c := report.Coverage
			if c == nil || c.SourceID != in.SourceID || c.RequestedURL != in.RequestedURL || c.CoverageMode != in.CoverageMode || c.PageLimit != in.PageLimit {
				return zero, ErrConflict
			}
			return report, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return zero, err
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO runs(request_id,kind,status,started_at) VALUES(?,'crawl','running',?)", in.RequestID, at.UnixMicro())
		if err != nil {
			return zero, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return zero, err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO crawl_coverage(run_id,source_id,requested_url,coverage_mode,page_limit) VALUES(?,?,?,?,?)", id, in.SourceID, in.RequestedURL, in.CoverageMode, in.PageLimit)
		if err != nil {
			return zero, err
		}
		return crawlReport(ctx, tx, id)
	})
}

// RecordCrawlPage fills parsed facts once, accepting identical normalized replays.
func (w *Writer) RecordCrawlPage(ctx context.Context, in state.CrawlPage) (state.CrawlCoverage, error) {
	if in.MinMessageID != nil {
		n := *in.MinMessageID
		in.MinMessageID = &n
	}
	if in.MaxMessageID != nil {
		n := *in.MaxMessageID
		in.MaxMessageID = &n
	}
	return transact(ctx, &w.Reader, true, "record crawl page", func(tx *sql.Tx) (state.CrawlCoverage, error) {
		var zero state.CrawlCoverage
		at, err := normalizeTime(in.FetchedAt)
		if err != nil || in.RunID <= 0 || in.ParsedPostCount < 0 {
			return zero, ErrInvalidInput
		}
		if in.ParsedPostCount == 0 {
			if in.MinMessageID != nil || in.MaxMessageID != nil {
				return zero, ErrInvalidInput
			}
		} else if in.MinMessageID == nil || in.MaxMessageID == nil || *in.MinMessageID <= 0 || *in.MaxMessageID < *in.MinMessageID {
			return zero, ErrInvalidInput
		}
		run, err := getRun(ctx, tx, in.RunID)
		if err != nil {
			return zero, err
		}
		if run.Kind != state.Crawl {
			return zero, ErrInvalidInput
		}
		c, err := getCoverage(ctx, tx, in.RunID)
		if err != nil {
			return zero, err
		}
		if c.FetchedAt != nil {
			if c.FetchedAt.Equal(at) && *c.ParsedPostCount == in.ParsedPostCount && equalID(c.MinMessageID, in.MinMessageID) && equalID(c.MaxMessageID, in.MaxMessageID) {
				return c, nil
			}
			return zero, ErrConflict
		}
		if run.Status != state.Running {
			return zero, ErrRunClosed
		}
		_, err = tx.ExecContext(ctx, "UPDATE crawl_coverage SET fetched_at=?,parsed_post_count=?,min_message_id=?,max_message_id=? WHERE run_id=?", at.UnixMicro(), in.ParsedPostCount, in.MinMessageID, in.MaxMessageID, in.RunID)
		if err != nil {
			return zero, err
		}
		return getCoverage(ctx, tx, in.RunID)
	})
}

func equalID(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func getCoverage(ctx context.Context, tx *sql.Tx, id int64) (state.CrawlCoverage, error) {
	var c state.CrawlCoverage
	var fetched, count, minID, maxID sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT run_id,source_id,requested_url,coverage_mode,page_limit,fetched_at,parsed_post_count,min_message_id,max_message_id FROM crawl_coverage WHERE run_id=?`, id).Scan(&c.RunID, &c.SourceID, &c.RequestedURL, &c.CoverageMode, &c.PageLimit, &fetched, &count, &minID, &maxID)
	c.FetchedAt = nullTime(fetched)
	if count.Valid {
		n := int(count.Int64)
		c.ParsedPostCount = &n
	}
	if minID.Valid {
		c.MinMessageID = &minID.Int64
	}
	if maxID.Valid {
		c.MaxMessageID = &maxID.Int64
	}
	return c, err
}

// GetCrawlReport reads saved coverage and counts, including active and legacy runs.
func (r *Reader) GetCrawlReport(ctx context.Context, id int64) (state.CrawlReport, error) {
	return transact(ctx, r, false, "get crawl report", func(tx *sql.Tx) (state.CrawlReport, error) {
		if id <= 0 {
			return state.CrawlReport{}, ErrInvalidInput
		}
		return crawlReport(ctx, tx, id)
	})
}

func crawlReport(ctx context.Context, tx *sql.Tx, id int64) (state.CrawlReport, error) {
	var out state.CrawlReport
	run, err := getRun(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if run.Kind != state.Crawl {
		return out, ErrInvalidInput
	}
	out.Run = run
	c, err := getCoverage(ctx, tx, id)
	if err == nil {
		out.Coverage = &c
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	err = tx.QueryRowContext(ctx, `SELECT count(DISTINCT s.candidate_id),count(*) FROM crawl_observations o JOIN sightings s ON s.id=o.sighting_id WHERE o.run_id=?`, id).Scan(&out.SavedCandidates, &out.SavedSightings)
	return out, err
}
