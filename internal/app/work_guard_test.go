package app_test

import (
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/stretchr/testify/require"
)

// Spec 0012 AC-9: admission and the first stop choose one immutable deadline.
func TestOperationGuard_AdmissionAndShutdown(t *testing.T) {
	g := &app.OperationGuard{}
	require.True(t, g.Arm())
	require.False(t, g.Arm())
	g.Stop()
	deadline := g.Deadline()
	require.WithinDuration(t, time.Now().Add(75*time.Second), deadline, time.Second)
	g.Stop()
	require.Equal(t, deadline, g.Deadline())
	require.Equal(t, deadline, g.Shutdown())
	g.Disarm()
	require.Equal(t, deadline, g.Shutdown())
	require.False(t, g.Arm())
}

// A completed cancellation does not lengthen a later idle shutdown.
func TestOperationGuard_ClosedResourcesReturnToIdleDeadline(t *testing.T) {
	g := &app.OperationGuard{}
	require.True(t, g.Arm())
	g.Stop()
	g.Disarm()
	require.True(t, g.Deadline().IsZero())
	deadline := g.Shutdown()
	require.WithinDuration(t, time.Now().Add(5*time.Second), deadline, time.Second)
	require.False(t, g.Arm())
}
