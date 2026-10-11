package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

type candidateEncoder struct {
	writer   io.Writer
	format   string
	showKeys bool
	count    int64
}

type candidateJSON struct {
	ID                  int64           `json:"id"`
	FirstSeenAt         time.Time       `json:"first_seen_at"`
	LastSeenAt          time.Time       `json:"last_seen_at"`
	LatestOutcome       state.Outcome   `json:"latest_outcome"`
	LatestReason        *string         `json:"latest_reason"`
	LastDefinitive      *state.Outcome  `json:"last_definitive"`
	EvidenceAt          *time.Time      `json:"evidence_at"`
	Freshness           state.Freshness `json:"freshness"`
	ClockAnomaly        bool            `json:"clock_anomaly"`
	LatestAttemptID     *int64          `json:"latest_attempt_id"`
	DefinitiveAttemptID *int64          `json:"definitive_attempt_id"`
	ActiveAttemptID     *int64          `json:"active_attempt_id"`
	Key                 *string         `json:"key,omitempty"`
}

func summaryJSON(c state.Candidate, showKeys bool) candidateJSON {
	row := candidateJSON{
		ID: c.ID, FirstSeenAt: c.FirstSeenAt.UTC(), LastSeenAt: c.LastSeenAt.UTC(),
		LatestOutcome: c.LatestOutcome, Freshness: c.Freshness, ClockAnomaly: c.ClockAnomaly,
	}
	if a := c.LatestAttempt; a != nil {
		row.LatestReason, row.LatestAttemptID = a.ReasonCode, &a.ID
	}
	if a := c.LastDefinitiveAttempt; a != nil {
		row.LastDefinitive, row.DefinitiveAttemptID = &a.Outcome, &a.ID
		if a.Evidence != nil {
			at := a.Evidence.ObservedAt.UTC()
			row.EvidenceAt = &at
		}
	}
	if a := c.UnresolvedAttempt; a != nil {
		row.ActiveAttemptID = &a.ID
	}
	if showKeys {
		row.Key = &c.Key
	}
	return row
}

func (e *candidateEncoder) start() error {
	if e.format == "json" {
		_, err := io.WriteString(e.writer, "{\"items\":[")
		return err
	}
	return nil
}

func (e *candidateEncoder) item(c state.Candidate) error {
	if err := e.writeItem(c); err != nil {
		if failure, ok := err.(*deliveryError); ok {
			return failure
		}
		return &deliveryError{code: "export_prepare_failed"}
	}
	e.count++
	return nil
}

func (e *candidateEncoder) writeItem(c state.Candidate) error {
	switch e.format {
	case "json":
		if e.showKeys && !utf8.ValidString(c.Key) {
			return &deliveryError{code: "export_prepare_failed"}
		}
		if e.count != 0 {
			if _, err := io.WriteString(e.writer, ","); err != nil {
				return err
			}
		}
		return json.NewEncoder(e.writer).Encode(summaryJSON(c, e.showKeys))
	case "keys":
		if !utf8.ValidString(c.Key) {
			return &deliveryError{code: "export_key_unrepresentable", candidateID: c.ID}
		}
		for _, r := range c.Key {
			if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
				return &deliveryError{code: "export_key_unrepresentable", candidateID: c.ID}
			}
		}
		_, err := io.WriteString(e.writer, c.Key+"\n")
		return err
	default:
		if e.count == 0 {
			header := "ID\tLATEST\tREASON\tLAST_DEFINITIVE\tEVIDENCE_AT\tFRESHNESS\tCLOCK_ANOMALY"
			if e.showKeys {
				header += "\tKEY"
			}
			if _, err := fmt.Fprintln(e.writer, header); err != nil {
				return err
			}
		}
		return renderCandidate(e.writer, c, e.showKeys)
	}
}

func (e *candidateEncoder) finish(result app.CandidateResult) error {
	if e.format == "json" {
		metadata := struct {
			SchemaVersion int                `json:"schema_version"`
			AsOf          time.Time          `json:"as_of"`
			MaxAge        string             `json:"max_age"`
			Filters       app.CandidateQuery `json:"filters"`
			Limit         *int64             `json:"limit"`
			ReturnedCount int64              `json:"returned_count"`
			Truncated     bool               `json:"truncated"`
		}{
			1, result.Evaluation.AsOf, result.Evaluation.MaxAge.String(),
			result.Query, result.Query.Limit, result.ReturnedCount, result.Truncated,
		}
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(e.writer, "],"); err != nil {
			return err
		}
		if _, err := e.writer.Write(encoded[1:]); err != nil {
			return err
		}
		_, err = io.WriteString(e.writer, "\n")
		return err
	}
	if e.format == "text" && e.count == 0 {
		message := "No candidates saved."
		q := result.Query
		if len(q.Status)+len(q.Definitive)+len(q.Freshness) > 0 {
			message = "No matching candidates."
		}
		_, err := fmt.Fprintln(e.writer, message)
		return err
	}
	return nil
}
