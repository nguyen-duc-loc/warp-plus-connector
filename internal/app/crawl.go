package app

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

// Crawl saves a selected source plan and processes bounded pages sequentially.
func (a *App) Crawl(ctx context.Context) (report state.CrawlReport, err error) {
	plan, err := a.Settings.SelectSources(a.SourceNames, a.PageLimit)
	if err != nil {
		return report, err
	}
	w, err := store.OpenWriter(ctx, a.Settings.StateDir, a.Clock)
	if err != nil {
		return report, classified(err)
	}
	defer closeStore(&w.Reader, &err)
	report, err = w.StartCrawlPlan(ctx, state.CrawlPlanInput{RequestID: rand.Text(), StartedAt: a.Clock(), Sources: plan})
	if err != nil {
		return report, classified(err)
	}
	id := report.Run.ID
	// A mutation error may be an uncertain commit. Never perform cleanup writes
	// after it. Only external failures and cancellation between known commits abort.
	defer func() {
		readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if saved, readErr := w.GetCrawlReport(readCtx, id); readErr == nil {
			report = saved
		} else if err == nil {
			err = classified(readErr)
		}
		a.log(id, string(report.Run.Status))
	}()
	abort := func(cause error) error {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		code := classified(cause).(*Error).Code
		status := state.Failed
		if errors.Is(cause, context.Canceled) {
			status = state.Cancelled
			code = "cancelled"
		}
		_, finishErr := w.FinishCrawlPlan(cleanup, state.RunCompletion{RunID: id, Status: status, FinishedAt: a.Clock(), ReasonCode: code})
		if finishErr != nil {
			return classified(finishErr)
		}
		return classified(cause)
	}
	failed := false
	var lastStart time.Time
	for _, selected := range plan {
		sourceID := "telegram:" + selected.Channel
		for number := 1; number <= selected.PageLimit; number++ {
			if err := pace(ctx, lastStart); err != nil {
				return report, abort(err)
			}
			pageID := state.CrawlPageID{RunID: id, SourceID: sourceID, PageNumber: number}
			request, writeErr := w.StartCrawlRequest(ctx, state.CrawlRequest{CrawlPageID: pageID, RequestedAt: a.Clock()})
			if writeErr != nil {
				return report, classified(writeErr)
			}
			if err := ctx.Err(); err != nil {
				return report, abort(err)
			}
			// Fixture sources use the call start. The HTTP adapter supplies its
			// actual monotonic request start after returning.
			lastStart = time.Now()
			page, fetchErr := a.Source.FetchPage(ctx, selected.Channel, request.BeforeID)
			if timing, ok := a.Source.(interface{ RequestStartedAt() time.Time }); ok {
				lastStart = timing.RequestStartedAt()
			}
			if fetchErr != nil {
				if ctx.Err() != nil {
					return report, abort(ctx.Err())
				}
				code := classified(fetchErr).(*Error).Code
				if _, writeErr = w.FinishCrawlPage(ctx, state.CrawlPageCompletion{CrawlPageID: pageID, Status: "failed", FinishedAt: a.Clock(), ReasonCode: code}); writeErr != nil {
					return report, classified(writeErr)
				}
				if _, writeErr = w.FinishCrawlSource(ctx, state.CrawlSourceCompletion{RunID: id, SourceID: sourceID, Status: "failed", FinishedAt: a.Clock(), StopReason: code}); writeErr != nil {
					return report, classified(writeErr)
				}
				a.logPage(id, sourceID, number, code)
				if code == "source_rate_limited" {
					return report, abort(fetchErr)
				}
				failed = true
				break
			}
			if err := ctx.Err(); err != nil {
				return report, abort(err)
			}
			facts := state.CrawlParse{CrawlPageID: pageID, FetchedAt: page.Facts.FetchedAt, ParsedPostCount: page.Facts.ParsedPostCount, MinMessageID: page.Facts.MinMessageID, MaxMessageID: page.Facts.MaxMessageID, NextBeforeID: page.NextBeforeID}
			if _, writeErr = w.RecordCrawlParse(ctx, facts); writeErr != nil {
				return report, classified(writeErr)
			}
			for _, message := range page.Messages {
				if err := ctx.Err(); err != nil {
					return report, abort(err)
				}
				if len(message.Keys) == 0 {
					continue
				}
				message.RunID = id
				if _, writeErr = w.RecordCrawlMessage(ctx, pageID, message); writeErr != nil {
					return report, classified(writeErr)
				}
			}
			if err := ctx.Err(); err != nil {
				return report, abort(err)
			}
			if _, writeErr = w.FinishCrawlPage(ctx, state.CrawlPageCompletion{CrawlPageID: pageID, Status: "completed", FinishedAt: a.Clock()}); writeErr != nil {
				return report, classified(writeErr)
			}
			a.logPage(id, sourceID, number, "completed")
			reason := ""
			if page.NextBeforeID == nil {
				reason = "preview_end"
			} else if number == selected.PageLimit {
				reason = "page_limit"
			}
			if reason != "" {
				if _, writeErr = w.FinishCrawlSource(ctx, state.CrawlSourceCompletion{RunID: id, SourceID: sourceID, Status: "completed", FinishedAt: a.Clock(), StopReason: reason}); writeErr != nil {
					return report, classified(writeErr)
				}
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return report, abort(err)
	}
	completion := state.RunCompletion{RunID: id, Status: state.Completed, FinishedAt: a.Clock()}
	if failed {
		completion.Status = state.Failed
		completion.ReasonCode = "source_failed"
	}
	report, err = w.FinishCrawlPlan(ctx, completion)
	if err != nil {
		return report, classified(err)
	}
	if failed {
		return report, &Error{Code: "source_failed"}
	}
	return report, nil
}

func pace(ctx context.Context, last time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if last.IsZero() {
		return nil
	}
	wait := time.Until(last.Add(time.Second))
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (a *App) logPage(runID int64, source string, number int, code string) {
	if a.Logger != nil {
		a.Logger.Info("crawl page", "run_id", runID, "source_id", source, "page_number", number, "result", code)
	}
}
