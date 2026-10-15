package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// Spec 0012 AC-1 and AC-3: an absent review has no filesystem effects, and its
// explicit admission can initialize the exact reviewed directory.
func TestReviewedWriter_InitializesOnlyAfterAdmission(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "collection")
	binding, err := store.BindCollection(dir)
	require.NoError(t, err)
	require.True(t, binding.Absent)
	_, err = os.Stat(filepath.Dir(dir))
	require.ErrorIs(t, err, os.ErrNotExist)
	w, err := store.OpenReviewedWriter(t.Context(), dir,
		func() time.Time { return fixtureTime }, state.DefaultSchedulePolicy(), binding)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	current, err := store.BindCollection(dir)
	require.NoError(t, err)
	require.False(t, current.Absent)
	info, err := os.Stat(filepath.Join(dir, "state.db"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

// Spec 0012 AC-3, AC-12 and AC-15: rejection precedes recovery and leaves the
// replacement collection's running history untouched.
func TestReviewedWriter_RejectsChangedCollectionBeforeRecovery(t *testing.T) {
	for _, mode := range []string{"appeared", "replaced", "alias retargeted"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			original := filepath.Join(base, "original")
			clock := func() time.Time { return fixtureTime }
			if mode != "appeared" {
				w, err := store.OpenWriter(t.Context(), original, clock)
				require.NoError(t, err)
				require.NoError(t, w.Close())
			}
			path := original
			if mode == "alias retargeted" {
				path = filepath.Join(base, "alias")
				require.NoError(t, os.Symlink(original, path))
			}
			binding, err := store.BindCollection(path)
			require.NoError(t, err)
			if mode == "replaced" {
				require.NoError(t, os.Rename(original, original+".old"))
			}
			if mode == "alias retargeted" {
				require.NoError(t, os.Remove(path))
				original = filepath.Join(base, "replacement")
				require.NoError(t, os.Symlink(original, path))
			}
			replacement, err := store.OpenWriter(t.Context(), original, clock)
			require.NoError(t, err)
			run := startRun(t, replacement, state.Crawl)
			require.NoError(t, replacement.Close())
			w, err := store.OpenReviewedWriter(t.Context(), path, clock, state.DefaultSchedulePolicy(), binding)
			if w != nil {
				require.NoError(t, w.Close())
			}
			require.ErrorIs(t, err, store.ErrCollectionChanged)
			r, err := store.OpenReader(t.Context(), original)
			require.NoError(t, err)
			saved, err := r.GetCrawlReport(t.Context(), run.ID)
			require.NoError(t, err)
			require.Equal(t, state.Running, saved.Run.Status)
			require.Nil(t, saved.Run.RecoveredAt)
			require.NoError(t, r.Close())
		})
	}
}

// Spec 0012 AC-3, AC-5 and AC-10: aliases share writer ownership. A valid binding
// permits recovery only after the competing writer releases its resources.
func TestReviewedWriter_AliasSharesOwnershipAndRecovery(t *testing.T) {
	w, dir := openWriter(t)
	run := startRun(t, w, state.Crawl)
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(dir, alias))
	binding, err := store.BindCollection(alias)
	require.NoError(t, err)
	clock := func() time.Time { return fixtureTime.Add(time.Minute) }
	second, err := store.OpenReviewedWriter(t.Context(), alias, clock, state.DefaultSchedulePolicy(), binding)
	require.Nil(t, second)
	require.ErrorIs(t, err, store.ErrBusy)
	saved, err := w.GetCrawlReport(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, state.Running, saved.Run.Status)
	require.NoError(t, w.Close())
	second, err = store.OpenReviewedWriter(t.Context(), alias, clock, state.DefaultSchedulePolicy(), binding)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, second.Close()) })
	saved, err = second.GetCrawlReport(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, state.Interrupted, saved.Run.Status)
	require.Equal(t, clock(), *saved.Run.RecoveredAt)
	require.Nil(t, saved.Run.FinishedAt)
}
