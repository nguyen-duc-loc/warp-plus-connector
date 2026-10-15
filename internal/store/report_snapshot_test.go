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

// Spec 0012 AC-12: report admission preserves access and replacement error identities.
func TestReportSnapshot_DistinguishesAccessFromReplacement(t *testing.T) {
	w, dir := openWriter(t)
	run := startRun(t, w, state.Crawl)
	require.NoError(t, w.Close())
	r, err := store.OpenReader(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	identity, err := store.IdentifyCollection(dir)
	require.NoError(t, err)
	database := filepath.Join(dir, "state.db")
	read := func() error {
		return r.VisitReport(t.Context(), run.ID, identity, dir, func() time.Time { return fixtureTime },
			func(store.ReportHeader) error { t.Error("failed admission reached the header"); return nil },
			func(string, string, any) error { t.Error("failed admission reached a row"); return nil })
	}
	require.NoError(t, os.Chmod(database, 0644))
	require.ErrorIs(t, read(), store.ErrStateAccess)
	require.NoError(t, os.Chmod(database, 0600))
	require.NoError(t, os.Rename(database, database+".original"))
	require.NoError(t, os.WriteFile(database, []byte("replacement"), 0644))
	require.ErrorIs(t, read(), store.ErrCollectionChanged)
}
