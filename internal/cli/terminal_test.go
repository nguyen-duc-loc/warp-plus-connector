package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/cli"
	"github.com/stretchr/testify/require"
)

// Spec 0010 AC-2, AC-14: redirected root help ignores runtime configuration and assets.
func TestTerminal_RedirectedRootDoesNotResolveConfiguration(t *testing.T) {
	for _, term := range []string{"", "dumb", "xterm-256color"} {
		t.Run(term, func(t *testing.T) {
			t.Setenv("TERM", term)
			root := t.TempDir()
			config := filepath.Join(root, "config.json")
			stateDir := filepath.Join(root, "absent")
			require.NoError(t, os.WriteFile(config, []byte("invalid private configuration"), 0600))
			var stdout, stderr bytes.Buffer
			cmd := cli.NewRootCommand("dev", "")
			cmd.SetIn(strings.NewReader(""))
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"--config", config, "--state-dir", stateDir, "--recheck-interval", "invalid"})
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			require.Contains(t, stdout.String(), "Usage:")
			require.NotContains(t, stdout.String(), "\x1b")
			require.Empty(t, stderr.String())
			_, err := os.Stat(stateDir)
			require.ErrorIs(t, err, os.ErrNotExist)
			data, err := os.ReadFile(config)
			require.NoError(t, err)
			require.Equal(t, "invalid private configuration", string(data))
		})
	}
}
