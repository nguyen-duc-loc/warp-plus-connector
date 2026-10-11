package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

// ErrCandidateQuery identifies invalid selection before any runtime access.
var ErrCandidateQuery = errors.New("invalid candidate query")

// CandidateQuery selects alternatives within each filter and intersects filters.
// Empty filters are unrestricted. A nil limit includes every matching candidate.
type CandidateQuery struct {
	Status     []string `json:"status"`
	Definitive []string `json:"definitive"`
	Freshness  []string `json:"freshness"`
	Limit      *int64   `json:"-"`
}

// Normalize validates selection and returns owned filters in canonical order.
func (q CandidateQuery) Normalize() (CandidateQuery, error) {
	var err error
	q.Status, err = normalizeFilter(q.Status, []string{"unchecked", "confirmed", "invalid", "inconclusive"})
	if err != nil {
		return CandidateQuery{}, err
	}
	q.Definitive, err = normalizeFilter(q.Definitive, []string{"none", "confirmed", "invalid"})
	if err != nil {
		return CandidateQuery{}, err
	}
	q.Freshness, err = normalizeFilter(q.Freshness, []string{"fresh", "stale", "unknown", "not_applicable"})
	if err != nil {
		return CandidateQuery{}, err
	}
	if q.Limit != nil {
		n := *q.Limit
		if n <= 0 {
			return CandidateQuery{}, ErrCandidateQuery
		}
		q.Limit = &n
	}
	return q, nil
}

func normalizeFilter(raw, order []string) ([]string, error) {
	seen := make(map[string]bool)
	for _, value := range raw {
		for _, token := range strings.Split(value, ",") {
			token = strings.TrimSpace(token)
			if !slices.Contains(order, token) {
				return nil, ErrCandidateQuery
			}
			seen[token] = true
		}
	}
	result := []string{}
	for _, token := range order {
		if seen[token] {
			result = append(result, token)
		}
	}
	return result, nil
}

func (q CandidateQuery) matches(c state.Candidate) bool {
	definitive := "none"
	if c.LastDefinitiveAttempt != nil {
		definitive = string(c.LastDefinitiveAttempt.Outcome)
	}
	return (len(q.Status) == 0 || slices.Contains(q.Status, string(c.LatestOutcome))) &&
		(len(q.Definitive) == 0 || slices.Contains(q.Definitive, definitive)) &&
		(len(q.Freshness) == 0 || slices.Contains(q.Freshness, string(c.Freshness)))
}

// CandidateResult records one fixed evaluation and the actual emitted selection.
type CandidateResult struct {
	Evaluation    state.Evaluation
	Query         CandidateQuery
	ReturnedCount int64
	Truncated     bool
}

// QueryCandidates prepares a selection through a synchronous callback. The reader
// and snapshot are closed before return, including after cancellation or failure.
// Callback errors retain their identity and callbacks must only prepare output.
func (a *App) QueryCandidates(
	ctx context.Context, query CandidateQuery, emit func(state.Candidate) error,
) (result CandidateResult, err error) {
	query, err = query.Normalize()
	if err != nil || emit == nil {
		return result, ErrCandidateQuery
	}
	result.Query = query
	r, err := store.OpenReader(ctx, a.Settings.StateDir)
	if err != nil {
		return result, classified(err)
	}
	defer func() {
		closeErr := r.Close()
		if err == nil {
			err = classified(closeErr)
		}
	}()
	result.Evaluation = a.evaluation()
	// Saved evidence uses microseconds; use that same precision in the metadata.
	result.Evaluation.AsOf = result.Evaluation.AsOf.Truncate(time.Microsecond)
	var callbackErr error
	err = r.VisitCandidates(
		ctx, result.Evaluation, func(c state.Candidate) (bool, error) {
			if !query.matches(c) {
				return true, nil
			}
			if query.Limit != nil && result.ReturnedCount == *query.Limit {
				result.Truncated = true
				return false, nil
			}
			callbackErr = emit(c)
			if callbackErr != nil {
				return false, callbackErr
			}
			result.ReturnedCount++
			return true, nil
		},
	)
	if callbackErr != nil {
		return result, callbackErr
	}
	return result, classified(err)
}
