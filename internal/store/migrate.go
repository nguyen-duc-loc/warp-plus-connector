package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed migrations/001_initial.sql
var initialSchema string

// A private argument lets package tests exercise rollback without exposing a
// migration override to application callers.
func migrate(ctx context.Context, db *sql.DB, migrations []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return ErrSchemaTooNew
	}
	if version < 0 {
		return ErrStateAccess
	}
	if version == 0 {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE substr(name,1,7) != 'sqlite_'").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrStateAccess
		}
	}
	for _, migration := range migrations[version:] {
		if _, err = tx.ExecContext(ctx, migration); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(migrations))); err != nil {
		return err
	}
	return tx.Commit()
}
