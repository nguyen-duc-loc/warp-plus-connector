package app_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// Spec 0010 AC-7, AC-9, AC-14: collection failures preserve independent service facts.
func TestReadOverview_CollectionFailuresDoNotHideService(t *testing.T) {
	for _, mode := range []string{"absent", "not a directory", "private permissions"} {
		t.Run(mode, func(t *testing.T) {
			a := crawlApp(t)
			availability, policy, code := "missing", "discard", "state_not_initialized"
			if mode == "not a directory" {
				require.NoError(t, os.WriteFile(a.Settings.StateDir, []byte("synthetic private content"), 0600))
				availability, policy, code = "unavailable", "retain", "state_error"
			}
			if mode == "private permissions" {
				w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
				require.NoError(t, err)
				require.NoError(t, w.Close())
				require.NoError(t, os.Chmod(a.Settings.StateDir, 0755))
				availability, policy, code = "unavailable", "retain", "state_error"
			}
			service := state.OverviewService{
				IdentityState: "verified", CollectionMatch: "different",
				Manager: state.OverviewComponent[state.OverviewManager]{
					Availability: "available", ObservedAt: crawlTime.Add(-time.Minute),
					CachePolicy: "replace", Data: &state.OverviewManager{Loaded: true},
				},
			}
			a.OverviewService = func(context.Context) state.OverviewService { return service }
			out := a.ReadOverview(t.Context())
			require.Equal(t, service, out.Service)
			require.Equal(t, crawlTime, out.HeaderAt)
			require.Equal(t, availability, out.Collection.Availability)
			require.Equal(t, policy, out.Collection.CachePolicy)
			require.NotNil(t, out.Collection.ErrorCode)
			require.Equal(t, code, *out.Collection.ErrorCode)
			require.Nil(t, out.Collection.Data)
			switch mode {
			case "absent":
				_, err := os.Stat(a.Settings.StateDir)
				require.ErrorIs(t, err, os.ErrNotExist)
			case "not a directory":
				data, err := os.ReadFile(a.Settings.StateDir)
				require.NoError(t, err)
				require.Equal(t, "synthetic private content", string(data))
			case "private permissions":
				info, err := os.Stat(a.Settings.StateDir)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0755), info.Mode().Perm())
			}
		})
	}
}

// Spec 0010 AC-6, AC-7, AC-14: a reader preserves active work and writer ownership.
func TestReadOverview_ServiceFailureDoesNotHideCollectionOrRecoverWork(t *testing.T) {
	a := crawlApp(t)
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "private_token", Kind: state.Crawl,
		StartedAt: crawlTime})
	require.NoError(t, err)
	_, err = w.RecordMessage(t.Context(), state.MessageInput{RunID: run.ID, SourceID: "fixture",
		MessageID: "1", ObservedAt: crawlTime, Keys: []string{crawlKey}})
	require.NoError(t, err)
	out := a.ReadOverview(t.Context())
	require.Equal(t, "available", out.Collection.Availability)
	require.NotNil(t, out.Collection.Data)
	require.Equal(t, int64(1), out.Collection.Data.Outcomes.Total)
	require.Equal(t, state.Running, out.Collection.Data.RecentRuns[0].Run.Status)
	require.Equal(t, "unavailable", out.Service.Manager.Availability)
	require.Equal(t, "discard", out.Service.IdentityCachePolicy)
	require.Nil(t, out.Service.Manager.Data)
	second, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.ErrorIs(t, err, store.ErrBusy)
	require.Nil(t, second)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{RunID: run.ID, Status: state.Completed,
		FinishedAt: crawlTime.Add(time.Second)})
	require.NoError(t, err, "the existing writer must still own its active run")
	refreshed := a.ReadOverview(t.Context())
	require.Equal(t, state.Completed, refreshed.Collection.Data.RecentRuns[0].Run.Status)
}

// Spec 0010 AC-9: an initialized empty collection remains available despite service failure.
func TestReadOverview_InitializedEmptyCollection(t *testing.T) {
	a := crawlApp(t)
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	out := a.ReadOverview(t.Context())
	require.Equal(t, "available", out.Collection.Availability)
	require.Equal(t, "replace", out.Collection.CachePolicy)
	require.Nil(t, out.Collection.ErrorCode)
	require.NotNil(t, out.Collection.Data)
	require.Zero(t, out.Collection.Data.Outcomes.Total)
	require.Empty(t, out.Collection.Data.RecentRuns)
}
