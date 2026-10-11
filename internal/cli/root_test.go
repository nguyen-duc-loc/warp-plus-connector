package cli_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/cli"
)

// Local criterion labels refer to docs/specs/0001-stack-architecture/index.md,
// section "Output, failure, and value sources", which has no numbered checklist.
// AC-2: Version uses supplied metadata, falls back to dev, and omits absent commits.
func TestNewRootCommand_ReportsVersion(t *testing.T) {
	for _, tt := range []struct {
		name, version, commit, want string
	}{
		{"empty version uses dev", "", "", "wpc dev\n"},
		{"release without commit", "1.2.3", "", "wpc 1.2.3\n"},
		{"release with commit", "1.2.3", "abc1234", "wpc 1.2.3 (abc1234)\n"},
		{"commit with fallback version", "", "abc1234", "wpc dev (abc1234)\n"},
	} {
		t.Run(
			tt.name, func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				cmd := cli.NewRootCommand(tt.version, tt.commit)
				cmd.SetOut(&stdout)
				cmd.SetErr(&stderr)
				cmd.SetArgs([]string{"version"})
				require.NoError(t, cmd.Execute())
				assert.Equal(t, tt.want, stdout.String())
				assert.Empty(t, stderr.String())
			},
		)
	}
}

func TestNewRootCommand_ReportsOutputFailure(t *testing.T) {
	want := errors.New("output unavailable")
	cmd := cli.NewRootCommand("dev", "")
	cmd.SetArgs([]string{"version"})
	cmd.SetOut(failingWriter{want})
	require.ErrorIs(t, cmd.Execute(), want)
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
