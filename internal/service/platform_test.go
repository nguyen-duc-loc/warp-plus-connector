package service

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// AC-1 and AC-18: account resolution refuses unsupported platforms and never
// takes service identity from caller HOME or creates managed files.
func TestAccountPlatformAndHomeAuthority(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_STATE_HOME", fakeHome)
	a, err := ResolveAccount(t.Context())
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		require.ErrorContains(t, err, "service_platform_unsupported")
		require.Empty(t, a.Root)
	} else {
		require.NoError(t, err)
		require.Equal(t, os.Geteuid(), a.UID)
		require.Equal(t, "systemd", a.Manager)
		require.Equal(t, "warp-plus-connector.service", a.Name)
		require.NotEqual(t, fakeHome, a.Home)
		require.Equal(t, filepath.Join(a.Home, ".local", "state", "warp-plus-connector", "service"), a.Root)
	}
	entries, err := os.ReadDir(fakeHome)
	require.NoError(t, err)
	require.Empty(t, entries)
}
