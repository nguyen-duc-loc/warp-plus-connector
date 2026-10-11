package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"time"
)

//go:embed migrations/001_initial.sql
var initialSchema string

//go:embed migrations/002_crawl_coverage.sql
var coverageSchema string

//go:embed migrations/003_crawl_plan.sql
var planSchema string

//go:embed migrations/004_validation_batches.sql
var batchSchema string

//go:embed migrations/005_schedules.sql
var scheduleSchema string

const schemaVersion = 5

// A private argument lets package tests exercise rollback without exposing a
// migration override to application callers.
func migrate(ctx context.Context, db *sql.DB, migrations []string) error {
	return migrateAndRecover(ctx, db, migrations, time.Time{}, state.SchedulePolicy{})
}

func migrateAndRecover(
	ctx context.Context, db *sql.DB, migrations []string,
	at time.Time, policy state.SchedulePolicy,
) error {
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
		if err = tx.QueryRowContext(
			ctx,
			"SELECT count(*) FROM sqlite_schema WHERE substr(name,1,7) != 'sqlite_'",
		).Scan(&count); err != nil {
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
	if !at.IsZero() {
		if err := recoverStateTx(ctx, tx, at); err != nil {
			return err
		}
		if err := initializeSchedules(ctx, tx, at, policy, version < 5); err != nil {
			return err
		}
	}
	return tx.Commit()
}
