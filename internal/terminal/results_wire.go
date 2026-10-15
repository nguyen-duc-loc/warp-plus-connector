package terminal

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func viewRecord(v *app.ResultsView) any {
	return record{
		"snapshot_id": decimal(v.SnapshotID), "view_id": decimal(v.ID), "as_of": stamp(v.Evaluation.AsOf),
		"max_age_ns": decimal(int64(v.Evaluation.MaxAge)), "filters": v.Filters, "matching_count": decimal(v.Count),
		"marked_count": decimal(v.Marked), "removed_marks": decimal(v.Removed), "focus_id": optionalID(v.FocusID),
		"focus_offset": optionalID(v.FocusOffset), "retained": v.Retained, "error_code": nullable(v.ErrorCode),
	}
}

func summaryRecord(c state.Candidate) any {
	r := record{
		"id": decimal(c.ID), "first_seen_at": stamp(c.FirstSeenAt), "last_seen_at": stamp(c.LastSeenAt),
		"latest_outcome": c.LatestOutcome, "latest_reason": nil, "last_definitive": nil, "evidence_at": nil,
		"freshness": c.Freshness, "clock_anomaly": c.ClockAnomaly, "latest_attempt_id": nil,
		"definitive_attempt_id": nil, "active_attempt_id": nil,
	}
	if a := c.LatestAttempt; a != nil {
		r["latest_reason"] = a.ReasonCode
		r["latest_attempt_id"] = decimal(a.ID)
	}
	if a := c.LastDefinitiveAttempt; a != nil {
		r["last_definitive"] = a.Outcome
		r["definitive_attempt_id"] = decimal(a.ID)
		if a.Evidence != nil {
			r["evidence_at"] = stamp(a.Evidence.ObservedAt)
		}
	}
	if a := c.UnresolvedAttempt; a != nil {
		r["active_attempt_id"] = decimal(a.ID)
	}
	return r
}

func detailRecord(d *app.ResultsDetail) any {
	return record{
		"detail_id": decimal(d.ID), "view_id": decimal(d.ViewID), "candidate_id": decimal(d.CandidateID),
		"as_of": stamp(d.Evaluation.AsOf), "summary": summaryRecord(d.Summary), "source_count": decimal(d.Sources),
		"attempt_count": decimal(d.Attempts),
	}
}

func displayText(s string) any {
	short := len(s) > 4096
	if short {
		s = s[:4096]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[:len(s)-1]
		}
	}
	return record{"text": s, "shortened": short}
}

func sourceRecord(s state.Sighting) any {
	var url any
	if s.MessageURL != nil {
		url = displayText(*s.MessageURL)
	}
	return record{
		"id": decimal(s.ID), "candidate_id": decimal(s.CandidateID), "source_id": displayText(s.SourceID),
		"message_id": displayText(s.MessageID), "message_url": url, "published_at": optionalStamp(s.PublishedAt),
		"first_seen_at": stamp(s.FirstSeenAt), "last_seen_at": stamp(s.LastSeenAt),
	}
}

func attemptRecord(d store.DetailAttempt) any {
	a := d.Attempt
	var evidence, retry any
	if e := a.Evidence; e != nil {
		var account, rejection any
		if e.Kind == state.AccountType {
			account = displayText(e.AccountType)
		} else {
			rejection = displayText(e.RejectionCode)
		}
		evidence = record{
			"kind": e.Kind, "observed_at": stamp(e.ObservedAt), "account_type": account, "rejection_code": rejection,
		}
	}
	if b := d.Retry; b != nil {
		retry = record{
			"attempt_number": decimal(int64(b.Number)), "failure_class": nullable(string(b.FailureClass)),
			"wait_ns": decimal(int64(b.RetryWait)), "eligible_at": optionalStamp(b.RetryEligibleAt),
		}
	}
	return record{
		"id": decimal(a.ID), "candidate_id": decimal(a.CandidateID), "run_id": decimal(a.RunID),
		"backend_id": displayText(a.BackendID), "backend_version": displayText(a.BackendVersion),
		"started_at": stamp(a.StartedAt), "finished_at": optionalStamp(a.FinishedAt),
		"recovered_at": optionalStamp(a.RecoveredAt),
		"outcome":      nullable(string(a.Outcome)), "reason_code": a.ReasonCode, "evidence": evidence,
		"clock_anomaly": a.ClockAnomaly, "retry": retry,
	}
}

func boundedItems(items []any, offset, total int64) ([]any, any, error) {
	size := 0
	count := 0
	for _, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			return nil, nil, failure("ui_snapshot_failed")
		}
		if size+len(b) > 768*1024 {
			break
		}
		size += len(b)
		count++
	}
	if count == 0 && len(items) > 0 {
		return nil, nil, failure("ui_snapshot_failed")
	}
	var next any
	if offset+int64(count) < total {
		next = decimal(offset + int64(count))
	}
	return items[:count], next, nil
}
