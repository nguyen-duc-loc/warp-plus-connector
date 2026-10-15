package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// BrowseStore owns disposable private records. Callers serialize operations and
// wait for entered calls before Close. No live application tables are mutated.
type BrowseStore struct {
	db        *sql.DB
	Directory string
}

// BrowseRow is an exact saved summary with its position and session mark.
type BrowseRow struct {
	Candidate state.Candidate
	Ordinal   int64
	Marked    bool
}

// ErrBrowseCommit means a temporary transaction's commit outcome is unobserved.
var ErrBrowseCommit = errors.New("temporary commit outcome unknown")

// NewBrowseStore creates a private disk snapshot outside application state.
func NewBrowseStore(ctx context.Context) (*BrowseStore, error) {
	dir, err := os.MkdirTemp("", "wpc-results-")
	if err != nil {
		return nil, err
	}
	s := &BrowseStore{Directory: dir}
	path := filepath.Join(dir, "snapshot.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		err = f.Close()
	}
	if err == nil {
		u := url.URL{Scheme: "file", Path: path}
		u.RawQuery = "mode=rw&_pragma=foreign_keys(1)&_pragma=secure_delete(1)"
		s.db, err = sql.Open("sqlite", u.String())
	}
	if err == nil {
		s.db.SetMaxOpenConns(1)
		_, err = s.db.ExecContext(
			ctx, `
CREATE TABLE snapshots(id INTEGER PRIMARY KEY,as_of INTEGER NOT NULL,max_age INTEGER NOT NULL,
 state_dir TEXT NOT NULL,device TEXT NOT NULL,inode TEXT NOT NULL,complete INTEGER NOT NULL);
CREATE TABLE candidates(id INTEGER PRIMARY KEY, key_bytes BLOB NOT NULL, data BLOB NOT NULL);
CREATE TABLE views(id INTEGER PRIMARY KEY,filters BLOB,matching_count INTEGER NOT NULL DEFAULT 0,
 marked_count INTEGER NOT NULL DEFAULT 0,revision INTEGER NOT NULL DEFAULT 0,
 retained INTEGER NOT NULL DEFAULT 0,error_code TEXT);
CREATE TABLE view_rows(view_id INTEGER NOT NULL REFERENCES views(id) ON DELETE CASCADE,
 ordinal INTEGER NOT NULL, candidate_id INTEGER NOT NULL REFERENCES candidates(id), marked INTEGER NOT NULL,
 PRIMARY KEY(view_id,ordinal), UNIQUE(view_id,candidate_id));
CREATE INDEX marks ON view_rows(view_id,marked,candidate_id);
CREATE TABLE details(id INTEGER PRIMARY KEY,data BLOB NOT NULL);
CREATE TABLE history(detail_id INTEGER NOT NULL, section TEXT NOT NULL, ordinal INTEGER NOT NULL,
 data BLOB NOT NULL, PRIMARY KEY(detail_id,section,ordinal));`,
		)
	}
	if err != nil {
		if cleanupErr := s.Close(); cleanupErr != nil {
			return s, errors.Join(err, cleanupErr)
		}
		return nil, err
	}
	return s, nil
}

// Close waits for SQL users, then removes only this store's private directory.
func (s *BrowseStore) Close() error {
	if s == nil {
		return nil
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			return err
		}
		s.db = nil
	}
	return os.RemoveAll(s.Directory)
}

// Add saves a summary and preserves even non UTF8 key bytes independently of JSON.
func (s *BrowseStore) Add(ctx context.Context, c state.Candidate) error {
	key := []byte(c.Key)
	c.Key = ""
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO candidates VALUES(?,?,?)", c.ID, key, data)
	return err
}

// SaveSnapshot records the complete traversal and admitted source identity only
// after the live reader has closed. This database contains exactly one snapshot.
func (s *BrowseStore) SaveSnapshot(ctx context.Context, id int64, e state.Evaluation, i CollectionIdentity) error {
	stat, ok := i.File.Sys().(*syscall.Stat_t)
	if !ok {
		return ErrStateAccess
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO snapshots VALUES(?,?,?,?,?,?,1)",
		id, e.AsOf.UnixMicro(), int64(e.MaxAge), i.Directory, fmt.Sprint(stat.Dev), fmt.Sprint(stat.Ino))
	return err
}

// SaveView records canonical filters and derived counts before activation.
func (s *BrowseStore) SaveView(ctx context.Context, id int64, filters [3][]string, count, marked int64) error {
	data, err := json.Marshal(filters)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "UPDATE views SET filters=?,matching_count=?,marked_count=? WHERE id=?",
		data, count, marked, id)
	return err
}

// RetainView saves the safe refresh failure while preserving membership.
func (s *BrowseStore) RetainView(ctx context.Context, id int64, code string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE views SET retained=1,error_code=? WHERE id=?", code, id)
	return err
}

func scanBrowse(row interface{ Scan(...any) error }) (BrowseRow, error) {
	var r BrowseRow
	var key, data []byte
	err := row.Scan(&data, &key, &r.Ordinal, &r.Marked)
	if err == nil {
		err = json.Unmarshal(data, &r.Candidate)
		r.Candidate.Key = string(key)
	}
	return r, err
}

// Candidate finds an identity within one view, or returns ErrNotFound.
func (s *BrowseStore) Candidate(ctx context.Context, view, id int64) (BrowseRow, error) {
	r, err := scanBrowse(
		s.db.QueryRowContext(
			ctx,
			`SELECT c.data,c.key_bytes,v.ordinal,v.marked FROM view_rows v JOIN candidates c ON c.id=v.candidate_id
WHERE v.view_id=? AND c.id=?`, view, id,
		),
	)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}

// BuildView materializes matching ordinals in a transaction. The optional mark
// callback compares old identities without retaining an unbounded ID set.
func (s *BrowseStore) BuildView(
	ctx context.Context, view int64,
	match func(state.Candidate) bool, mark func(state.Candidate) (bool, error),
	progress func(int64),
) (count, marked int64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "INSERT INTO views(id) VALUES(?)", view); err != nil {
		return
	}
	var after int64
	for {
		// Close each bounded reader before writing to this same temporary database.
		rows, e := tx.QueryContext(
			ctx,
			"SELECT data,key_bytes,0,0 FROM candidates WHERE id>? ORDER BY id LIMIT 50",
			after,
		)
		if e != nil {
			err = e
			return
		}
		batch := make([]BrowseRow, 0, 50)
		for rows.Next() {
			r, e := scanBrowse(rows)
			if e != nil {
				err = e
				break
			}
			batch = append(batch, r)
		}
		err = errors.Join(err, rows.Err(), rows.Close())
		if err != nil {
			return
		}
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			after = r.Candidate.ID
			if !match(r.Candidate) {
				continue
			}
			m := false
			if mark != nil {
				m, err = mark(r.Candidate)
				if err != nil {
					return
				}
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO view_rows VALUES(?,?,?,?)", view, count, after, m)
			if err != nil {
				return
			}
			count++
			if m {
				marked++
			}
			progress(count)
		}
	}
	if err = ctx.Err(); err != nil {
		return
	}
	if err = tx.Commit(); err != nil {
		err = errors.Join(ErrBrowseCommit, err)
	}
	return
}

// Page returns at most 50 exact records at the given ordinal.
func (s *BrowseStore) Page(ctx context.Context, view, offset int64, markedOnly bool) ([]BrowseRow, error) {
	query := `SELECT c.data,c.key_bytes,v.ordinal,v.marked FROM view_rows v JOIN candidates c ON c.id=v.candidate_id
WHERE v.view_id=? AND v.ordinal>=?`
	if markedOnly {
		query += " AND v.marked=1"
	}
	rows, err := s.db.QueryContext(ctx, query+" ORDER BY v.ordinal LIMIT 50", view, offset)
	if err != nil {
		return nil, err
	}
	out := make([]BrowseRow, 0, 50)
	for rows.Next() {
		r, e := scanBrowse(rows)
		if e != nil {
			err = e
			break
		}
		out = append(out, r)
	}
	return out, errors.Join(err, rows.Err(), rows.Close())
}

// SetMark sets rather than toggles one membership fact.
func (s *BrowseStore) SetMark(ctx context.Context, view, id int64, marked bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(
		ctx,
		"UPDATE view_rows SET marked=? WHERE view_id=? AND candidate_id=? AND marked<>?",
		marked,
		view,
		id,
		marked,
	)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 0 {
		delta := -1
		if marked {
			delta = 1
		}
		if _, err = tx.ExecContext(ctx, "UPDATE views SET marked_count=marked_count+?,revision=revision+1 WHERE id=?",
			delta, view); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearMarks clears a view in one atomic statement.
func (s *BrowseStore) ClearMarks(ctx context.Context, view int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "UPDATE view_rows SET marked=0 WHERE view_id=?", view); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE views SET marked_count=0,revision=revision+1 WHERE id=?", view); err != nil {
		return err
	}
	return tx.Commit()
}

// DropView releases superseded ordinals.
func (s *BrowseStore) DropView(ctx context.Context, view int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM views WHERE id=?", view)
	return err
}

// Focus returns the retained identity, successor, or last row in a new view.
func (s *BrowseStore) Focus(ctx context.Context, view, id int64, key *string) (*int64, *int64, error) {
	if key != nil {
		r, err := s.Candidate(ctx, view, id)
		if err == nil && r.Candidate.Key == *key {
			return &r.Candidate.ID, &r.Ordinal, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, nil, err
		}
	}
	var candidate, ordinal int64
	err := s.db.QueryRowContext(
		ctx,
		"SELECT candidate_id,ordinal FROM view_rows WHERE view_id=? AND candidate_id>? ORDER BY ordinal LIMIT 1",
		view, id,
	).Scan(&candidate, &ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.db.QueryRowContext(
			ctx,
			"SELECT candidate_id,ordinal FROM view_rows WHERE view_id=? ORDER BY ordinal DESC LIMIT 1",
			view,
		).Scan(&candidate, &ordinal)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &candidate, &ordinal, nil
}

// AddHistory saves one exact typed source or attempt record.
func (s *BrowseStore) AddHistory(ctx context.Context, detail int64, section string, ordinal int64, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO history VALUES(?,?,?,?)", detail, section, ordinal, b)
	return err
}

// History reads a bounded record page for projection by the caller.
func (s *BrowseStore) History(ctx context.Context, detail int64, section string, offset int64) (
	[]json.RawMessage,
	error,
) {
	rows, err := s.db.QueryContext(
		ctx,
		"SELECT data FROM history WHERE detail_id=? AND section=? AND ordinal>=? ORDER BY ordinal LIMIT 50",
		detail, section, offset,
	)
	if err != nil {
		return nil, err
	}
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			break
		}
		out = append(out, json.RawMessage(b))
	}
	return out, errors.Join(err, rows.Err(), rows.Close())
}

// DropDetail discards a closed or partial detail snapshot.
func (s *BrowseStore) DropDetail(ctx context.Context, detail int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM history WHERE detail_id=?", detail)
	if err == nil {
		_, err = s.db.ExecContext(ctx, "DELETE FROM details WHERE id=?", detail)
	}
	return err
}

// SaveDetail retains the exact completed detail header alongside its pages.
func (s *BrowseStore) SaveDetail(ctx context.Context, id, view int64, e state.Evaluation,
	c state.Candidate, sources, attempts int64) error {
	data, err := json.Marshal(struct {
		ViewID            int64
		Evaluation        state.Evaluation
		Summary           state.Candidate
		Sources, Attempts int64
		Complete          bool
	}{view, e, c, sources, attempts, true})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO details VALUES(?,?)", id, data)
	return err
}
