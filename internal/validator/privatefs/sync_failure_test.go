package privatefs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The replacement is visible even if its directory cannot acknowledge durability.
func TestDirectorySyncFailureLeavesRenamedPayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	require.NoError(t, Write(path, map[string]string{"phase": "old"}))
	err := writeBytes(path, []byte(`{"phase":"new"}`), func(parent string) error {
		require.Equal(t, dir, parent)
		var visible map[string]string
		require.NoError(t, Read(path, &visible))
		require.Equal(t, "new", visible["phase"])
		return ErrAccess
	})
	require.ErrorIs(t, err, ErrAccess)
	var recovered map[string]string
	require.NoError(t, Read(path, &recovered))
	require.Equal(t, "new", recovered["phase"])
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "session.json", entries[0].Name())
}
