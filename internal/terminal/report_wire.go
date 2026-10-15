package terminal

import (
	"encoding/json"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

func reportFocus(f app.ReportFocus) any {
	return record{
		"section": f.Section, "source_id": nullable(f.SourceID), "candidate_id": optionalID(f.CandidateID),
		"page_number": optionalID(f.PageNumber), "attempt_id": optionalID(f.AttemptID), "offset": decimal(f.Offset),
	}
}

func reportRecord(v *app.ReportView) any {
	run := runRecord(v.Header.Run).(record)
	out := record{
		"report_id": decimal(v.ID), "as_of": stamp(v.Header.AsOf), "run": run, "legacy": v.Legacy,
		"source_count": decimal(v.Sources), "page_count": decimal(v.Pages), "item_count": decimal(v.Items),
		"attempt_count": decimal(v.Attempts), "crawl_counts": run["crawl_counts"],
		"validation_counts": run["validation_counts"], "policy": nil, "backend_id": nil, "backend_version": nil,
		"legacy_coverage": nil, "focus": reportFocus(v.Focus), "retained": v.Retained,
		"error_code": nullable(v.ErrorCode),
	}
	if c := v.Header.Check; c != nil {
		if c.Policy != nil {
			out["policy"], out["backend_id"], out["backend_version"] =
				policyRecord(*c.Policy), displayText(c.BackendID), displayText(c.BackendVersion)
		} else {
			out["item_count"] = nil
		}
	}
	if c := v.Header.Crawl; c != nil && c.Coverage != nil {
		l := c.Coverage
		out["legacy_coverage"] = record{
			"source_id": displayText(l.SourceID), "requested_url": displayText(l.RequestedURL),
			"coverage_mode": displayText(l.CoverageMode), "page_limit": decimal(int64(l.PageLimit)),
			"fetched_at": optionalStamp(l.FetchedAt), "parsed_post_count": optionalInt(l.ParsedPostCount),
			"min_message_id": optionalID(l.MinMessageID), "max_message_id": optionalID(l.MaxMessageID),
		}
	}
	return out
}

func reportRow(section string, raw json.RawMessage) (any, error) {
	switch section {
	case "sources":
		var s state.CrawlSource
		if json.Unmarshal(raw, &s) != nil {
			return nil, failure("ui_snapshot_failed")
		}
		return record{
			"source_id": s.SourceID, "channel": displayText(s.Channel), "position": decimal(int64(s.Position)),
			"page_limit": decimal(int64(s.PageLimit)), "status": s.Status, "started_at": optionalStamp(s.StartedAt),
			"finished_at": optionalStamp(s.FinishedAt), "recovered_at": optionalStamp(s.RecoveredAt),
			"stop_reason": s.StopReason, "page_count": decimal(int64(len(s.Pages))),
			"saved_candidates": decimal(s.SavedCandidates), "saved_sightings": decimal(s.SavedSightings),
		}, nil
	case "pages":
		var p state.CrawlPageRecord
		if json.Unmarshal(raw, &p) != nil {
			return nil, failure("ui_snapshot_failed")
		}
		return record{
			"source_id": p.SourceID, "page_number": decimal(int64(p.PageNumber)),
			"requested_url": displayText(p.RequestedURL), "status": p.Status, "requested_at": stamp(p.RequestedAt),
			"before_id": optionalID(p.BeforeID), "fetched_at": optionalStamp(p.FetchedAt),
			"finished_at": optionalStamp(p.FinishedAt), "recovered_at": optionalStamp(p.RecoveredAt),
			"parsed_post_count": optionalInt(p.ParsedPostCount), "min_message_id": optionalID(p.MinMessageID),
			"max_message_id": optionalID(p.MaxMessageID), "next_before_id": optionalID(p.NextBeforeID),
			"reason_code": p.ReasonCode, "saved_candidates": decimal(p.SavedCandidates),
			"saved_sightings": decimal(p.SavedSightings),
		}, nil
	case "items":
		var i state.ValidationItem
		if json.Unmarshal(raw, &i) != nil {
			return nil, failure("ui_snapshot_failed")
		}
		var outcome any
		if i.LatestAttempt != nil {
			outcome = nullable(string(i.LatestAttempt.Outcome))
		}
		return record{
			"candidate_id": decimal(i.CandidateID), "ordinal": decimal(int64(i.Ordinal)),
			"status": i.Status, "stop_reason": nullable(i.StopReason), "started_at": optionalStamp(i.StartedAt),
			"finished_at": optionalStamp(i.FinishedAt), "recovered_at": optionalStamp(i.RecoveredAt),
			"attempt_count": decimal(int64(i.Attempts)), "latest_outcome": outcome,
		}, nil
	case "attempts":
		var a store.DetailAttempt
		if json.Unmarshal(raw, &a) != nil {
			return nil, failure("ui_snapshot_failed")
		}
		return record{"candidate_id": decimal(a.Attempt.CandidateID), "attempt": attemptRecord(a)}, nil
	}
	return nil, failure("state_invalid_input")
}
