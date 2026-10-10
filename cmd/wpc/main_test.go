package main_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Local labels trace to the unnumbered "Output, failure, and value sources"
// section of docs/specs/0001-stack-architecture/index.md.
// AC-1: Root invocation and explicit help show usable help.
// AC-2: Version comes from build metadata, with dev as the local fallback.
// AC-3: Invalid commands fail. README also promises errors on stderr only.
// AC-4: Help and version do not load configuration or create state.
// The architecture's network prohibition still needs an external observation.
func TestCLI_ExposesHelpVersionAndErrors(t *testing.T) {
	binary := buildCLI(t, "")
	for _, tt := range []struct {
		name string
		args []string
		code int
		out  []string
		err  string
	}{
		{"AC-1 root shows help", nil, 0, []string{"Usage:", "wpc", "version", "--help"}, ""},
		{"AC-1 help flag", []string{"--help"}, 0, []string{"Usage:", "wpc", "version"}, ""},
		{"AC-1 help command", []string{"help"}, 0, []string{"Usage:", "wpc", "version"}, ""},
		{"AC-1 version help", []string{"version", "--help"}, 0, []string{"Usage:", "wpc version"}, ""},
		{"AC-2 local version", []string{"version"}, 0, []string{"wpc dev\n"}, ""},
		{"AC-3 unknown command", []string{"unknown-command"}, 1, nil, "unknown command"},
		{"AC-3 extra version argument", []string{"version", "extra"}, 1, nil, "unknown command"},
		{"AC-3 invalid root flag", []string{"--not-a-flag"}, 1, nil, "unknown flag"},
		{"AC-3 invalid version flag", []string{"version", "--not-a-flag"}, 1, nil, "unknown flag"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, binary, tt.args...)
			require.Equal(t, tt.code, code, "stderr = %q", stderr)
			if tt.err != "" {
				if stdout != "" || !strings.HasPrefix(stderr, "Error: ") || !strings.Contains(stderr, tt.err) || strings.Contains(stderr, "Usage:") {
					t.Fatalf("want error only on stderr; stdout = %q, stderr = %q", stdout, stderr)
				}
				return
			}
			assert.Empty(t, stderr)
			for _, want := range tt.out {
				assert.Contains(t, stdout, want)
			}
			if len(tt.args) == 1 && tt.args[0] == "version" && stdout != "wpc dev\n" {
				t.Errorf("version = %q, want exactly wpc dev with a newline", stdout)
			}
		})
	}
}

// covers: AC-2, including the real Go linker to executable boundary.
func TestCLI_ReportsReleaseMetadata(t *testing.T) {
	for _, tt := range []struct{ name, flags, want string }{
		{"version alone", "-X main.version=1.2.3", "wpc 1.2.3\n"},
		{"version and commit", "-X main.version=1.2.3 -X main.commit=abc1234", "wpc 1.2.3 (abc1234)\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			binary := buildCLI(t, tt.flags)
			stdout, stderr, code := runCLI(t, binary, "version")
			require.Zero(t, code, "stderr = %q", stderr)
			assert.Equal(t, tt.want, stdout)
			assert.Empty(t, stderr)
		})
	}
}

func buildCLI(t *testing.T, flags string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "wpc")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	args := []string{"build", "-o", binary}
	if flags != "" {
		args = append(args, "-ldflags", flags)
	}
	args = append(args, ".")
	cmd := exec.CommandContext(ctx, "go", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return binary
}

func runCLI(t *testing.T, binary string, args ...string) (string, string, int) {
	t.Helper()
	home := t.TempDir()
	config := filepath.Join(home, "config")
	// covers: AC-4. Malformed files at both platform defaults must not affect
	// help or version. Snapshot the sandbox to detect state creation or edits.
	for _, base := range []string{config, filepath.Join(home, "Library", "Application Support")} {
		dir := filepath.Join(base, "warp-plus-connector")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("invalid json"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot(t, home)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+config, "XDG_STATE_HOME="+filepath.Join(home, "state"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI did not finish: %v", ctx.Err())
	}
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("start CLI: %v", err)
		}
		code = exit.ExitCode()
	}
	assert.Equal(t, before, snapshot(t, home), "AC-4: CLI changed configuration or state")
	return stdout.String(), stderr.String(), code
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[path] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
