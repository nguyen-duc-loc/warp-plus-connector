package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// GetCandidate returns current evidence and discovery times at an explicit evaluation.
func (r *Reader) GetCandidate(ctx context.Context, id int64, e state.Evaluation) (state.Candidate, error) {
	return transact(
		ctx, r, false, "get candidate", func(tx *sql.Tx) (state.Candidate, error) {
			e, err := normalizeEvaluation(e)
			if err != nil || id <= 0 {
				return state.Candidate{}, ErrInvalidInput
			}
			return getCandidate(ctx, tx, id, e)
		},
	)
}

func getCandidate(ctx context.Context, tx *sql.Tx, id int64, e state.Evaluation) (state.Candidate, error) {
	c := state.Candidate{LatestOutcome: state.Unchecked, Freshness: state.NotApplicable}
	var first, last int64
	err := tx.QueryRowContext(
		ctx, `SELECT c.id,c.key_text,min(o.observed_at),max(o.observed_at)
 FROM candidates c JOIN sightings s ON s.candidate_id=c.id
 JOIN crawl_observations o ON o.sighting_id=s.id WHERE c.id=? GROUP BY c.id`, id,
	).Scan(&c.ID, &c.Key, &first, &last)
	if err != nil {
		return c, err
	}
	c.FirstSeenAt, c.LastSeenAt = time.UnixMicro(first).UTC(), time.UnixMicro(last).UTC()
	for _, selection := range []struct {
		predicate string
		dest      **state.Attempt
	}{
		{"outcome IS NOT NULL", &c.LatestAttempt},
		{"outcome IN ('confirmed','invalid')", &c.LastDefinitiveAttempt},
		{"outcome IS NULL", &c.UnresolvedAttempt},
	} {
		a, err := scanAttempt(
			tx.QueryRowContext(
				ctx,
				"SELECT "+attemptColumns+" FROM validation_attempts WHERE candidate_id=? AND "+
					selection.predicate+" ORDER BY id DESC LIMIT 1",
				id,
			),
		)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return c, err
		}
		*selection.dest = &a
	}
	if c.LatestAttempt != nil {
		c.LatestOutcome = c.LatestAttempt.Outcome
	}
	if a := c.LastDefinitiveAttempt; a != nil && a.Outcome == state.Confirmed {
		if a.Evidence == nil {
			return c, ErrStateAccess
		}
		observed := a.Evidence.ObservedAt
		switch {
		case observed.After(e.AsOf):
			c.Freshness, c.ClockAnomaly = state.Unknown, true
		case e.AsOf.Sub(observed) >= e.MaxAge:
			c.Freshness = state.Stale
		default:
			c.Freshness = state.Fresh
		}
	}
	return c, nil
}

type collection struct {
	name       string
	table      string
	idColumn   string
	where      string
	args       []any
	parent     int64
	filter     string
	evaluation state.Evaluation
}

func page[T any](
	ctx context.Context,
	tx *sql.Tx,
	request state.PageRequest,
	c collection,
	read func(int64) (T, error),
) (state.Page[T], error) {
	out := state.Page[T]{Items: []T{}}
	size := request.Size
	if size == 0 {
		size = 100
	}
	if size < 1 || size > 1000 {
		return out, ErrInvalidInput
	}
	cursor := state.Cursor{
		Version: 1, Collection: c.name, ParentID: c.parent, Filter: c.filter, Evaluation: c.evaluation,
	}
	if request.Cursor != nil {
		given := *request.Cursor
		if given.Version != 1 || given.Collection != c.name || given.ParentID != c.parent ||
			given.Filter != c.filter || given.LastID <= 0 || given.HighWaterID <= given.LastID ||
			!given.Evaluation.AsOf.Equal(c.evaluation.AsOf) || given.Evaluation.MaxAge != c.evaluation.MaxAge {
			return out, ErrInvalidCursor
		}
		cursor = given
	} else {
		if err := tx.QueryRowContext(
			ctx,
			"SELECT coalesce(max("+c.idColumn+"),0) FROM "+c.table+" WHERE "+c.where,
			c.args...,
		).Scan(&cursor.HighWaterID); err != nil {
			return out, err
		}
	}
	args := append([]any{}, c.args...)
	args = append(args, cursor.LastID, cursor.HighWaterID, size+1)
	rows, err := tx.QueryContext(
		ctx,
		"SELECT "+c.idColumn+" FROM "+c.table+" WHERE "+c.where+" AND "+c.idColumn+">? AND "+
			c.idColumn+"<=? ORDER BY "+c.idColumn+" LIMIT ?",
		args...,
	)
	if err != nil {
		return out, err
	}
	ids := make([]int64, 0, size+1)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return out, err
	}
	more := len(ids) > size
	if more {
		ids = ids[:size]
	}
	for _, id := range ids {
		item, err := read(id)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	if more {
		cursor.LastID = ids[len(ids)-1]
		out.Next = &cursor
	}
	return out, nil
}

// ListCandidates captures membership on the first page and reads current details
// on each later page. Evaluation must remain fixed throughout traversal.
func (r *Reader) ListCandidates(
	ctx context.Context,
	p state.PageRequest,
	e state.Evaluation,
) (state.Page[state.Candidate], error) {
	p = copyPageRequest(p)
	return transact(
		ctx, r, false, "list candidates", func(tx *sql.Tx) (state.Page[state.Candidate], error) {
			e, err := normalizeEvaluation(e)
			if err != nil {
				return state.Page[state.Candidate]{}, err
			}
			c := collection{name: "candidates", table: "candidates", idColumn: "id", where: "1=1", evaluation: e}
			return page(ctx, tx, p, c, func(id int64) (state.Candidate, error) { return getCandidate(ctx, tx, id, e) })
		},
	)
}

// ListRuns lists execution records, optionally restricted to a run kind.
func (r *Reader) ListRuns(ctx context.Context, kind state.RunKind, p state.PageRequest) (state.Page[state.Run], error) {
	p = copyPageRequest(p)
	return transact(
		ctx, r, false, "list runs", func(tx *sql.Tx) (state.Page[state.Run], error) {
			c := collection{name: "runs", table: "runs", idColumn: "id", where: "1=1", filter: string(kind)}
			if kind != "" {
				if kind != state.Crawl && kind != state.Validation {
					return state.Page[state.Run]{}, ErrInvalidInput
				}
				c.where, c.args = "kind=?", []any{kind}
			}
			return page(ctx, tx, p, c, func(id int64) (state.Run, error) { return getRun(ctx, tx, id) })
		},
	)
}

// ListSightings returns all provenance for a candidate through bounded pages.
func (r *Reader) ListSightings(ctx context.Context, candidateID int64, p state.PageRequest) (
	state.Page[state.Sighting],
	error,
) {
	p = copyPageRequest(p)
	return transact(
		ctx, r, false, "list sightings", func(tx *sql.Tx) (state.Page[state.Sighting], error) {
			if err := requireCandidate(ctx, tx, candidateID); err != nil {
				return state.Page[state.Sighting]{}, err
			}
			c := collection{
				name: "sightings", table: "sightings", idColumn: "id", where: "candidate_id=?",
				args: []any{candidateID}, parent: candidateID,
			}
			return page(
				ctx, tx, p, c, func(id int64) (state.Sighting, error) {
					var s state.Sighting
					var published sql.NullInt64
					var messageURL sql.NullString
					var first, last int64
					err := tx.QueryRowContext(
						ctx,
						"SELECT s.id,s.candidate_id,s.source_id,s.message_id,s.message_url,"+
							"s.published_at,min(o.observed_at),max(o.observed_at)\n    FROM "+
							"sightings s JOIN crawl_observations o ON o.sighting_id=s.id WHERE "+
							"s.id=? GROUP BY s.id",
						id,
					).Scan(&s.ID, &s.CandidateID, &s.SourceID, &s.MessageID, &messageURL, &published, &first, &last)
					s.MessageURL, s.PublishedAt = nullString(messageURL), nullTime(published)
					s.FirstSeenAt, s.LastSeenAt = time.UnixMicro(first).UTC(), time.UnixMicro(last).UTC()
					return s, err
				},
			)
		},
	)
}

// ListAttempts selects the complete attempt history of one candidate or run.
func (r *Reader) ListAttempts(
	ctx context.Context,
	f state.AttemptFilter,
	p state.PageRequest,
) (state.Page[state.Attempt], error) {
	p = copyPageRequest(p)
	return transact(
		ctx, r, false, "list attempts", func(tx *sql.Tx) (state.Page[state.Attempt], error) {
			var zero state.Page[state.Attempt]
			if f.CandidateID < 0 || f.RunID < 0 || (f.CandidateID == 0) == (f.RunID == 0) {
				return zero, ErrInvalidInput
			}
			c := collection{name: "attempts", table: "validation_attempts", idColumn: "id"}
			if f.CandidateID != 0 {
				if err := requireCandidate(ctx, tx, f.CandidateID); err != nil {
					return zero, err
				}
				c.where, c.args, c.parent, c.filter = "candidate_id=?", []any{f.CandidateID}, f.CandidateID, "candidate"
			} else {
				run, err := getRun(ctx, tx, f.RunID)
				if err != nil {
					return zero, err
				}
				if run.Kind != state.Validation {
					return zero, ErrInvalidInput
				}
				c.where, c.args, c.parent, c.filter = "run_id=?", []any{f.RunID}, f.RunID, "run"
			}
			return page(ctx, tx, p, c, func(id int64) (state.Attempt, error) { return getAttempt(ctx, tx, id) })
		},
	)
}

// ListObservations requires a terminal crawl run so membership cannot change.
func (r *Reader) ListObservations(ctx context.Context, runID int64, p state.PageRequest) (
	state.Page[state.Observation],
	error,
) {
	p = copyPageRequest(p)
	return transact(
		ctx, r, false, "list observations", func(tx *sql.Tx) (state.Page[state.Observation], error) {
			var zero state.Page[state.Observation]
			if runID <= 0 {
				return zero, ErrInvalidInput
			}
			run, err := getRun(ctx, tx, runID)
			if err != nil {
				return zero, err
			}
			if run.Kind != state.Crawl {
				return zero, ErrInvalidInput
			}
			if run.Status == state.Running {
				return zero, ErrRunActive
			}
			c := collection{
				name: "observations", table: "crawl_observations", idColumn: "sighting_id", where: "run_id=?",
				args: []any{runID}, parent: runID,
			}
			return page(
				ctx, tx, p, c, func(id int64) (state.Observation, error) {
					o := state.Observation{RunID: runID, SightingID: id}
					var observed int64
					err := tx.QueryRowContext(
						ctx,
						"SELECT observed_at FROM crawl_observations WHERE run_id=? AND sighting_id=?",
						runID,
						id,
					).Scan(&observed)
					o.ObservedAt = time.UnixMicro(observed).UTC()
					return o, err
				},
			)
		},
	)
}
