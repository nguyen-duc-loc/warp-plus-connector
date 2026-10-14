package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Spec 0010 AC-7 and AC-14: service evidence never depends on its collection being readable.
func TestOverviewServiceDoesNotOpenBoundCollection(t *testing.T) {
	a, m, _ := doctorFilesFixture(t)
	require.NoError(t, os.WriteFile(m.StateDir, []byte("not a collection directory"), 0600))
	out := OverviewStatus(t.Context(), a)
	require.Equal(t, "verified", out.IdentityState)
	require.Equal(t, m.InstallationID, *out.InstallationID)
	require.Equal(t, "available", out.Installation.Availability)
	require.NotNil(t, out.Installation.Data)
	require.Equal(t, "available", out.Runner.Availability)
	CompareOverviewCollection(filepath.Join(a.Home, "other", "absent"), &out)
	require.Equal(t, "different", out.CollectionMatch)
	data, err := os.ReadFile(m.StateDir)
	require.NoError(t, err)
	require.Equal(t, "not a collection directory", string(data))
}

// Spec 0010 AC-9: missing and unreadable identities discard every cached service component.
func TestOverviewServiceIdentityFailureDiscardsComponents(t *testing.T) {
	a, _, _ := doctorFilesFixture(t)
	path := filepath.Join(a.Root, "installation.json")
	require.NoError(t, os.WriteFile(path, []byte("corrupt"), 0600))
	out := OverviewStatus(t.Context(), a)
	require.Equal(t, "unknown", out.IdentityState)
	require.Equal(t, "discard", out.IdentityCachePolicy)
	require.Nil(t, out.InstallationID)
	require.Nil(t, out.BoundStateDir)
	require.Nil(t, out.Installation.Data)
	require.Nil(t, out.Manager.Data)
	require.Nil(t, out.Runner.Data)
	require.NoError(t, os.Remove(path))
	out = OverviewStatus(t.Context(), a)
	require.Equal(t, "missing", out.IdentityState)
	require.Equal(t, "missing", out.Installation.Availability)
}
