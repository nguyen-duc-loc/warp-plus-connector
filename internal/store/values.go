package store

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

var reasonPattern = regexp.MustCompile("^[a-z][a-z0-9_]{0,63}$")

func copyPageRequest(p state.PageRequest) state.PageRequest {
	if p.Cursor != nil {
		value := *p.Cursor
		p.Cursor = &value
	}
	return p
}

func validText(s string) bool { return s != "" && utf8.ValidString(s) && !strings.ContainsRune(s, 0) }

func normalizeTime(t time.Time) (time.Time, error) {
	if t.IsZero() {
		return time.Time{}, ErrInvalidInput
	}
	n := time.UnixMicro(t.UnixMicro()).UTC()
	// Round trip detects overflow of the signed microsecond representation.
	if !n.Equal(t.UTC().Truncate(time.Microsecond)) || n.IsZero() {
		return time.Time{}, ErrInvalidInput
	}
	return n, nil
}

func normalizeEvaluation(e state.Evaluation) (state.Evaluation, error) {
	t, err := normalizeTime(e.AsOf)
	if err != nil || e.MaxAge <= 0 {
		return state.Evaluation{}, ErrInvalidInput
	}
	e.AsOf = t
	return e, nil
}

func optionalTime(t *time.Time) (any, error) {
	if t == nil {
		return nil, nil
	}
	n, err := normalizeTime(*t)
	if err != nil {
		return nil, err
	}
	return n.UnixMicro(), nil
}

func optionalString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func validURL(s string) bool {
	if !validText(s) {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") &&
		u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery &&
		u.Fragment == "" && !strings.Contains(s, "#") && u.Opaque == ""
}

type evidenceRecord struct {
	Version       int                `json:"version"`
	Kind          state.EvidenceKind `json:"kind"`
	ObservedAt    int64              `json:"observed_at"`
	AccountType   string             `json:"account_type,omitempty"`
	RejectionCode string             `json:"rejection_code,omitempty"`
}

func encodeEvidence(outcome state.Outcome, reason string, e *state.Evidence) (any, error) {
	if outcome == state.Inconclusive {
		if !reasonPattern.MatchString(reason) || e != nil {
			return nil, ErrInvalidInput
		}
		return nil, nil
	}
	if e == nil {
		return nil, ErrInvalidInput
	}
	at, err := normalizeTime(e.ObservedAt)
	if err != nil {
		return nil, err
	}
	switch outcome {
	case state.Confirmed:
		if reason != "" || e.Kind != state.AccountType || e.AccountType != "Unlimited" || e.RejectionCode != "" {
			return nil, ErrInvalidInput
		}
	case state.Invalid:
		if !reasonPattern.MatchString(reason) || e.Kind != state.Rejection ||
			!reasonPattern.MatchString(e.RejectionCode) || e.AccountType != "" {
			return nil, ErrInvalidInput
		}
	default:
		return nil, ErrInvalidInput
	}
	b, err := json.Marshal(evidenceRecord{1, e.Kind, at.UnixMicro(), e.AccountType, e.RejectionCode})
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func decodeEvidence(s string) (*state.Evidence, error) {
	var e evidenceRecord
	if err := json.Unmarshal([]byte(s), &e); err != nil {
		return nil, ErrStateAccess
	}
	if e.Version != 1 {
		return nil, ErrStateAccess
	}
	return &state.Evidence{
		Kind: e.Kind, ObservedAt: time.UnixMicro(e.ObservedAt).UTC(), AccountType: e.AccountType,
		RejectionCode: e.RejectionCode,
	}, nil
}
