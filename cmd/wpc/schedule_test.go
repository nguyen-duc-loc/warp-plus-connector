package main_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

func futureCrawlProfile(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	stateDir := filepath.Join(dir, "state")
	require.NoError(
		t,
		os.WriteFile(configPath, []byte(`{"validation_enabled":false,"sources":[{"channel":"warpplus"}]}`), 0600),
	)
	w, err := store.OpenWriter(t.Context(), stateDir, time.Now)
	require.NoError(t, err)
	at := time.Now().UTC().Truncate(time.Microsecond)
	_, err = w.PrepareSchedule(
		t.Context(),
		at,
		state.DefaultSchedulePolicy(),
		[]state.SourcePlan{{Channel: "warpplus", PageLimit: 1}},
		false,
	)
	require.NoError(t, err)
	r, err := w.StartCrawl(
		t.Context(), state.CrawlInput{
			RequestID: "future_crawl", StartedAt: at, SourceID: "telegram:warpplus",
			RequestedURL: "https://t.me/s/warpplus", CoverageMode: "latest_preview", PageLimit: 1,
		},
	)
	require.NoError(t, err)
	_, err = w.RecordCrawlPage(t.Context(), state.CrawlPage{RunID: r.Run.ID, FetchedAt: at})
	require.NoError(t, err)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{RunID: r.Run.ID, Status: state.Completed, FinishedAt: at})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return configPath, stateDir
}

// Spec 0007 AC-11, AC-14: production parsing and signal handling operate against a
// future saved schedule, so this check makes no public source or provider requests.
func TestCLI_ScheduleInspectionAndRunnerSignals(t *testing.T) {
	binary := buildCLI(t, "")
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(
			sig.String(), func(t *testing.T) {
				configPath, stateDir := futureCrawlProfile(t)
				output, err := exec.Command(
					binary,
					"schedule",
					"show",
					"--format",
					"json",
					"--config",
					configPath,
					"--state-dir",
					stateDir,
				).Output()
				require.NoError(t, err)
				var saved map[string]any
				require.NoError(t, json.Unmarshal(output, &saved))
				require.Equal(t, float64(1), saved["schema_version"])
				require.Equal(t, "summary", saved["view"])
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, binary, "run", "--config", configPath, "--state-dir", stateDir)
				diagnostics, err := cmd.StderrPipe()
				require.NoError(t, err)
				var stdout bytes.Buffer
				cmd.Stdout = &stdout
				require.NoError(t, cmd.Start())
				scanner := bufio.NewScanner(diagnostics)
				require.True(t, scanner.Scan())
				require.Contains(t, scanner.Text(), "schedule_starting")
				require.NoError(t, cmd.Process.Signal(sig))
				for scanner.Scan() {
				}
				err = cmd.Wait()
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit)
				expected := 130
				if sig == syscall.SIGTERM {
					expected = 143
				}
				require.Equal(t, expected, exit.ExitCode())
				require.Empty(t, stdout.String())
				require.NoError(t, ctx.Err())
				w, err := store.OpenWriter(t.Context(), stateDir, time.Now)
				require.NoError(t, err)
				require.NoError(t, w.Close())
			},
		)
	}
}
