package store

import (
	"context"
	"database/sql"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// FinishCrawlPage records an attested completion or an explicit failure.
func (w *Writer) FinishCrawlPage(ctx context.Context, in state.CrawlPageCompletion) (state.CrawlPageRecord, error) {
	return transact(ctx, &w.Reader, true, "finish crawl page", func(tx *sql.Tx) (state.CrawlPageRecord, error) {
		var zero state.CrawlPageRecord
		at, err := normalizeTime(in.FinishedAt)
		if err != nil || !validPageOutcome(in.Status, in.ReasonCode) {
			return zero, ErrInvalidInput
		}
		p, err := getPage(ctx, tx, in.CrawlPageID)
		if err != nil {
			return zero, err
		}
		if p.Status != "requested" && p.Status != "parsed" {
			if p.Status == in.Status && p.FinishedAt != nil && p.FinishedAt.Equal(at) && sameReason(p.ReasonCode, in.ReasonCode) {
				return p, nil
			}
			return zero, ErrConflict
		}
		if err := activePage(ctx, tx, p, p.Status); err != nil {
			return zero, err
		}
		if in.Status == "completed" && p.Status != "parsed" {
			return zero, ErrConflict
		}
		if _, err := tx.ExecContext(ctx, "UPDATE crawl_pages SET status=?,finished_at=?,reason_code=? WHERE run_id=? AND source_id=? AND page_number=?", in.Status, at.UnixMicro(), optionalString(in.ReasonCode), in.RunID, in.SourceID, in.PageNumber); err != nil {
			return zero, err
		}
		return getPage(ctx, tx, in.CrawlPageID)
	})
}

func validPageOutcome(status, reason string) bool {
	return status == "completed" && reason == "" || (status == "failed" || status == "cancelled") && reasonPattern.MatchString(reason)
}
func sameReason(saved *string, reason string) bool {
	return saved == nil && reason == "" || saved != nil && *saved == reason
}

// FinishCrawlSource validates its last page and the declared coverage boundary.
func (w *Writer) FinishCrawlSource(ctx context.Context, in state.CrawlSourceCompletion) (state.CrawlSource, error) {
	return transact(ctx, &w.Reader, true, "finish crawl source", func(tx *sql.Tx) (state.CrawlSource, error) {
		var zero state.CrawlSource
		at, err := normalizeTime(in.FinishedAt)
		if err != nil || !reasonPattern.MatchString(in.StopReason) || (in.Status != "completed" && in.Status != "failed" && in.Status != "cancelled") {
			return zero, ErrInvalidInput
		}
		s, err := getSource(ctx, tx, in.RunID, in.SourceID)
		if err != nil {
			return zero, err
		}
		if s.Status != "running" {
			if s.Status == in.Status && s.FinishedAt != nil && s.FinishedAt.Equal(at) && sameReason(s.StopReason, in.StopReason) {
				return s, nil
			}
			return zero, ErrConflict
		}
		if err := activePlan(ctx, tx, in.RunID); err != nil {
			return zero, err
		}
		p, err := scanPage(tx.QueryRowContext(ctx, "SELECT "+pageColumns+" FROM crawl_pages WHERE run_id=? AND source_id=? ORDER BY page_number DESC LIMIT 1", in.RunID, in.SourceID))
		if err != nil {
			return zero, err
		}
		if p.Status == "requested" || p.Status == "parsed" {
			return zero, ErrConflict
		}
		if in.Status == "completed" {
			if p.Status != "completed" {
				return zero, ErrConflict
			}
			reason := "preview_end"
			if p.NextBeforeID != nil {
				if p.PageNumber != s.PageLimit {
					return zero, ErrConflict
				}
				reason = "page_limit"
			}
			if in.StopReason != reason {
				return zero, ErrConflict
			}
		} else if p.Status != in.Status || !sameReason(p.ReasonCode, in.StopReason) {
			return zero, ErrConflict
		}
		if _, err := tx.ExecContext(ctx, "UPDATE crawl_sources SET status=?,finished_at=?,stop_reason=? WHERE run_id=? AND source_id=?", in.Status, at.UnixMicro(), in.StopReason, in.RunID, in.SourceID); err != nil {
			return zero, err
		}
		return getSource(ctx, tx, in.RunID, in.SourceID)
	})
}

// FinishCrawlPlan finalizes the parent and atomically aborts unfinished children.
func (w *Writer) FinishCrawlPlan(ctx context.Context, in state.RunCompletion) (state.CrawlReport, error) {
	return transact(ctx, &w.Reader, true, "finish crawl plan", func(tx *sql.Tx) (state.CrawlReport, error) {
		var zero state.CrawlReport
		at, err := normalizeTime(in.FinishedAt)
		if err != nil || !validPageOutcome(string(in.Status), in.ReasonCode) {
			return zero, ErrInvalidInput
		}
		r, err := crawlReport(ctx, tx, in.RunID)
		if err != nil {
			return zero, err
		}
		if len(r.Sources) == 0 {
			return zero, ErrInvalidInput
		}
		if r.Run.Status != state.Running {
			if r.Run.Status == in.Status && r.Run.FinishedAt != nil && r.Run.FinishedAt.Equal(at) && sameReason(r.Run.ReasonCode, in.ReasonCode) {
				return r, nil
			}
			return zero, ErrConflict
		}
		if in.Status == state.Completed || in.ReasonCode == "source_failed" {
			failed := false
			for _, s := range r.Sources {
				if s.Status == "pending" || s.Status == "running" || in.Status == state.Completed && s.Status != "completed" {
					return zero, ErrConflict
				}
				if s.Status != "completed" {
					failed = true
				}
			}
			if in.ReasonCode == "source_failed" && !failed {
				return zero, ErrConflict
			}
		} else {
			pending := "state_abort"
			if in.Status == state.Cancelled {
				pending = "cancelled"
			}
			if in.ReasonCode == "source_rate_limited" {
				pending = "rate_limit_abort"
			}
			if _, err := tx.ExecContext(ctx, "UPDATE crawl_pages SET status=?,finished_at=?,reason_code=? WHERE run_id=? AND status IN ('requested','parsed')", in.Status, at.UnixMicro(), in.ReasonCode, in.RunID); err != nil {
				return zero, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE crawl_sources SET status=?,finished_at=?,stop_reason=? WHERE run_id=? AND status='running'", in.Status, at.UnixMicro(), in.ReasonCode, in.RunID); err != nil {
				return zero, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE crawl_sources SET status='skipped',finished_at=?,stop_reason=? WHERE run_id=? AND status='pending'", at.UnixMicro(), pending, in.RunID); err != nil {
				return zero, err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE runs SET status=?,finished_at=?,reason_code=? WHERE id=?", in.Status, at.UnixMicro(), optionalString(in.ReasonCode), in.RunID); err != nil {
			return zero, err
		}
		return crawlReport(ctx, tx, in.RunID)
	})
}
