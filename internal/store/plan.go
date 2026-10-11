package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// StartCrawlPlan atomically saves an immutable selected plan before requests.
func (w *Writer) StartCrawlPlan(ctx context.Context, in state.CrawlPlanInput) (state.CrawlReport, error) {
	in.Origin = normalizedOrigin(in.Origin)
	if in.Origin != "manual" && in.Origin != "scheduled" {
		return state.CrawlReport{}, ErrInvalidInput
	}
	in.Sources = append([]state.SourcePlan(nil), in.Sources...)
	return transact(
		ctx, &w.Reader, true, "start crawl plan", func(tx *sql.Tx) (state.CrawlReport, error) {
			var zero state.CrawlReport
			at, err := normalizeTime(in.StartedAt)
			if err != nil || !validText(in.RequestID) || len(in.Sources) < 1 || len(in.Sources) > 32 {
				return zero, ErrInvalidInput
			}
			seen := map[string]bool{}
			for i := range in.Sources {
				s := &in.Sources[i]
				channel, ok := state.NormalizeChannel(s.Channel)
				if !ok || seen[channel] || s.PageLimit < 1 || s.PageLimit > 100 {
					return zero, ErrInvalidInput
				}
				s.Channel = channel
				seen[channel] = true
			}
			run, err := scanRun(
				tx.QueryRowContext(
					ctx,
					"SELECT "+runColumns+" FROM runs WHERE request_id=?",
					in.RequestID,
				),
			)
			if err == nil {
				if run.Origin != in.Origin || run.Kind != state.Crawl || !run.StartedAt.Equal(at) {
					return zero, ErrConflict
				}
				report, err := crawlReport(ctx, tx, run.ID)
				if err != nil {
					return zero, err
				}
				if report.Coverage != nil || len(report.Sources) != len(in.Sources) {
					return zero, ErrConflict
				}
				for i, s := range report.Sources {
					if s.Channel != in.Sources[i].Channel || s.PageLimit != in.Sources[i].PageLimit {
						return zero, ErrConflict
					}
				}
				return report, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return zero, err
			}
			result, err := tx.ExecContext(
				ctx,
				"INSERT INTO runs(request_id,kind,status,started_at,origin) VALUES(?,'crawl','running',?,?)",
				in.RequestID,
				at.UnixMicro(), in.Origin,
			)
			if err != nil {
				return zero, err
			}
			id, err := result.LastInsertId()
			if err != nil {
				return zero, err
			}
			for i, s := range in.Sources {
				if _, err := tx.ExecContext(
					ctx,
					"INSERT INTO crawl_sources(run_id,source_id,channel,position,"+
						"page_limit,status) VALUES(?,?,?,?,?,'pending')",
					id,
					"telegram:"+s.Channel,
					s.Channel,
					i+1,
					s.PageLimit,
				); err != nil {
					return zero, err
				}
			}
			return crawlReport(ctx, tx, id)
		},
	)
}

func activePlan(ctx context.Context, tx *sql.Tx, runID int64) error {
	r, err := getRun(ctx, tx, runID)
	if err != nil {
		return err
	}
	if r.Kind != state.Crawl {
		return ErrInvalidInput
	}
	if r.Status != state.Running {
		return ErrRunClosed
	}
	return nil
}

// StartCrawlRequest derives the next request from saved coverage and persists intent.
func (w *Writer) StartCrawlRequest(ctx context.Context, in state.CrawlRequest) (state.CrawlPageRecord, error) {
	return transact(
		ctx, &w.Reader, true, "start crawl request", func(tx *sql.Tx) (state.CrawlPageRecord, error) {
			var zero state.CrawlPageRecord
			at, err := normalizeTime(in.RequestedAt)
			if err != nil || in.RunID <= 0 || in.PageNumber < 1 {
				return zero, ErrInvalidInput
			}
			existing, err := getPage(ctx, tx, in.CrawlPageID)
			if err == nil {
				if !existing.RequestedAt.Equal(at) {
					return zero, ErrConflict
				}
				return existing, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return zero, err
			}
			if err := activePlan(ctx, tx, in.RunID); err != nil {
				return zero, err
			}
			s, err := getSource(ctx, tx, in.RunID, in.SourceID)
			if err != nil {
				return zero, err
			}
			if s.Status != "pending" && s.Status != "running" {
				return zero, ErrRunClosed
			}
			if in.PageNumber > s.PageLimit {
				return zero, ErrInvalidInput
			}
			var count int
			if err := tx.QueryRowContext(
				ctx,
				"SELECT count(*) FROM crawl_pages WHERE run_id=? AND source_id=?",
				in.RunID,
				in.SourceID,
			).Scan(&count); err != nil {
				return zero, err
			}
			if count+1 != in.PageNumber {
				return zero, ErrConflict
			}
			url := "https://t.me/s/" + s.Channel
			var before *int64
			if in.PageNumber > 1 {
				previous, err := getPage(
					ctx,
					tx,
					state.CrawlPageID{RunID: in.RunID, SourceID: in.SourceID, PageNumber: in.PageNumber - 1},
				)
				if err != nil {
					return zero, err
				}
				if previous.Status != "completed" || previous.NextBeforeID == nil {
					return zero, ErrConflict
				}
				before = previous.NextBeforeID
				url += "?before=" + strconv.FormatInt(*before, 10)
			}
			if s.Status == "pending" {
				if _, err := tx.ExecContext(
					ctx,
					"UPDATE crawl_sources SET status='running',started_at=? WHERE run_id=? AND source_id=?",
					at.UnixMicro(),
					in.RunID,
					in.SourceID,
				); err != nil {
					return zero, err
				}
			}
			if _, err := tx.ExecContext(
				ctx,
				"INSERT INTO crawl_pages(run_id,source_id,page_number,requested_url,"+
					"requested_at,before_id,status) VALUES(?,?,?,?,?,?,'requested')",
				in.RunID,
				in.SourceID,
				in.PageNumber,
				url,
				at.UnixMicro(),
				before,
			); err != nil {
				return zero, err
			}
			return getPage(ctx, tx, in.CrawlPageID)
		},
	)
}

func copyID(v *int64) *int64 {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}

// RecordCrawlParse fills facts once after full adapter validation.
func (w *Writer) RecordCrawlParse(ctx context.Context, in state.CrawlParse) (state.CrawlPageRecord, error) {
	in.MinMessageID, in.MaxMessageID, in.NextBeforeID = copyID(in.MinMessageID),
		copyID(in.MaxMessageID), copyID(in.NextBeforeID)
	return transact(
		ctx, &w.Reader, true, "record crawl parse", func(tx *sql.Tx) (state.CrawlPageRecord, error) {
			var zero state.CrawlPageRecord
			at, err := normalizeTime(in.FetchedAt)
			if err != nil || in.ParsedPostCount < 0 {
				return zero, ErrInvalidInput
			}
			if in.ParsedPostCount == 0 {
				if in.MinMessageID != nil || in.MaxMessageID != nil || in.NextBeforeID != nil {
					return zero, ErrInvalidInput
				}
			} else if in.MinMessageID == nil || in.MaxMessageID == nil || *in.MinMessageID <= 0 ||
				*in.MaxMessageID < *in.MinMessageID {
				return zero, ErrInvalidInput
			}
			p, err := getPage(ctx, tx, in.CrawlPageID)
			if err != nil {
				return zero, err
			}
			if in.NextBeforeID != nil && (*in.NextBeforeID <= 0 ||
				*in.NextBeforeID > *in.MinMessageID || p.BeforeID != nil && *in.NextBeforeID >= *p.BeforeID) {
				return zero, ErrInvalidInput
			}
			if p.FetchedAt != nil {
				if p.FetchedAt.Equal(at) && *p.ParsedPostCount == in.ParsedPostCount && equalID(
					p.MinMessageID,
					in.MinMessageID,
				) && equalID(p.MaxMessageID, in.MaxMessageID) && equalID(p.NextBeforeID, in.NextBeforeID) {
					return p, nil
				}
				return zero, ErrConflict
			}
			if err := activePage(ctx, tx, p, "requested"); err != nil {
				return zero, err
			}
			if _, err := tx.ExecContext(
				ctx,
				"UPDATE crawl_pages SET status='parsed',fetched_at=?,"+
					"parsed_post_count=?,min_message_id=?,max_message_id=?,"+
					"next_before_id=? WHERE run_id=? AND source_id=? AND page_number=?",
				at.UnixMicro(),
				in.ParsedPostCount,
				in.MinMessageID,
				in.MaxMessageID,
				in.NextBeforeID,
				in.RunID,
				in.SourceID,
				in.PageNumber,
			); err != nil {
				return zero, err
			}
			return getPage(ctx, tx, in.CrawlPageID)
		},
	)
}

func activePage(ctx context.Context, tx *sql.Tx, p state.CrawlPageRecord, status string) error {
	if err := activePlan(ctx, tx, p.RunID); err != nil {
		return err
	}
	s, err := getSource(ctx, tx, p.RunID, p.SourceID)
	if err != nil {
		return err
	}
	if s.Status != "running" || p.Status != status {
		return ErrRunClosed
	}
	return nil
}

// RecordCrawlMessage saves discovery and its first page link in one transaction.
func (w *Writer) RecordCrawlMessage(
	ctx context.Context,
	page state.CrawlPageID,
	in state.MessageInput,
) (state.MessageResult, error) {
	in.Keys = append([]string(nil), in.Keys...)
	if in.PublishedAt != nil {
		v := *in.PublishedAt
		in.PublishedAt = &v
	}
	if in.MessageURL != nil {
		v := *in.MessageURL
		in.MessageURL = &v
	}
	return transact(
		ctx, &w.Reader, true, "record crawl message", func(tx *sql.Tx) (state.MessageResult, error) {
			var zero state.MessageResult
			if in.RunID != page.RunID || in.SourceID != page.SourceID {
				return zero, ErrInvalidInput
			}
			p, err := getPage(ctx, tx, page)
			if err != nil {
				return zero, err
			}
			if err := activePage(ctx, tx, p, "parsed"); err != nil {
				return zero, err
			}
			at, err := normalizeTime(in.ObservedAt)
			if err != nil || !at.Equal(*p.FetchedAt) {
				return zero, ErrInvalidInput
			}
			return recordMessage(ctx, tx, in, &page)
		},
	)
}
