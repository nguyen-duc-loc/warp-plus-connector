package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

func countValue(v *int) string {
	if v == nil {
		return "none"
	}
	return strconv.Itoa(*v)
}

func renderPlan(w io.Writer, r state.CrawlReport) error {
	b := strings.Builder{}
	fmt.Fprintf(&b, "run_id=%d status=%s reason=%s counts_current=%t selected_sources=%d saved_candidates=%d saved_sightings=%d\n", r.Run.ID, r.Run.Status, textValue(r.Run.ReasonCode), r.Run.Status == state.Running, len(r.Sources), r.SavedCandidates, r.SavedSightings)
	for _, s := range r.Sources {
		parsed, completed := 0, 0
		var occurrences *int
		var minID, maxID *int64
		more := "unknown"
		for _, p := range s.Pages {
			if p.FetchedAt != nil {
				parsed++
				if occurrences == nil {
					n := 0
					occurrences = &n
				}
				*occurrences += *p.ParsedPostCount
				if p.MinMessageID != nil && (minID == nil || *p.MinMessageID < *minID) {
					minID = p.MinMessageID
				}
				if p.MaxMessageID != nil && (maxID == nil || *p.MaxMessageID > *maxID) {
					maxID = p.MaxMessageID
				}
			}
			if p.Status == "completed" {
				completed++
			}
			more = "unknown"
			if p.FetchedAt != nil {
				more = strconv.FormatBool(p.NextBeforeID != nil)
			}
		}
		fmt.Fprintf(&b, "source=%s channel=%s url=https://t.me/s/%s page_limit=%d status=%s stop_reason=%s counts_current=%t started_at=%s finished_at=%s recovered_at=%s\n", s.SourceID, s.Channel, s.Channel, s.PageLimit, s.Status, textValue(s.StopReason), s.Status == "pending" || s.Status == "running", timeValue(s.StartedAt), timeValue(s.FinishedAt), timeValue(s.RecoveredAt))
		fmt.Fprintf(&b, "pages_requested=%d pages_parsed=%d pages_completed=%d parsed_post_occurrences=%s min_message_id=%s max_message_id=%s saved_candidates=%d saved_sightings=%d more_available=%s history_complete=false\n", len(s.Pages), parsed, completed, countValue(occurrences), idValue(minID), idValue(maxID), s.SavedCandidates, s.SavedSightings, more)
		for _, p := range s.Pages {
			fmt.Fprintf(&b, "page=%d status=%s requested_url=%s before_id=%s requested_at=%s fetched_at=%s finished_at=%s recovered_at=%s next_before_id=%s parsed_post_count=%s min_message_id=%s max_message_id=%s saved_candidates=%d saved_sightings=%d reason=%s\n", p.PageNumber, p.Status, p.RequestedURL, idValue(p.BeforeID), timeValue(&p.RequestedAt), timeValue(p.FetchedAt), timeValue(p.FinishedAt), timeValue(p.RecoveredAt), idValue(p.NextBeforeID), countValue(p.ParsedPostCount), idValue(p.MinMessageID), idValue(p.MaxMessageID), p.SavedCandidates, p.SavedSightings, textValue(p.ReasonCode))
		}
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return &app.Error{Code: "output_error"}
	}
	return nil
}
