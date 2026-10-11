package scripts_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		name          string
		files         map[string]string
		maxLineLength int
		wantError     bool
		wantFiles     []string
		wantMessage   string
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
		{
			name: "script Go files are included",
			files: map[string]string{
				"scripts/example_test.go": unformatted,
			},
			wantError: true,
			wantFiles: []string{"scripts/example_test.go"},
		},
		{
			name: "gofmt compliant long line fails EditorConfig",
			files: map[string]string{
				"internal/long.go": "package example\n\nvar value = \"" + strings.Repeat("x", 110) + "\"\n",
			},
			wantError:   true,
			wantFiles:   []string{"internal/long.go"},
			wantMessage: "line too long",
		},
		{
			name: "line at configured limit passes",
			files: map[string]string{
				"internal/boundary.go": "package example\n\nvar value = \"" + strings.Repeat("x", 106) + "\"\n",
			},
		},
		{
			name: "limit is read from EditorConfig",
			files: map[string]string{
				"internal/long.go": "package example\n\nvar value = \"" + strings.Repeat("x", 80) + "\"\n",
			},
			maxLineLength: 80,
			wantError:     true,
			wantFiles:     []string{"internal/long.go"},
			wantMessage:   "instead of 80",
		},
		{
			name: "larger configured limit permits longer line",
			files: map[string]string{
				"internal/long.go": "package example\n\nvar value = \"" + strings.Repeat("x", 110) + "\"\n",
			},
			maxLineLength: 160,
		},
		{
			name: "raw literal indentation is data",
			files: map[string]string{
				"internal/raw.go": "package example\n\nvar value = `first\n   second`\n",
			},
		},
	} {
		t.Run(
			tt.name, func(t *testing.T) {
				t.Parallel()
				fixture := t.TempDir()
				for _, dir := range []string{"cmd", "internal", "scripts"} {
					require.NoError(t, os.MkdirAll(filepath.Join(fixture, dir), 0700))
				}
				limit := tt.maxLineLength
				if limit == 0 {
					limit = 120
				}
				config := fmt.Sprintf(
					"root = true\n\n[*.go]\nindent_style = tab\nmax_line_length = %d\n", limit,
				)
				require.NoError(t, os.WriteFile(filepath.Join(fixture, ".editorconfig"), []byte(config), 0600))
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
					if tt.wantMessage != "" {
						assert.Contains(t, stderr.String(), tt.wantMessage)
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
			},
		)
	}
}
