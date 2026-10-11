package app_test

import (
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/stretchr/testify/require"
)

// Spec 0008 AC-6 and AC-14: a failed startup observation stops admission before a source call.
func TestRunObservationFailurePreventsSourceRequest(t *testing.T) {
	a := crawlApp(t)
	scheduledSettings(a)
	a.Settings.CrawlEnabled, a.Settings.ValidationEnabled = true, false
	a.Source = nil // Any source admission would panic instead of quietly passing.
	sentinel := &app.Error{Code: "output_error"}
	seen := []app.RunnerState{}
	a.Observe = func(s app.RunnerState) error { seen = append(seen, s); return sentinel }
	err := a.Run(t.Context(), nil)
	require.ErrorIs(t, err, sentinel)
	require.NotEmpty(t, seen)
	require.Equal(t, "running", seen[0].Phase)
	require.Equal(t, "idle", seen[0].CrawlPhase)
	require.Equal(t, "disabled", seen[0].ValidationPhase)
}
