package privatefs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/stretchr/testify/require"
)

// Recovery state must stay private and have one owner across different profiles.
func TestOwnershipAndPrivateFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	require.NoError(t, privatefs.Directory(dir))
	lock, err := privatefs.Lock(filepath.Join(dir, "owner.lock"))
	require.NoError(t, err)
	_, err = privatefs.Lock(filepath.Join(dir, "owner.lock"))
	require.ErrorIs(t, err, privatefs.ErrBusy)
	require.NoError(t, lock.Close())
	path := filepath.Join(dir, "state.json")
	require.NoError(t, privatefs.Write(path, map[string]string{"phase": "pending"}))
	var value map[string]string
	require.NoError(t, privatefs.Read(path, &value))
	require.Equal(t, "pending", value["phase"])
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0077)
	link := filepath.Join(dir, "link.json")
	require.NoError(t, os.Symlink(path, link))
	require.ErrorIs(t, privatefs.Write(link, true), privatefs.ErrAccess)
	require.ErrorIs(t, privatefs.Read(link, &value), privatefs.ErrAccess)
	require.NoError(t, os.Chmod(path, 0644))
	require.ErrorIs(t, privatefs.Read(path, &value), privatefs.ErrAccess)
}

// covers: spec 0003 AC-7. Damaged journals are rejected without changing bytes.
func TestReadRejectsMalformedAndUnknownJournalFields(t *testing.T) {
	for _, body := range []string{`{`, `{"phase":"prepared","unknown":"secret"}`, `{"phase":"prepared"} {}`} {
		t.Run(body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.json")
			require.NoError(t, os.WriteFile(path, []byte(body), 0600))
			var value struct {
				Phase string `json:"phase"`
			}
			require.ErrorIs(t, privatefs.Read(path, &value), privatefs.ErrAccess)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, body, string(after))
		})
	}
}
