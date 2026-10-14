package main_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// Spec 0005 AC-7 and AC-9: a closed process output pipe must not bypass batch finalization.
func TestCLI_ClosedPipePreservesBatchFinalization(t *testing.T) {
	binary := buildCLI(t, "")
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.json")
			require.NoError(t, os.WriteFile(configPath, []byte(`{}`), 0600))
			stateDir := filepath.Join(dir, "state")
			w, err := store.OpenWriter(t.Context(), stateDir, time.Now)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, w.Close()) })
			run, err := w.StartRun(t.Context(), state.RunInput{
				RequestID: "pipe_fixture", Kind: state.Crawl, StartedAt: time.Now(),
			})
			require.NoError(t, err)
			keys := make([]string, 5)
			for i := range keys {
				keys[i] = fmt.Sprintf("%08d-12345678-AbCdEf01", i)
			}
			_, err = w.RecordMessage(t.Context(), state.MessageInput{
				RunID: run.ID, SourceID: "fixture", MessageID: "1", ObservedAt: time.Now(), Keys: keys,
			})
			require.NoError(t, err)
			_, err = w.FinishRun(t.Context(), state.RunCompletion{
				RunID: run.ID, Status: state.Completed, FinishedAt: time.Now(),
			})
			require.NoError(t, err)
			require.NoError(t, w.Close())

			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, writer.Close()) })
			// Close before starting the child so its first write always reaches a broken pipe.
			require.NoError(t, reader.Close())
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary,
				"check", "1", "2", "3", "4", "5", "--workers", "2", "--validator", "unsupported",
				"--config", configPath, "--state-dir", stateDir,
			)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if stream == "stdout" {
				cmd.Stdout = writer
			} else {
				cmd.Stderr = writer
			}
			err = cmd.Run()
			require.NoError(t, ctx.Err())
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit)
			require.Equal(t, 1, exit.ExitCode(), "signal exit bypassed finalization: %s", stderr.String())

			// A reader must see finalization from the child, without writer recovery hiding the failure.
			r, err := store.OpenReader(t.Context(), stateDir)
			require.NoError(t, err)
			defer func() { require.NoError(t, r.Close()) }()
			runs, err := r.ListRuns(t.Context(), state.Validation, state.PageRequest{})
			require.NoError(t, err)
			require.Len(t, runs.Items, 1)
			report, err := r.GetValidationReport(t.Context(), runs.Items[0].ID)
			require.NoError(t, err)
			require.Zero(t, report.Totals.Pending)
			require.Zero(t, report.Totals.Active)
			require.Zero(t, report.Totals.Unresolved)
			if stream == "stdout" {
				require.Contains(t, stderr.String(), "code=output_error")
				require.Equal(t, state.Failed, report.Run.Status)
				require.NotNil(t, report.Run.ReasonCode)
				require.Equal(t, "output_error", *report.Run.ReasonCode)
				require.Equal(t, 2, report.Totals.Completed)
				require.Equal(t, 3, report.Totals.Skipped)
			} else {
				require.Contains(t, stdout.String(), "selected=5")
				// Spec 0009 AC-10: the first failed diagnostic write cancels admission.
				require.Equal(t, state.Cancelled, report.Run.Status)
				require.Zero(t, report.Totals.Completed)
				require.Equal(t, 5, report.Totals.Skipped)
			}
		})
	}
}
