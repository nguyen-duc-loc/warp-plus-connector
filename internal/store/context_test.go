package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
)

// covers: AC-7, AC-12.
func TestNativeBusyTimeoutAndCancelledOwnership(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "five_second_bound"
		if cancelled {
			name = "explicit_cancellation"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			w, err := OpenWriter(t.Context(), dir, time.Now)
			require.NoError(t, err)
			defer func() { require.NoError(t, w.Close()) }()
			db, err := openDB(t.Context(), dir, false)
			require.NoError(t, err)
			defer func() { require.NoError(t, db.Close()) }()
			tx, err := db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				time.AfterFunc(50*time.Millisecond, cancel)
			}
			started := time.Now()
			_, err = w.StartRun(ctx, state.RunInput{RequestID: "blocked", Kind: state.Crawl, StartedAt: time.Now()})
			elapsed := time.Since(started)
			if cancelled {
				require.ErrorIs(t, err, context.Canceled)
				require.Less(t, elapsed, 500*time.Millisecond)
				other, err := OpenWriter(t.Context(), dir, time.Now)
				require.ErrorIs(t, err, ErrBusy)
				require.Nil(t, other)
			} else {
				require.ErrorIs(t, err, ErrStateAccess)
				require.GreaterOrEqual(t, elapsed, 4500*time.Millisecond)
				require.Less(t, elapsed, 6*time.Second)
			}
			require.NoError(t, tx.Rollback())
			// Close must join any remaining native work before another writer enters.
			require.NoError(t, w.Close())
			next, err := OpenWriter(t.Context(), dir, time.Now)
			require.NoError(t, err)
			defer func() { require.NoError(t, next.Close()) }()
			runs, err := next.ListRuns(t.Context(), "", state.PageRequest{})
			require.NoError(t, err)
			require.Empty(t, runs.Items, "cancelled or failed work must never commit later")
		})
	}
}
