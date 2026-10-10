// Package store persists local key state in SQLite with explicit writer ownership.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Stable errors can be inspected with errors.Is. Provider text is never exposed.
var (
	ErrBusy              = errors.New("state writer is busy")
	ErrStateAccess       = errors.New("state access failed")
	ErrSchemaTooNew      = errors.New("state schema is newer than supported")
	ErrMigrationRequired = errors.New("state schema requires migration")
	ErrNotInitialized    = errors.New("state is not initialized")
	ErrInvalidInput      = errors.New("invalid state input")
	ErrConflict          = errors.New("state operation conflicts with saved input")
	ErrRunClosed         = errors.New("run is closed")
	ErrRunActive         = errors.New("run is still active")
	ErrAttemptActive     = errors.New("validation attempt is unresolved")
	ErrNotFound          = errors.New("state record not found")
	ErrInvalidCursor     = errors.New("invalid state cursor")
	ErrClosed            = errors.New("state handle is closed")
)

func safeError(ctx context.Context, op string, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", op, ctx.Err())
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	for _, known := range []error{ErrBusy, ErrStateAccess, ErrSchemaTooNew,
		ErrMigrationRequired, ErrNotInitialized, ErrInvalidInput, ErrConflict,
		ErrRunClosed, ErrRunActive, ErrAttemptActive, ErrNotFound, ErrInvalidCursor, ErrClosed,
		context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return fmt.Errorf("%s: %w", op, known)
		}
	}
	// Do not wrap the raw driver error: even its unwrap chain can contain inputs.
	return fmt.Errorf("%s: %w", op, ErrStateAccess)
}
