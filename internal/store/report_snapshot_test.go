package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// Spec 0012 AC-11 and AC-12: the transaction exists before sampling the clock.
// Later commits cannot mix a new outcome with the earlier report header or rows.
func TestReportSnapshot_EstablishesSnapshotBeforeClock(t *testing.T) {
	w, dir := openWriter(t)
	crawl := startRun(t, w, state.Crawl)
	discover(t, w, crawl.ID, "private-report-key")
	run := startRun(t, w, state.Validation)
	attempt := beginAttempt(t, w, run.ID, 1, fixtureTime)
	r, err := store.OpenReader(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	identity, err := store.IdentifyCollection(dir)
	require.NoError(t, err)
	var header store.ReportHeader
	var rows []store.DetailAttempt
	var clockCalls int
	observed := fixtureTime.Add(time.Minute)
	err = r.VisitReport(t.Context(), run.ID, identity, dir, func() time.Time {
		clockCalls++
		_, e := w.CompleteAttempt(t.Context(), state.AttemptCompletion{
			AttemptID: attempt.ID, FinishedAt: observed, Outcome: state.Inconclusive, ReasonCode: "validation_timeout",
		})
		require.NoError(t, e)
		_, e = w.FinishRun(t.Context(), state.RunCompletion{
			RunID: run.ID, FinishedAt: observed, Status: state.Completed,
		})
		require.NoError(t, e)
		return observed
	}, func(h store.ReportHeader) error { header = h; return nil },
		func(section, parent string, value any) error {
			require.Equal(t, "attempts", section)
			require.Empty(t, parent, "legacy reports have no invented selection")
			rows = append(rows, value.(store.DetailAttempt))
			return nil
		})
	require.NoError(t, err)
	require.Equal(t, 1, clockCalls)
	require.Equal(t, observed, header.AsOf)
	require.Equal(t, state.Running, header.Run.Run.Status)
	require.Nil(t, header.Check.Policy)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].Attempt.Outcome)
	require.Nil(t, rows[0].Retry)
	current, err := r.GetValidationReport(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, state.Completed, current.Run.Status)
}

// Spec 0012 AC-11 and AC-15: callback failure retains its identity and stops
// traversal; the reader can still serve a later request after cleanup.
func TestReportSnapshot_CallbackFailureStopsTraversal(t *testing.T) {
	for _, failAt := range []string{"header", "row"} {
		t.Run(failAt, func(t *testing.T) {
			w, dir := openWriter(t)
			crawl := startRun(t, w, state.Crawl)
			discover(t, w, crawl.ID, "one", "two")
			run := startRun(t, w, state.Validation)
			beginAttempt(t, w, run.ID, 1, fixtureTime)
			beginAttempt(t, w, run.ID, 2, fixtureTime)
			r, err := store.OpenReader(t.Context(), dir)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, r.Close()) })
			identity, err := store.IdentifyCollection(dir)
			require.NoError(t, err)
			sentinel := errors.New("stop report consumer")
			rows := 0
			err = r.VisitReport(t.Context(), run.ID, identity, dir,
				func() time.Time { return fixtureTime }, func(store.ReportHeader) error {
					if failAt == "header" {
						return sentinel
					}
					return nil
				}, func(string, string, any) error { rows++; return sentinel })
			require.ErrorIs(t, err, sentinel)
			want := 0
			if failAt == "row" {
				want = 1
			}
			require.Equal(t, want, rows)
			_, err = r.GetValidationReport(t.Context(), run.ID)
			require.NoError(t, err)
		})
	}
}

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
