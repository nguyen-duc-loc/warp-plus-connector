package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// CollectionIdentity binds a saved snapshot to its admitted live database file.
type CollectionIdentity struct {
	Directory string
	File      os.FileInfo
}

// IdentifyCollection performs existing private path admission without opening a writer.
func IdentifyCollection(path string) (CollectionIdentity, error) {
	dir, err := prepareDir(path, false)
	if err != nil {
		return CollectionIdentity{}, err
	}
	if err = checkFiles(dir); err != nil {
		return CollectionIdentity{}, err
	}
	info, err := os.Stat(filepath.Join(dir, "state.db"))
	if errors.Is(err, os.ErrNotExist) {
		return CollectionIdentity{}, ErrNotInitialized
	}
	if err != nil {
		return CollectionIdentity{}, ErrStateAccess
	}
	return CollectionIdentity{dir, info}, nil
}

// Same reports whether an admitted path still names the captured collection.
func (i CollectionIdentity) Same(path string) bool {
	n, err := IdentifyCollection(path)
	return err == nil && n.Directory == i.Directory && os.SameFile(i.File, n.File)
}

// Check distinguishes a missing or replaced collection from failed private admission.
func (i CollectionIdentity) Check(path string) error {
	if !filepath.IsAbs(path) {
		return ErrInvalidInput
	}
	dir, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrCollectionChanged
	}
	if err != nil {
		return ErrStateAccess
	}
	if dir != i.Directory {
		return ErrCollectionChanged
	}
	info, err := os.Lstat(filepath.Join(dir, "state.db"))
	if errors.Is(err, os.ErrNotExist) {
		return ErrCollectionChanged
	}
	if err != nil {
		return ErrStateAccess
	}
	if i.File == nil || !os.SameFile(i.File, info) {
		return ErrCollectionChanged
	}
	n, err := IdentifyCollection(path)
	if errors.Is(err, ErrNotInitialized) {
		return ErrCollectionChanged
	}
	if err != nil {
		return err
	}
	if n.Directory != i.Directory || !os.SameFile(i.File, n.File) {
		return ErrCollectionChanged
	}
	return nil
}

// DetailAttempt preserves nullable saved retry facts alongside an attempt.
type DetailAttempt struct {
	Attempt state.Attempt
	Retry   *state.BatchAttempt
}

// VisitDetail establishes one live read snapshot before sampling its clock. It
// streams complete source and attempt history synchronously into caller storage.
func (r *Reader) VisitDetail(
	ctx context.Context, id int64, key string,
	identity CollectionIdentity, stateDir string, clock func() time.Time, maxAge time.Duration,
	summary func(state.Candidate, state.Evaluation) error,
	source func(state.Sighting) error, attempt func(DetailAttempt) error,
) error {
	var callbackErr error
	_, err := transactOwned(
		ctx, r, false, "detail snapshot", true, func(tx *sql.Tx) (struct{}, error) {
			var saved string
			if err := tx.QueryRowContext(
				ctx,
				"SELECT key_text FROM candidates WHERE id=?",
				id,
			).Scan(&saved); err != nil {
				return struct{}{}, err
			}
			if saved != key || !identity.Same(stateDir) {
				return struct{}{}, ErrNotFound
			}
			e := state.Evaluation{AsOf: clock().UTC().Truncate(time.Microsecond), MaxAge: maxAge}
			c, err := getCandidate(ctx, tx, id, e)
			if err != nil {
				return struct{}{}, err
			}
			if callbackErr = summary(c, e); callbackErr != nil {
				return struct{}{}, nil
			}
			for _, section := range []string{"sources", "attempts"} {
				table := "sightings"
				if section == "attempts" {
					table = "validation_attempts"
				}
				p := state.PageRequest{Size: 50}
				for {
					batch, err := page(
						ctx, tx, p, collection{
							name: section, table: table, idColumn: "id",
							where: "candidate_id=?", args: []any{id}, parent: id,
						}, func(n int64) (int64, error) { return n, nil },
					)
					if err != nil {
						return struct{}{}, err
					}
					for _, n := range batch.Items {
						if err = ctx.Err(); err != nil {
							return struct{}{}, err
						}
						if section == "sources" {
							s, err := getSighting(ctx, tx, n)
							if err != nil {
								return struct{}{}, err
							}
							callbackErr = source(s)
						} else {
							a, err := getAttempt(ctx, tx, n)
							if err != nil {
								return struct{}{}, err
							}
							d := DetailAttempt{Attempt: a}
							var run, candidate int64
							err = tx.QueryRowContext(
								ctx, "SELECT run_id,candidate_id FROM validation_attempt_details WHERE attempt_id=?",
								n,
							).Scan(&run, &candidate)
							if err == nil {
								if run != a.RunID || candidate != a.CandidateID {
									return struct{}{}, ErrStateAccess
								}
								b, err := getBatchAttempt(ctx, tx, n)
								if err != nil {
									return struct{}{}, err
								}
								d.Retry = &b
							} else if !errors.Is(err, sql.ErrNoRows) {
								return struct{}{}, err
							}
							callbackErr = attempt(d)
						}
						if callbackErr != nil {
							return struct{}{}, nil
						}
					}
					if batch.Next == nil {
						break
					}
					p.Cursor = batch.Next
				}
			}
			return struct{}{}, nil
		},
	)
	if callbackErr != nil {
		return callbackErr
	}
	return err
}
