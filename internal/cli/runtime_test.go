package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/cli"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// covers: spec 0003 AC-3, AC-6. Absent state is not an empty collection;
// broken output is reported even when the saved collection is empty.
func TestRuntime_DistinguishesAbsentEmptyAndFailedOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
	stateDir := filepath.Join(dir, "state")
	execute := func(fail bool) (string, error) {
		cmd := cli.NewRootCommand("test", "")
		var out, diagnostic bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&diagnostic)
		if fail {
			cmd.SetOut(failingWriter{err: errors.New("private-secret")})
		}
		cmd.SetArgs([]string{"list", "--config", path, "--state-dir", stateDir})
		err := cmd.ExecuteContext(t.Context())
		return out.String(), err
	}
	out, err := execute(false)
	require.EqualError(t, err, "state_not_initialized")
	require.Empty(t, out)
	_, err = os.Stat(stateDir)
	require.ErrorIs(t, err, os.ErrNotExist)
	w, err := store.OpenWriter(t.Context(), stateDir, time.Now)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	out, err = execute(false)
	require.NoError(t, err)
	require.Equal(t, "No candidates saved.\n", out)
	_, err = execute(true)
	require.Error(t, err)
	require.Equal(t, "command or output failed", cli.Diagnostic(err))
}
