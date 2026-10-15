package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/diagnostic"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

type resultsJob struct {
	id                       int64
	kind, state, phase       string
	count                    int64
	expected                 *int64
	code, warning, remaining string
	result                   any
	cancel                   context.CancelFunc
	committed                bool
	committing               bool
	cancelAt                 time.Time
}

type resultsBridge struct {
	mu                  sync.Mutex
	work                sync.WaitGroup
	options             Options
	session             *app.ResultsSession
	job                 *resultsJob
	nextJob, nextReview int64
	nextWorkReview      int64
	workReview          *app.WorkReview
	review              *ExportReview
	receipt             *ExportReceipt
	fatal               chan error
}

func newResultsBridge(o Options) *resultsBridge {
	return &resultsBridge{options: o, session: o.Results, fatal: make(chan error, 1)}
}

func codeFor(err error) string {
	if err == nil {
		return ""
	}
	var a *app.Error
	if errors.As(err, &a) && diagnostic.SafeCode(a.Code) {
		return a.Code
	}
	var t *Error
	if errors.As(err, &t) && diagnostic.SafeCode(t.Code) {
		return t.Code
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "ui_snapshot_failed"
}

func (b *resultsBridge) close() error {
	b.mu.Lock()
	if b.job != nil {
		b.job.cancel()
	}
	b.mu.Unlock()
	var workErr error
	if b.options.Work != nil {
		workErr = b.options.Work.Close()
	}
	b.work.Wait()
	if b.options.Reports != nil {
		workErr = errors.Join(workErr, b.options.Reports.Close())
	}
	if b.session != nil {
		return errors.Join(workErr, b.session.Close())
	}
	return workErr
}

func (b *resultsBridge) active() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.job != nil && (b.job.state == "running" || b.job.state == "cancelling")
}

func (b *resultsBridge) jobRecord() any {
	j := b.job
	return record{
		"job_id": decimal(j.id), "kind": j.kind, "state": j.state, "phase": j.phase,
		"completed_count": decimal(j.count), "expected_count": optionalID(j.expected), "error_code": nullable(j.code),
		"warning_code": nullable(j.warning), "remaining_path": nullable(j.remaining), "result": j.result,
	}
}

func (b *resultsBridge) poll(id int64, cancel bool) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.job == nil || b.job.id != id {
		return nil, failure("ui_handle_stale")
	}
	j := b.job
	if cancel && (j.state == "running" || j.state == "cancelling") && j.cancelAt.IsZero() {
		j.cancelAt = time.Now()
		j.state = "cancelling"
		if !j.committing {
			j.cancel()
		}
		time.AfterFunc(
			5*time.Second, func() {
				b.mu.Lock()
				stuck := b.job == j && (j.state == "running" || j.state == "cancelling")
				b.mu.Unlock()
				if stuck {
					select {
					case b.fatal <- failure("ui_shutdown_timeout"):
					default:
					}
				}
			},
		)
	}
	return b.jobRecord(), nil
}

func (b *resultsBridge) start(
	parent context.Context, kind, phase string, expected *int64,
	operation func(context.Context, app.ResultsProgress, app.ResultsCommit) (any, error),
) any {
	b.mu.Lock()
	b.nextJob++
	ctx, cancel := context.WithCancel(parent)
	j := &resultsJob{id: b.nextJob, kind: kind, state: "running", phase: phase, expected: expected, cancel: cancel}
	b.job = j
	b.mu.Unlock()
	b.work.Add(1)
	go func() {
		defer b.work.Done()
		defer cancel()
		progress := func(phase string, n int64) { b.mu.Lock(); j.phase = phase; j.count = n; b.mu.Unlock() }
		commit := func(f func() error) error {
			b.mu.Lock()
			if err := ctx.Err(); err != nil {
				b.mu.Unlock()
				return err
			}
			j.committing = true
			b.mu.Unlock()
			err := f()
			b.mu.Lock()
			j.committing = false
			if err == nil {
				j.committed = true
			}
			if !j.cancelAt.IsZero() {
				j.cancel()
			}
			b.mu.Unlock()
			return err
		}

		var value any
		var err error
		if b.session != nil {
			err = b.session.Prepare(ctx)
		}
		if err == nil && b.options.Reports != nil {
			err = b.options.Reports.Prepare()
		}
		if err == nil {
			value, err = operation(ctx, progress, commit)
		} else if kind == "export" {
			b.mu.Lock()
			if b.receipt != nil {
				receipt := *b.receipt
				receipt.ErrorCode = codeFor(err)
				if b.options.Reports != nil {
					receipt.Remaining = b.options.Reports.Remaining
				}
				if receipt.Remaining == "" && b.session != nil {
					receipt.Remaining = b.session.Remaining
				}
				b.receipt = &receipt
				if b.options.ObserveExport != nil {
					b.options.ObserveExport(receipt)
				}
				value = record{"type": "export", "value": receiptRecord(receipt)}
			}
			b.mu.Unlock()
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		j.result = value
		if err == nil || j.committed {
			j.state = "succeeded"
			j.phase = "complete"
			if (kind == "report_open" || kind == "report_refresh") && b.options.Reports != nil {
				j.warning, j.remaining = b.options.Reports.Warning, b.options.Reports.Remaining
			} else if kind != "export" && b.session != nil {
				j.warning = b.session.Warning
				j.remaining = b.session.Remaining
			}
		} else {
			j.code = codeFor(err)
			j.state = "failed"
			if j.code == "cancelled" {
				j.state = "cancelled"
			}
			if kind != "export" {
				j.result = nil
			}
			if b.session != nil {
				j.remaining = b.session.Remaining
			}
			if j.code == "ui_protocol_invalid" {
				select {
				case b.fatal <- failure(j.code):
				default:
				}
			}
		}
	}()
	return record{"job_id": decimal(j.id)}
}

func requestParams(r request) (map[string]json.RawMessage, error) {
	if strings.HasPrefix(r.Method, "work.") {
		return workParams(r)
	}
	if strings.HasPrefix(r.Method, "report.") {
		return reportParams(r)
	}
	shapes := map[string]string{
		"results.open": "", "results.refresh": "view_id focus_id", "results.filter": "view_id filters",
		"results.page": "view_id offset", "results.mark": "view_id candidate_id marked",
		"results.clear_marks": "view_id",
		"details.open":        "view_id candidate_id", "details.page": "detail_id section offset",
		"details.close":    "detail_id",
		"candidate.reveal": "view_id detail_id candidate_id",
		"export.review":    "view_id selection format include_keys path", "export.start": "review_id",
		"export.discard": "review_id", "job.poll": "job_id", "job.cancel": "job_id",
	}
	shape, ok := shapes[r.Method]
	if !ok {
		return nil, failure("ui_protocol_invalid")
	}
	var p map[string]json.RawMessage
	if strictJSONNullable(
		r.Params,
		&p,
		r.Method == "results.refresh",
	) != nil || p == nil || len(p) != len(strings.Fields(shape)) {
		return nil, failure("ui_protocol_invalid")
	}
	for _, key := range strings.Fields(shape) {
		raw, ok := p[key]
		if !ok {
			return nil, failure("ui_protocol_invalid")
		}
		if key == "focus_id" && string(raw) == "null" {
			continue
		}
		if strings.HasSuffix(key, "_id") || key == "offset" {
			var s string
			if json.Unmarshal(raw, &s) != nil || !decimalPattern.MatchString(s) {
				return nil, failure("ui_protocol_invalid")
			}
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil || (n == 0 && key != "offset") {
				return nil, failure("ui_protocol_invalid")
			}
		} else if key == "marked" || key == "include_keys" {
			var v bool
			if string(raw) == "null" || json.Unmarshal(raw, &v) != nil {
				return nil, failure("ui_protocol_invalid")
			}
		} else if key == "filters" {
			var q app.CandidateQuery
			if strictJSON(raw, &q) != nil || q.Status == nil || q.Definitive == nil || q.Freshness == nil {
				return nil, failure("ui_protocol_invalid")
			}
			n, err := q.Normalize()
			if err != nil || !reflect.DeepEqual(n, q) {
				return nil, failure("ui_protocol_invalid")
			}
		} else {
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return nil, failure("ui_protocol_invalid")
			}
			allowed := map[string][]string{
				"section": {"sources", "attempts"}, "selection": {"matching", "marked"},
				"format": {"json", "keys"},
			}
			if choices, ok := allowed[key]; ok && v != choices[0] && v != choices[1] {
				return nil, failure("ui_protocol_invalid")
			}
		}
	}
	if r.Method == "export.review" && textParam(p, "format") == "keys" && !boolParam(p, "include_keys") {
		return nil, failure("ui_protocol_invalid")
	}
	return p, nil
}

func textParam(p map[string]json.RawMessage, k string) string {
	var v string
	_ = json.Unmarshal(p[k], &v)
	return v
}
func idParam(p map[string]json.RawMessage, k string) int64 {
	n, _ := strconv.ParseInt(textParam(p, k), 10, 64)
	return n
}
func boolParam(p map[string]json.RawMessage, k string) bool {
	var v bool
	_ = json.Unmarshal(p[k], &v)
	return v
}

func (b *resultsBridge) dispatch(ctx context.Context, r request) (any, error) {
	p, err := requestParams(r)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(r.Method, "work.") {
		return b.dispatchWork(ctx, r, p)
	}
	if strings.HasPrefix(r.Method, "report.") {
		return b.dispatchReport(ctx, r, p)
	}
	if r.Method == "job.poll" || r.Method == "job.cancel" {
		return b.poll(idParam(p, "job_id"), r.Method == "job.cancel")
	}
	if b.active() {
		return nil, failure("ui_busy")
	}
	s := b.session
	if s == nil {
		return nil, failure("ui_handle_stale")
	}
	if _, ok := p["view_id"]; ok {
		if err = s.RequireView(idParam(p, "view_id")); err != nil {
			return nil, err
		}
	}
	if _, ok := p["detail_id"]; ok {
		if s.Detail == nil || s.Detail.ID != idParam(p, "detail_id") {
			return nil, failure("ui_handle_stale")
		}
	}
	pageCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	viewResult := func() any { return record{"type": "view", "value": viewRecord(s.View)} }
	switch r.Method {
	case "results.open", "results.refresh":
		refresh := r.Method == "results.refresh"
		if refresh != (s.View != nil) {
			return nil, failure("ui_handle_stale")
		}
		var focus *int64
		if string(p["focus_id"]) != "null" && refresh {
			v := idParam(p, "focus_id")
			focus = &v
		}
		b.review = nil
		b.workReview = nil
		kind := "open"
		if refresh {
			kind = "refresh"
		}
		return b.start(
			ctx,
			kind,
			"collection",
			nil,
			func(ctx context.Context, p app.ResultsProgress, c app.ResultsCommit) (any, error) {
				if err := s.Open(ctx, refresh, focus, p, c); err != nil {
					return nil, err
				}
				return viewResult(), nil
			},
		), nil
	case "results.filter":
		var q app.CandidateQuery
		_ = json.Unmarshal(p["filters"], &q)
		b.review = nil
		b.workReview = nil
		return b.start(
			ctx,
			"filter",
			"view",
			nil,
			func(ctx context.Context, p app.ResultsProgress, c app.ResultsCommit) (any, error) {
				if err := s.Filter(ctx, q, p, c); err != nil {
					return nil, err
				}
				return viewResult(), nil
			},
		), nil
	case "results.clear_marks":
		b.review = nil
		b.workReview = nil
		return b.start(
			ctx,
			"clear_marks",
			"view",
			nil,
			func(ctx context.Context, _ app.ResultsProgress, c app.ResultsCommit) (any, error) {
				if err := s.ClearMarks(ctx, c); err != nil {
					return nil, err
				}
				return viewResult(), nil
			},
		), nil
	case "results.page":
		offset := idParam(p, "offset")
		rows, err := s.Page(pageCtx, offset)
		if err != nil {
			return nil, err
		}
		items := []any{}
		for _, r := range rows {
			items = append(items, record{"summary": summaryRecord(r.Candidate), "marked": r.Marked})
		}
		items, next, err := boundedItems(items, offset, s.View.Count)
		return record{"view": viewRecord(s.View), "offset": decimal(offset), "items": items, "next_offset": next}, err
	case "results.mark":
		id := idParam(p, "candidate_id")
		marked := boolParam(p, "marked")
		if err := s.Mark(pageCtx, id, marked); err != nil {
			return nil, err
		}
		b.review = nil
		b.workReview = nil
		return record{
			"view_id": decimal(s.View.ID), "candidate_id": decimal(id), "marked": marked,
			"marked_count": decimal(s.View.Marked),
		}, nil
	case "details.open":
		id := idParam(p, "candidate_id")
		return b.start(
			ctx,
			"details",
			"summary",
			nil,
			func(ctx context.Context, p app.ResultsProgress, c app.ResultsCommit) (any, error) {
				if err := s.OpenDetail(ctx, id, p, c); err != nil {
					return nil, err
				}
				return record{"type": "detail", "value": detailRecord(s.Detail)}, nil
			},
		), nil
	case "details.close":
		return record{}, s.CloseDetail(pageCtx)
	case "details.page":
		offset := idParam(p, "offset")
		section := textParam(p, "section")
		rows, err := s.History(pageCtx, section, offset)
		if err != nil {
			return nil, err
		}
		items := []any{}
		total := s.Detail.Sources
		for _, raw := range rows {
			if section == "sources" {
				var v state.Sighting
				if json.Unmarshal(raw, &v) != nil {
					return nil, failure("ui_snapshot_failed")
				}
				items = append(items, sourceRecord(v))
			} else {
				var v store.DetailAttempt
				if json.Unmarshal(raw, &v) != nil {
					return nil, failure("ui_snapshot_failed")
				}
				items = append(items, attemptRecord(v))
			}
		}
		if section == "attempts" {
			total = s.Detail.Attempts
		}
		items, next, err := boundedItems(items, offset, total)
		return record{
			"detail_id": decimal(s.Detail.ID), "section": section, "offset": decimal(offset), "total": decimal(total),
			"items": items, "next_offset": next,
		}, err
	case "candidate.reveal":
		id := idParam(p, "candidate_id")
		if s.Detail.ViewID != s.View.ID || s.Detail.CandidateID != id {
			return nil, failure("ui_candidate_unavailable")
		}
		row, err := s.Candidate(pageCtx, id)
		if err != nil {
			return nil, err
		}
		if !utf8.ValidString(row.Candidate.Key) {
			return nil, failure("ui_snapshot_failed")
		}
		value := record{
			"view_id": decimal(s.View.ID), "detail_id": decimal(s.Detail.ID), "candidate_id": decimal(id),
			"key": row.Candidate.Key,
		}
		raw, err := json.Marshal(record{"version": 3, "id": r.ID, "result": value})
		if err != nil || len(raw)+1 > 256*1024 {
			return nil, failure("ui_key_too_large")
		}
		return value, nil
	case "export.review":
		b.review = nil
		b.workReview = nil
		selection := textParam(p, "selection")
		count := s.View.Count
		if selection == "marked" {
			count = s.View.Marked
			if count == 0 {
				return nil, failure("ui_export_selection_empty")
			}
		}
		review := ExportReview{
			View: *s.View, Selection: selection, Count: count, Format: textParam(p, "format"),
			IncludeKeys: boolParam(p, "include_keys"), Exclusions: []string{s.StateExclusion(), b.options.StateDir},
		}
		return b.start(
			ctx,
			"review",
			"destination",
			nil,
			func(ctx context.Context, _ app.ResultsProgress, c app.ResultsCommit) (any, error) {
				if err := s.Prepare(ctx); err != nil {
					return nil, err
				}
				if b.options.Destination == nil {
					return nil, failure("export_destination_invalid")
				}
				dest, info, err := b.options.Destination(ctx, textParam(p, "path"), review.Exclusions)
				if err != nil {
					return nil, err
				}
				review.Destination = dest
				review.ParentIdentity = info
				b.nextReview++
				review.ID = b.nextReview
				if err = c(func() error { b.review = &review; return nil }); err != nil {
					return nil, err
				}
				return record{"type": "review", "value": reviewRecord(review)}, nil
			},
		), nil
	case "export.discard", "export.start":
		if b.review == nil || b.review.ID != idParam(p, "review_id") {
			return nil, failure("ui_handle_stale")
		}
		review := *b.review
		b.review = nil
		b.workReview = nil
		if r.Method == "export.discard" {
			return record{}, nil
		}
		if s.View.ID != review.View.ID || s.View.Revision != review.View.Revision {
			return nil, failure("ui_handle_stale")
		}
		initial := ExportReceipt{
			Destination: review.Destination, Selection: review.Selection, Format: review.Format,
			IncludeKeys: review.IncludeKeys, Count: review.Count, Published: new(false), Durability: "not_published",
		}
		b.mu.Lock()
		b.receipt = &initial
		if b.options.ObserveExport != nil {
			b.options.ObserveExport(initial)
		}
		b.mu.Unlock()
		return b.start(
			ctx, "export", "encoding", &review.Count,
			func(ctx context.Context, _ app.ResultsProgress, _ app.ResultsCommit) (any, error) {
				if err := s.Prepare(ctx); err != nil {
					initial.ErrorCode = codeFor(err)
					initial.Remaining = s.Remaining
					b.mu.Lock()
					b.receipt = &initial
					if b.options.ObserveExport != nil {
						b.options.ObserveExport(initial)
					}
					b.mu.Unlock()
					return record{"type": "export", "value": receiptRecord(initial)}, err
				}
				receipt := b.options.Export(
					ctx, review, func(ctx context.Context, emit func(state.Candidate) error) error {
						return s.VisitExport(ctx, review.Selection == "marked", emit)
					}, func(phase string, r ExportReceipt) {
						b.mu.Lock()
						b.receipt = &r
						if b.options.ObserveExport != nil {
							b.options.ObserveExport(r)
						}
						b.job.phase = phase
						b.job.count = r.Prepared
						b.mu.Unlock()
					},
				)
				b.mu.Lock()
				b.receipt = &receipt
				if b.options.ObserveExport != nil {
					b.options.ObserveExport(receipt)
				}
				b.mu.Unlock()
				var err error
				if receipt.ErrorCode != "" {
					err = failure(receipt.ErrorCode)
				}
				return record{"type": "export", "value": receiptRecord(receipt)}, err
			},
		), nil
	}
	return nil, failure("ui_protocol_invalid")
}
