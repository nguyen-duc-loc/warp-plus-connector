package terminal

import (
	"encoding/json"
	"strings"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

func policyRecord(p state.CheckPolicy) any {
	b, _ := json.Marshal(p)
	var values map[string]int64
	_ = json.Unmarshal(b, &values)
	out := record{}
	for k, v := range values {
		if strings.HasSuffix(k, "_us") {
			k = strings.TrimSuffix(k, "_us") + "_ns"
			v *= 1000
		}
		out[k] = decimal(v)
	}
	return out
}

func workOptions(s config.Settings) any {
	sources := []any{}
	for _, v := range s.Sources {
		sources = append(
			sources,
			record{"channel": v.Channel, "enabled": v.Enabled, "page_limit": decimal(int64(v.PageLimit))},
		)
	}
	var image, code any
	if s.Validator == "docker" {
		image = displayText(s.ValidatorImage)
	} else {
		code = "validation_unsupported"
	}
	return record{
		"sources": sources, "validator": s.Validator, "image": image,
		"check_policy":    policyRecord(s.CheckPolicy),
		"check_available": s.Validator == "docker", "check_error_code": code,
	}
}

func workReviewRecord(r *app.WorkReview, settings config.Settings) any {
	collection := "existing"
	if r.Binding.Absent {
		collection = "absent"
	}
	out := record{
		"review_id": decimal(r.ID), "kind": r.Kind, "created_at": stamp(r.CreatedAt),
		"collection_state": collection, "sources": nil, "page_budget": nil, "selection": nil,
		"candidate_id": nil, "selected_count": nil, "view_id": nil, "as_of": nil, "filters": nil,
		"policy": nil, "image": nil,
	}
	if r.Kind == "crawl" {
		plans := []any{}
		var budget int64
		for _, p := range r.Sources {
			budget += int64(p.PageLimit)
			plans = append(plans, record{"channel": p.Channel, "page_limit": decimal(int64(p.PageLimit))})
		}
		out["sources"], out["page_budget"] = plans, decimal(budget)
	} else {
		out["selection"], out["candidate_id"] = r.Selection, optionalID(r.CandidateID)
		out["selected_count"], out["view_id"] = decimal(int64(len(r.Candidates))), decimal(r.View.ID)
		out["as_of"], out["filters"] = stamp(r.View.Evaluation.AsOf), r.View.Filters
		out["policy"], out["image"] = policyRecord(r.Policy), displayText(settings.ValidatorImage)
	}
	return out
}

func workRecord(v app.WorkState) any {
	var runID, stop, crawl, check any
	if v.RunID != 0 {
		runID = decimal(v.RunID)
	}
	if v.StopElapsed != nil {
		stop = decimal(int64(*v.StopElapsed))
	}
	if c := v.Crawl; c != nil {
		var completed, failed, skipped, budget, requested, pages int64
		for _, s := range c.Sources {
			budget += int64(s.PageLimit)
			switch s.Status {
			case "completed":
				completed++
			case "failed":
				failed++
			case "skipped":
				skipped++
			}
			requested += int64(len(s.Pages))
			for _, p := range s.Pages {
				if p.Status == "completed" {
					pages++
				}
			}
		}
		var page any
		if v.CurrentPage > 0 {
			page = decimal(int64(v.CurrentPage))
		}
		crawl = record{
			"selected_sources": decimal(int64(len(c.Sources))), "completed_sources": decimal(completed),
			"failed_sources": decimal(failed), "skipped_sources": decimal(skipped), "page_budget": decimal(budget),
			"requested_pages": decimal(requested), "completed_pages": decimal(pages),
			"saved_candidates": decimal(c.SavedCandidates), "saved_sightings": decimal(c.SavedSightings),
			"current_channel": nullable(v.CurrentChannel), "current_page": page,
		}
	}
	if c := v.Check; c != nil {
		workers := []any{}
		for _, w := range v.Workers {
			workers = append(
				workers, record{
					"candidate_id":   decimal(w.CandidateID),
					"attempt_number": decimal(int64(w.AttemptNumber)), "phase": w.Phase,
				},
			)
		}
		var wait any
		if v.NextRetry != nil {
			wait = decimal(int64(*v.NextRetry))
		}
		counts := runRecord(state.OperationalRun{Run: c.Run, Totals: &c.Totals}).(record)["validation_counts"]
		check = record{
			"totals": counts, "attempts_started": decimal(int64(v.AttemptsStarted)),
			"attempts_completed": decimal(int64(v.AttemptsCompleted)), "retry_waiting": decimal(int64(v.RetryWaiting)),
			"next_retry_wait_ns": wait, "policy": policyRecord(*c.Policy), "workers": workers,
		}
	}
	action := "none"
	switch v.ErrorCode {
	case "state_busy":
		action = "inspect_owner"
	case "validation_unsupported":
		action = "configure_validator"
	case "validator_probe_required":
		action = "review_single_candidate"
	case "ui_collection_changed", "ui_candidate_unavailable":
		action = "retry_review"
	default:
		if v.ErrorCode != "" && v.RunID != 0 {
			action = "inspect_report"
		}
	}
	if v.Guidance != "" {
		action = v.Guidance
	}
	if v.Persistence == "uncertain" {
		action = "restart_session"
	}
	if v.Cleanup == "unresolved" {
		action = "recover_validator"
	}
	return record{
		"operation_id": decimal(v.ID), "revision": decimal(v.Revision), "kind": v.Kind,
		"state": v.State, "phase": v.Phase, "observed_at": stamp(v.ObservedAt), "elapsed_ns": decimal(int64(v.Elapsed)),
		"stop_elapsed_ns": stop,
		"run_id":          runID, "run_created": v.RunCreated, "run_status": nullable(string(v.RunStatus)),
		"cancellation_requested": v.CancellationRequested, "persistence": v.Persistence, "cleanup": v.Cleanup,
		"error_code": nullable(v.ErrorCode), "crawl": crawl, "check": check,
		"guidance": record{
			"action":   action,
			"retry_at": optionalStamp(v.RetryAt), "service_observed_at": optionalStamp(v.ServiceObservedAt),
		},
		"unresolved_cleanup": unresolvedRecord(v.Unresolved),
	}
}

func unresolvedRecord(v *app.WorkState) any {
	if v == nil {
		return nil
	}
	var id any
	if v.RunID != 0 {
		id = decimal(v.RunID)
	}
	code := v.ErrorCode
	if code == "" {
		code = "validation_cleanup_pending"
	}
	return record{"operation_id": decimal(v.ID), "run_id": id, "run_created": v.RunCreated, "error_code": code}
}
