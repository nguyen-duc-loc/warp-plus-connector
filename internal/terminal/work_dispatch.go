package terminal

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
)

func workParams(r request) (map[string]json.RawMessage, error) {
	shapes := map[string]string{
		"work.options": "", "work.review_crawl": "channels page_limit",
		"work.review_check": "view_id selection candidate_id", "work.review_release": "review_id",
		"work.start": "review_id", "work.poll": "operation_id", "work.cancel": "operation_id",
		"work.acknowledge": "operation_id",
	}
	shape, ok := shapes[r.Method]
	if !ok {
		return nil, failure("ui_protocol_invalid")
	}
	var p map[string]json.RawMessage
	if strictJSONNullable(r.Params, &p, true) != nil || p == nil || len(p) != len(strings.Fields(shape)) {
		return nil, failure("ui_protocol_invalid")
	}
	for _, key := range strings.Fields(shape) {
		raw, ok := p[key]
		if !ok {
			return nil, failure("ui_protocol_invalid")
		}
		if string(raw) == "null" && (key == "candidate_id" || key == "page_limit") {
			continue
		}
		if key == "channels" {
			var values []json.RawMessage
			if json.Unmarshal(raw, &values) != nil || len(values) < 1 || len(values) > 32 {
				return nil, failure("ui_protocol_invalid")
			}
			for _, value := range values {
				var channel string
				if string(value) == "null" || json.Unmarshal(value, &channel) != nil {
					return nil, failure("ui_protocol_invalid")
				}
			}
		} else {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return nil, failure("ui_protocol_invalid")
			}
			if key == "selection" {
				if value != "focused" && value != "marked" && value != "matching" {
					return nil, failure("ui_protocol_invalid")
				}
			} else if !decimalPattern.MatchString(value) {
				return nil, failure("ui_protocol_invalid")
			} else {
				n, err := strconv.ParseInt(value, 10, 64)
				if err != nil || n == 0 {
					return nil, failure("state_invalid_input")
				}
			}
		}
	}
	if r.Method == "work.review_check" &&
		((textParam(p, "selection") == "focused") == (string(p["candidate_id"]) == "null")) {
		return nil, failure("ui_protocol_invalid")
	}
	return p, nil
}

func (b *resultsBridge) dispatchWork(ctx context.Context, r request, p map[string]json.RawMessage) (any, error) {
	w := b.options.Work
	if w == nil {
		return nil, failure("ui_handle_stale")
	}
	if r.Method == "work.options" {
		return workOptions(b.options.Settings), nil
	}
	if r.Method == "work.poll" || r.Method == "work.cancel" {
		v, err := w.Poll(idParam(p, "operation_id"), r.Method == "work.cancel")
		if err != nil {
			return nil, err
		}
		return workRecord(v), nil
	}
	if r.Method == "work.acknowledge" {
		if err := w.Acknowledge(idParam(p, "operation_id")); err != nil {
			return nil, err
		}
		return record{"acknowledged": true, "unresolved_cleanup": unresolvedRecord(w.Unresolved())}, nil
	}
	if b.active() {
		return nil, failure("ui_busy")
	}
	if r.Method == "work.review_release" || r.Method == "work.start" {
		if b.workReview == nil || b.workReview.ID != idParam(p, "review_id") {
			return nil, failure("ui_handle_stale")
		}
		if r.Method == "work.review_release" {
			b.workReview = nil
			return record{"released": true}, nil
		}
		if w.Busy() {
			return nil, failure("ui_busy")
		}
		review := b.workReview
		if review.View != nil {
			if b.session.View == nil || b.session.View.ID != review.View.ID ||
				(review.Selection == "marked" && b.session.View.Revision != review.View.Revision) {
				b.workReview = nil
				return nil, failure("ui_handle_stale")
			}
		}
		b.workReview = nil
		id, err := w.Start(ctx, review)
		if err != nil {
			return nil, err
		}
		return record{"operation_id": decimal(id)}, nil
	}
	if w.Busy() {
		return nil, failure("ui_busy")
	}
	var prepare func(context.Context) (*app.WorkReview, error)
	if r.Method == "work.review_crawl" {
		var channels []string
		_ = json.Unmarshal(p["channels"], &channels)
		var limit *int
		if string(p["page_limit"]) != "null" {
			n, err := strconv.Atoi(textParam(p, "page_limit"))
			if err != nil || n < 1 || n > 100 {
				return nil, failure("state_invalid_input")
			}
			limit = &n
		}
		prepare = func(context.Context) (*app.WorkReview, error) { return w.ReviewCrawl(channels, limit) }
	} else {
		if b.session == nil {
			return nil, failure("ui_handle_stale")
		}
		if err := b.session.RequireView(idParam(p, "view_id")); err != nil {
			return nil, err
		}
		var candidate *int64
		if string(p["candidate_id"]) != "null" {
			n := idParam(p, "candidate_id")
			candidate = &n
		}
		prepare = func(ctx context.Context) (*app.WorkReview, error) {
			return w.ReviewCheck(ctx, b.session, textParam(p, "selection"), candidate)
		}
	}
	return b.start(
		ctx, "work_review", "selection", nil,
		func(ctx context.Context, _ app.ResultsProgress, commit app.ResultsCommit) (any, error) {
			review, err := prepare(ctx)
			if err != nil {
				return nil, err
			}
			b.nextWorkReview++
			review.ID = b.nextWorkReview
			if err = commit(func() error { b.workReview = review; return nil }); err != nil {
				return nil, err
			}
			return record{"type": "work_review", "value": workReviewRecord(review, b.options.Settings)}, nil
		},
	), nil
}
