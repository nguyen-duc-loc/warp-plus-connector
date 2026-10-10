package scripts_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// covers: AC-2 in docs/reviews/tooling/verify.md.
// Run the real script and gofmt in isolated source trees. In particular,
// gofmt returning success must not let unformatted source pass the gate.
func TestCheckFormat_ReportsProblemsWithoutChangingSource(t *testing.T) {
	t.Parallel()
	script, err := filepath.Abs("check-format.sh")
	require.NoError(t, err)

	const formatted = "package example\n\nfunc example() {}\n"
	const unformatted = "package example\nfunc example(){ }\n"
	for _, tt := range []struct {
		name      string
		files     map[string]string
		wantError bool
		wantFiles []string
	}{
		{
			name: "formatted files pass quietly",
			files: map[string]string{
				"cmd/main.go":         formatted,
				"internal/example.go": formatted,
			},
		},
		{
			name:  "empty source directories pass quietly",
			files: map[string]string{},
		},
		{
			name: "unformatted command fails",
			files: map[string]string{
				"cmd/main.go":         unformatted,
				"internal/example.go": formatted,
			},
			wantError: true,
			wantFiles: []string{"cmd/main.go"},
		},
		{
			name: "nested internal file with spaces fails",
			files: map[string]string{
				"cmd/main.go":                      formatted,
				"internal/nested dir/some file.go": unformatted,
			},
			wantError: true,
			wantFiles: []string{"internal/nested dir/some file.go"},
		},
		{
			name: "all unformatted files are reported",
			files: map[string]string{
				"cmd/main.go":         unformatted,
				"internal/example.go": unformatted,
			},
			wantError: true,
			wantFiles: []string{"cmd/main.go", "internal/example.go"},
		},
		{
			name: "invalid syntax fails even without a formatting result",
			files: map[string]string{
				"cmd/main.go":         formatted,
				"internal/example.go": "package example\nfunc broken(\n",
			},
			wantError: true,
			wantFiles: []string{"internal/example.go"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := t.TempDir()
			for _, dir := range []string{"cmd", "internal"} {
				require.NoError(t, os.MkdirAll(filepath.Join(fixture, dir), 0700))
			}
			for name, contents := range tt.files {
				path := filepath.Join(fixture, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
				require.NoError(t, os.WriteFile(path, []byte(contents), 0600))
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sh", script)
			cmd.Dir = fixture
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()

			require.NoError(t, ctx.Err(), "formatting check timed out")
			if tt.wantError {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit, "stderr = %q", stderr.String())
				assert.NotZero(t, exit.ExitCode())
				for _, name := range tt.wantFiles {
					assert.Contains(t, stderr.String(), name)
				}
			} else {
				require.NoError(t, err, "stderr = %q", stderr.String())
				assert.Empty(t, stderr.String())
			}
			assert.Empty(t, stdout.String(), "diagnostics belong on stderr")
			for name, want := range tt.files {
				got, err := os.ReadFile(filepath.Join(fixture, name))
				require.NoError(t, err)
				assert.Equal(t, want, string(got), "source changed: %s", name)
			}
		})
	}
}
