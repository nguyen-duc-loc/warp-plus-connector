package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/stretchr/testify/require"
)

// Spec 0012 AC-11, AC-12 and AC-15: cancellation at activation keeps the old
// report and handle. A later successful refresh replaces it exactly once.
func TestReportSession_CancelledActivationRetainsPriorReport(t *testing.T) {
	a := crawlApp(t)
	seedQuerySnapshot(t, a.Settings.StateDir)
	s := a.NewReportSession()
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	progress := func(string, int64) {}
	commit := func(f func() error) error { return f() }
	require.NoError(t, s.Open(t.Context(), 1, nil, progress, commit))
	old := s.View
	a.Clock = func() time.Time { return crawlTime.Add(time.Hour) }
	err := s.Open(t.Context(), 1, &old.Focus, progress,
		func(func() error) error { return context.Canceled })
	require.ErrorIs(t, err, context.Canceled)
	require.Same(t, old, s.View)
	require.Equal(t, old.ID, s.HandleID())
	require.Equal(t, crawlTime, s.View.Header.AsOf)
	require.True(t, s.View.Retained)
	require.NoError(t, s.Require(old.ID))
	require.NoError(t, s.Open(t.Context(), 1, &old.Focus, progress, commit))
	require.NotEqual(t, old.ID, s.View.ID)
	require.False(t, s.View.Retained)
	require.Equal(t, a.Clock(), s.View.Header.AsOf)
	var failure *app.Error
	require.ErrorAs(t, s.Require(old.ID), &failure)
	require.Equal(t, "ui_handle_stale", failure.Code)
}

// Spec 0012 AC-12: access lost after the read retains the complete old snapshot.
func TestReportSession_RetainsFailureAfterSnapshotRead(t *testing.T) {
	a := crawlApp(t)
	seedQuerySnapshot(t, a.Settings.StateDir)
	s := a.NewReportSession()
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	progress := func(string, int64) {}
	commit := func(f func() error) error { return f() }
	require.NoError(t, s.Open(t.Context(), 1, nil, progress, commit))
	old := s.View
	database := filepath.Join(a.Settings.StateDir, "state.db")
	a.Clock = func() time.Time {
		require.NoError(t, os.Chmod(database, 0644))
		return crawlTime.Add(time.Minute)
	}
	t.Cleanup(func() { _ = os.Chmod(database, 0600) })
	err := s.Open(t.Context(), 1, &old.Focus, progress, commit)
	var failure *app.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "state_error", failure.Code)
	require.Same(t, old, s.View)
	require.True(t, s.View.Retained)
	require.Equal(t, "state_error", s.View.ErrorCode)
	require.Equal(t, crawlTime, s.View.Header.AsOf)
	require.NoError(t, s.Require(old.ID))

	// Missing identity still invalidates a retained report.
	require.NoError(t, os.Remove(database))
	require.ErrorAs(t, s.Require(old.ID), &failure)
	require.Equal(t, "ui_collection_changed", failure.Code)
	require.Nil(t, s.View)
}
