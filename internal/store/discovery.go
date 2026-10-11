package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// RecordMessage atomically saves all normalized candidates and their provenance.
// Replays keep saved observation times and return zero insertion counts.
func (w *Writer) RecordMessage(ctx context.Context, in state.MessageInput) (state.MessageResult, error) {
	in.Keys = append([]string(nil), in.Keys...)
	if in.PublishedAt != nil {
		value := *in.PublishedAt
		in.PublishedAt = &value
	}
	if in.MessageURL != nil {
		value := *in.MessageURL
		in.MessageURL = &value
	}
	return transact(
		ctx, &w.Reader, true, "record message", func(tx *sql.Tx) (state.MessageResult, error) {
			exists, err := hasPlan(ctx, tx, in.RunID)
			if err != nil {
				return state.MessageResult{}, err
			}
			if exists {
				return state.MessageResult{}, ErrInvalidInput
			}
			return recordMessage(ctx, tx, in, nil)
		},
	)
}

func recordMessage(
	ctx context.Context,
	tx *sql.Tx,
	in state.MessageInput,
	page *state.CrawlPageID,
) (state.MessageResult, error) {
	result := state.MessageResult{Discoveries: []state.Discovery{}}
	observed, err := normalizeTime(in.ObservedAt)
	if err != nil || in.RunID <= 0 || !validText(in.SourceID) || !validText(in.MessageID) {
		return result, ErrInvalidInput
	}
	published, err := optionalTime(in.PublishedAt)
	if err != nil {
		return result, err
	}
	var messageURL any
	if in.MessageURL != nil {
		if !validURL(*in.MessageURL) {
			return result, ErrInvalidInput
		}
		messageURL = *in.MessageURL
	}
	unique := make(map[string]struct{}, len(in.Keys))
	for _, key := range in.Keys {
		key = strings.TrimSpace(key)
		if !validText(key) {
			return result, ErrInvalidInput
		}
		unique[key] = struct{}{}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	run, err := getRun(ctx, tx, in.RunID)
	if err != nil {
		return result, err
	}
	if run.Kind != state.Crawl {
		return result, ErrInvalidInput
	}
	if run.Status != state.Running {
		// A committed replay must succeed even after completion or recovery. It must
		// not fill metadata or create any new links in this terminal run.
		for _, key := range keys {
			var d state.Discovery
			err = tx.QueryRowContext(
				ctx, `SELECT c.id,s.id FROM candidates c
     JOIN sightings s ON s.candidate_id=c.id
     JOIN crawl_observations o ON o.sighting_id=s.id
     WHERE c.key_text=? AND s.source_id=? AND s.message_id=? AND o.run_id=?`,
				key, in.SourceID, in.MessageID, in.RunID,
			).Scan(&d.CandidateID, &d.SightingID)
			if errors.Is(err, sql.ErrNoRows) {
				return result, ErrRunClosed
			}
			if err != nil {
				return result, err
			}
			result.Discoveries = append(result.Discoveries, d)
		}
		return result, nil
	}
	for _, key := range keys {
		var d state.Discovery
		n, err := insertCount(
			ctx,
			tx,
			"INSERT INTO candidates(key_text) VALUES(?) ON CONFLICT(key_text) DO NOTHING",
			key,
		)
		if err != nil {
			return result, err
		}
		result.NewCandidates += n
		if err = tx.QueryRowContext(
			ctx,
			"SELECT id FROM candidates WHERE key_text=?",
			key,
		).Scan(&d.CandidateID); err != nil {
			return result, err
		}
		n, err = insertCount(
			ctx,
			tx,
			`INSERT INTO sightings(candidate_id,source_id,message_id,message_url,published_at)
    VALUES(?,?,?,?,?) ON CONFLICT(candidate_id,source_id,message_id) DO NOTHING`,
			d.CandidateID,
			in.SourceID,
			in.MessageID,
			messageURL,
			published,
		)
		if err != nil {
			return result, err
		}
		result.NewSightings += n
		if err = tx.QueryRowContext(
			ctx,
			"SELECT id FROM sightings WHERE candidate_id=? AND source_id=? AND message_id=?",
			d.CandidateID,
			in.SourceID,
			in.MessageID,
		).Scan(&d.SightingID); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(
			ctx,
			"UPDATE sightings SET message_url=coalesce(message_url,?),published_at=coalesce(published_at,?) WHERE id=?",
			messageURL,
			published,
			d.SightingID,
		); err != nil {
			return result, err
		}
		n, err = insertCount(
			ctx,
			tx,
			"INSERT INTO crawl_observations(run_id,sighting_id,observed_at) "+
				"VALUES(?,?,?) ON CONFLICT(run_id,sighting_id) DO NOTHING",
			in.RunID,
			d.SightingID,
			observed.UnixMicro(),
		)
		if err != nil {
			return result, err
		}
		if n != 0 && page != nil {
			if _, err := tx.ExecContext(
				ctx,
				"INSERT INTO crawl_page_observations(run_id,sighting_id,source_id,page_number) VALUES(?,?,?,?)",
				page.RunID,
				d.SightingID,
				page.SourceID,
				page.PageNumber,
			); err != nil {
				return result, err
			}
		}
		result.NewObservations += n
		result.Discoveries = append(result.Discoveries, d)
	}
	return result, nil
}

func insertCount(ctx context.Context, tx *sql.Tx, query string, args ...any) (int, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}
