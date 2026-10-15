package terminal

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
)

func reportParams(r request) (map[string]json.RawMessage, error) {
	shapes := map[string]string{
		"report.open": "run_id", "report.refresh": "report_id focus",
		"report.state": "report_id", "report.close": "report_id",
		"report.page": "report_id section source_id candidate_id offset",
	}
	shape, ok := shapes[r.Method]
	if !ok {
		return nil, failure("ui_protocol_invalid")
	}
	return reportFields(r.Params, shape)
}

func reportFields(raw json.RawMessage, shape string) (map[string]json.RawMessage, error) {
	var p map[string]json.RawMessage
	if strictJSONNullable(raw, &p, true) != nil || p == nil || len(p) != len(strings.Fields(shape)) {
		return nil, failure("ui_protocol_invalid")
	}
	for _, key := range strings.Fields(shape) {
		value, ok := p[key]
		if !ok {
			return nil, failure("ui_protocol_invalid")
		}
		if key == "focus" {
			if _,
				err := reportFields(
				value,
				"section source_id candidate_id page_number attempt_id offset",
			); err != nil {
				return nil, err
			}
			continue
		}
		if string(value) == "null" &&
			(key == "source_id" || key == "candidate_id" || key == "page_number" || key == "attempt_id") {
			continue
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			return nil, failure("ui_protocol_invalid")
		}
		if key == "section" {
			if text != "summary" && text != "sources" && text != "pages" && text != "items" && text != "attempts" {
				return nil, failure("ui_protocol_invalid")
			}
		} else if key != "source_id" {
			if !decimalPattern.MatchString(text) {
				return nil, failure("ui_protocol_invalid")
			}
			if _, err := strconv.ParseInt(text, 10, 64); err != nil {
				return nil, failure("state_invalid_input")
			}
		}
	}
	return p, nil
}

func focusParams(p map[string]json.RawMessage) app.ReportFocus {
	f := app.ReportFocus{
		Section: textParam(
			p,
			"section",
		), SourceID: textParam(p, "source_id"), Offset: idParam(p, "offset"),
	}
	for _, field := range []struct {
		name   string
		target **int64
	}{
		{"candidate_id", &f.CandidateID}, {"page_number", &f.PageNumber}, {"attempt_id", &f.AttemptID},
	} {
		if raw, ok := p[field.name]; ok && string(raw) != "null" {
			n := idParam(p, field.name)
			*field.target = &n
		}
	}
	return f
}

func (b *resultsBridge) dispatchReport(ctx context.Context, r request, p map[string]json.RawMessage) (any, error) {
	s := b.options.Reports
	if s == nil {
		return nil, failure("ui_report_unavailable")
	}
	if b.active() {
		return nil, failure("ui_busy")
	}
	if r.Method != "report.open" && r.Method != "report.close" {
		if err := s.Require(idParam(p, "report_id")); err != nil {
			return nil, err
		}
	}
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	switch r.Method {
	case "report.close":
		if s.HandleID() == 0 || s.HandleID() != idParam(p, "report_id") {
			return nil, failure("ui_handle_stale")
		}
		if err := s.Close(); err != nil {
			return nil, err
		}
		return record{"closed": true}, nil
	case "report.state":
		return reportRecord(s.View), nil
	case "report.page":
		f := focusParams(p)
		if (f.Section == "pages" && (f.SourceID == "" || f.CandidateID != nil)) ||
			(f.Section != "pages" && f.SourceID != "") ||
			(f.Section != "attempts" && f.CandidateID != nil) ||
			(f.Section == "attempts" && s.View.Legacy && f.CandidateID != nil) {
			return nil, failure("state_invalid_input")
		}
		rows, total, err := s.Page(readCtx, f)
		if err != nil {
			return nil, err
		}
		items := []any{}
		for _, raw := range rows {
			item, err := reportRow(f.Section, raw)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		items, next, err := boundedItems(items, f.Offset, total)
		return record{
			"report_id": decimal(s.View.ID), "section": f.Section, "source_id": nullable(f.SourceID),
			"candidate_id": optionalID(f.CandidateID), "offset": decimal(f.Offset), "total": decimal(total),
			"items": items, "next_offset": next,
		}, err
	case "report.open", "report.refresh":
		runID := idParam(p, "run_id")
		var focus *app.ReportFocus
		kind := "report_open"
		if r.Method == "report.refresh" {
			kind = "report_refresh"
			runID = s.View.Header.Run.Run.ID
			fields, _ := reportFields(p["focus"], "section source_id candidate_id page_number attempt_id offset")
			f := focusParams(fields)
			if err := s.ValidateFocus(readCtx, f); err != nil {
				return nil, err
			}
			focus = &f
		}
		return b.start(
			ctx, kind, "report", nil,
			func(ctx context.Context, progress app.ResultsProgress, commit app.ResultsCommit) (any, error) {
				if err := s.Open(ctx, runID, focus, progress, commit); err != nil {
					return nil, err
				}
				return record{"type": "report", "value": reportRecord(s.View)}, nil
			},
		), nil
	}
	return nil, failure("ui_protocol_invalid")
}
