package main_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
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
		t.Run(
			tt.name, func(t *testing.T) {
				stdout, stderr, code := runCLI(t, binary, tt.args...)
				require.Equal(t, tt.code, code, "stderr = %q", stderr)
				if tt.err != "" {
					if stdout != "" || !strings.HasPrefix(stderr, "Error: ") || !strings.Contains(
						stderr,
						tt.err,
					) || strings.Contains(stderr, "Usage:") {
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
			},
		)
	}
}

// covers: AC-2, including the real Go linker to executable boundary.
func TestCLI_ReportsReleaseMetadata(t *testing.T) {
	for _, tt := range []struct{ name, flags, want string }{
		{"version alone", "-X main.version=1.2.3", "wpc 1.2.3\n"},
		{"version and commit", "-X main.version=1.2.3 -X main.commit=abc1234", "wpc 1.2.3 (abc1234)\n"},
	} {
		t.Run(
			tt.name, func(t *testing.T) {
				binary := buildCLI(t, tt.flags)
				stdout, stderr, code := runCLI(t, binary, "version")
				require.Zero(t, code, "stderr = %q", stderr)
				assert.Equal(t, tt.want, stdout)
				assert.Empty(t, stderr)
			},
		)
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
	cmd.Env = append(
		os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+config,
		"XDG_STATE_HOME="+filepath.Join(home, "state"),
	)
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
	err := filepath.WalkDir(
		root, func(path string, entry fs.DirEntry, err error) error {
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
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// covers: spec 0003 AC-2, AC-3, AC-4, AC-6. Fresh processes read every saved
// candidate and return an explicit failing exit for unsupported validation.
func TestCLI_ListsEveryCandidateAndReadsCheckAfterRestart(t *testing.T) {
	binary := buildCLI(t, "")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{}`), 0600))
	stateDir := filepath.Join(dir, "state")
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	w, err := store.OpenWriter(t.Context(), stateDir, func() time.Time { return at })
	require.NoError(t, err)
	report, err := w.StartCrawl(
		t.Context(),
		state.CrawlInput{
			RequestID: "fixture", StartedAt: at, SourceID: "telegram:warpplus", RequestedURL: "https://t.me/s/warpplus",
			CoverageMode: "latest_preview", PageLimit: 1,
		},
	)
	require.NoError(t, err)
	minID := int64(7)
	_, err = w.RecordCrawlPage(
		t.Context(),
		state.CrawlPage{
			RunID: report.Run.ID, FetchedAt: at, ParsedPostCount: 1, MinMessageID: &minID, MaxMessageID: &minID,
		},
	)
	require.NoError(t, err)
	keys := make([]string, 338)
	for i := range keys {
		keys[i] = fmt.Sprintf("%08d-12345678-AbCdEf01", i)
	}
	_, err = w.RecordMessage(
		t.Context(),
		state.MessageInput{
			RunID: report.Run.ID, SourceID: "telegram:warpplus", MessageID: "7", ObservedAt: at, Keys: keys,
		},
	)
	require.NoError(t, err)
	_, err = w.FinishRun(
		t.Context(),
		state.RunCompletion{RunID: report.Run.ID, Status: state.Completed, FinishedAt: at},
	)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	invoke := func(args ...string) (string, string, int) {
		return runCLI(t, binary, append(args, "--config", configPath, "--state-dir", stateDir)...)
	}
	out, diagnostic, code := invoke("list")
	require.Zero(t, code, diagnostic)
	require.Empty(t, diagnostic)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 339)
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		require.Len(t, fields, 7)
		require.False(t, seen[fields[0]])
		seen[fields[0]] = true
		require.Equal(t, "unchecked", fields[1])
	}
	for i := 1; i <= 338; i++ {
		require.True(t, seen[fmt.Sprint(i)])
	}
	require.NotContains(t, out, "AbCdEf01")
	require.NotContains(t, out, "12345678")
	out, diagnostic, code = invoke("list", "--show-keys")
	require.Zero(t, code, diagnostic)
	for _, key := range keys {
		require.Contains(t, out, key)
	}
	out, diagnostic, code = invoke("crawl", "show", fmt.Sprint(report.Run.ID))
	require.Zero(t, code, diagnostic)
	for _, value := range []string{
		"status=completed", "history_complete=false", "coverage=latest_preview", "parsed_posts=1", "min_message_id=7",
		"saved_candidates=338", "saved_sightings=338",
	} {
		require.Contains(t, out, value)
	}
	out, diagnostic, code = invoke("check", "1")
	require.Equal(t, 1, code)
	require.Contains(t, out, "outcome=inconclusive reason=validation_unsupported")
	require.Contains(t, diagnostic, "Error: validation_batch_inconclusive")
	require.NotContains(t, out+diagnostic, keys[0])
	out, diagnostic, code = invoke("list")
	require.Zero(t, code, diagnostic)
	require.Contains(t, out, "1\tinconclusive\tvalidation_unsupported\tnone\tnone\tnot_applicable")
	out, diagnostic, code = invoke("check", "999")
	require.Equal(t, 1, code)
	require.Empty(t, out)
	require.Contains(t, diagnostic, "state_record_not_found")
	batchArgs := []string{"check"}
	for i := 1; i <= 338; i++ {
		batchArgs = append(batchArgs, fmt.Sprint(i))
	}
	batchArgs = append(batchArgs, "1", "--workers", "4", "--max-attempts", "1")
	out, diagnostic, code = invoke(batchArgs...)
	require.Equal(t, 1, code)
	require.Contains(t, out, "selected=338")
	require.Equal(t, 338, strings.Count(out, "item_status=completed"))
	require.NotContains(t, out+diagnostic, "AbCdEf01")
	// A separate process reads the saved policy rather than current defaults.
	out, diagnostic, code = invoke("check", "show", "3")
	require.Zero(t, code, diagnostic)
	require.Empty(t, diagnostic)
	require.Contains(t, out, `"workers":4`)
	require.Contains(t, out, `"max_attempts":1`)
	require.Contains(t, out, "selected=338")
	require.Equal(t, 338, strings.Count(out, "item_status=completed"))
	require.Equal(t, 338, strings.Count(out, "attempt_number=1"))
	require.NotContains(t, out, "AbCdEf01")
}

// covers: spec 0003 AC-5, AC-6. Runtime help ignores invalid configuration,
// and argument errors never echo supplied secrets or create state.
func TestCLI_RuntimeHelpAndErrorsHaveNoSideEffects(t *testing.T) {
	binary := buildCLI(t, "")
	for _, args := range [][]string{
		{"crawl", "--help"}, {"crawl", "show", "--help"}, {"list", "--help"}, {"check", "--help"},
		{"validator", "recover", "--help"},
		{"check", "show", "--help"},
	} {
		out, diagnostic, code := runCLI(t, binary, args...)
		require.Zero(t, code, diagnostic)
		require.Contains(t, out, "Usage:")
		require.Empty(t, diagnostic)
	}
	for _, args := range [][]string{
		{"check", "ABCDEFGH-12345678-AbCdEf01"}, {"check", "0"}, {"check", "9223372036854775808"}, {"list", "secret"},
		{"crawl", "show"}, {"crawl", "--config", "/missing/private-secret.json"},
	} {
		out, diagnostic, code := runCLI(t, binary, args...)
		require.Equal(t, 1, code)
		require.Empty(t, out)
		require.NotContains(t, diagnostic, "secret")
		require.NotContains(t, diagnostic, "ABCDEFGH")
		require.NotContains(t, diagnostic, "Usage:")
	}
}
