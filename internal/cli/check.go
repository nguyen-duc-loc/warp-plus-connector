package cli

import (
	"encoding/json"
	"fmt"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"io"
	"strings"
)

func renderValidationItem(w io.Writer, item state.ValidationItem) error {
	b := strings.Builder{}
	reason := item.StopReason
	if reason == "" {
		reason = "none"
	}
	fmt.Fprintf(
		&b, "candidate_id=%d run_id=%d item_status=%s stop_reason=%s attempts=%d ",
		item.CandidateID, item.RunID, item.Status, reason, item.Attempts,
	)
	fmt.Fprintf(
		&b, "started_at=%s finished_at=%s recovered_at=%s ",
		timeValue(item.StartedAt), timeValue(item.FinishedAt), timeValue(item.RecoveredAt),
	)
	if item.LatestAttempt == nil {
		b.WriteString("attempt_id=none outcome=unchecked\n")
	} else {
		if err := renderAttempt(&b, *item.LatestAttempt); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func renderValidationSummary(w io.Writer, r state.ValidationReport) error {
	t := r.Totals
	_, err := fmt.Fprintf(
		w,
		"run_id=%d status=%s reason=%s selected=%d pending=%d active=%d completed=%d skipped=%d "+
			"interrupted=%d confirmed=%d invalid=%d inconclusive=%d unresolved=%d unstarted=%d\n",
		r.Run.ID,
		r.Run.Status,
		textValue(r.Run.ReasonCode),
		t.Selected,
		t.Pending,
		t.Active,
		t.Completed,
		t.Skipped,
		t.Interrupted,
		t.Confirmed,
		t.Invalid,
		t.Inconclusive,
		t.Unresolved,
		t.Unstarted,
	)
	return err
}

func renderCheckRecord(w io.Writer, r state.CheckRecord) error {
	if r.Header != nil {
		h := r.Header
		if h.Policy == nil {
			_, err := fmt.Fprintf(w, "run_id=%d batch_metadata=unavailable\n", h.Run.ID)
			return err
		}
		policy, err := json.Marshal(h.Policy)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(
			w, "run_id=%d backend=%s version=%s counts_current=%t policy=%s\n",
			h.Run.ID, h.BackendID, h.BackendVersion, h.Run.Status == state.Running, policy,
		)
		return err
	}
	if r.Item != nil {
		return renderValidationItem(w, *r.Item)
	}
	if r.Attempt != nil {
		a := r.Attempt
		class := string(a.FailureClass)
		if class == "" {
			class = "none"
		}
		wait := "none"
		if a.RetryWait > 0 {
			wait = a.RetryWait.String()
		}
		b := strings.Builder{}
		fmt.Fprintf(
			&b, "attempt_number=%d failure_class=%s retry_wait=%s retry_eligible_at=%s ",
			a.Number, class, wait, timeValue(a.RetryEligibleAt),
		)
		if err := renderAttempt(&b, a.Attempt); err != nil {
			return err
		}
		_, err := io.WriteString(w, b.String())
		return err
	}
	return nil
}
