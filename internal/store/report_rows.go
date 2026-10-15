package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// NewReportStore creates private indexed rows outside the live collection.
func NewReportStore(ctx context.Context) (*BrowseStore, error) {
	s, err := NewBrowseStore(ctx)
	if err != nil {
		return s, err
	}
	_, err = s.db.ExecContext(
		ctx, `CREATE TABLE report_rows(
 section TEXT NOT NULL,parent TEXT NOT NULL,ordinal INTEGER NOT NULL,identity TEXT NOT NULL,data BLOB NOT NULL,
 PRIMARY KEY(section,parent,ordinal),UNIQUE(section,parent,identity))`,
	)
	return s, err
}

// AddReport appends an immutable row in section order.
func (s *BrowseStore) AddReport(ctx context.Context, section, parent, identity string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(
		ctx, `INSERT INTO report_rows
 SELECT ?,?,count(*),?,? FROM report_rows WHERE section=? AND parent=?`, section, parent, identity, b, section, parent,
	)
	return err
}

// ReportCount returns the exact saved section count, optionally across all parents.
func (s *BrowseStore) ReportCount(ctx context.Context, section string, parent *string) (int64, error) {
	query := "SELECT count(*) FROM report_rows WHERE section=?"
	args := []any{section}
	if parent != nil {
		query += " AND parent=?"
		args = append(args, *parent)
	}
	var n int64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

// ReportPosition resolves a structural identity within the fixed snapshot.
func (s *BrowseStore) ReportPosition(ctx context.Context, section, parent, identity string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(
		ctx,
		"SELECT ordinal FROM report_rows WHERE section=? AND parent=? AND identity=?", section, parent, identity,
	).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return n, err
}

// ReportPage returns at most 50 saved rows and their structural identities.
func (s *BrowseStore) ReportPage(ctx context.Context, section, parent string, offset int64) (
	[]json.RawMessage, []string, error,
) {
	rows, err := s.db.QueryContext(
		ctx,
		"SELECT data,identity FROM report_rows WHERE section=? AND parent=? AND ordinal>=? ORDER BY ordinal LIMIT 50",
		section, parent, offset,
	)
	if err != nil {
		return nil, nil, err
	}
	values, identities := []json.RawMessage{}, []string{}
	for rows.Next() {
		var b []byte
		var id string
		if err = rows.Scan(&b, &id); err != nil {
			break
		}
		values, identities = append(values, b), append(identities, id)
	}
	return values, identities, errors.Join(err, rows.Err(), rows.Close())
}
