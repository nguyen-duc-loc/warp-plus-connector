package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// covers: spec 0006 AC-8, AC-10. A failed seek cannot begin stdout delivery.
func TestOutputStage_SeekFailureDeliversNothing(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	stage, err := newOutputStage("", "")
	require.NoError(t, err)
	_, err = stage.file.WriteString("private-key\n")
	require.NoError(t, err)
	require.NoError(t, stage.file.Close())
	var stdout bytes.Buffer
	delivered, err := stage.copy(t.Context(), &stdout)
	require.False(t, delivered)
	require.Equal(t, "export_prepare_failed", Diagnostic(err))
	require.Empty(t, stdout.String())
	// The already closed handle reports an error, but owned paths are removed.
	require.Error(t, stage.cleanup())
	_, err = os.Lstat(stage.directory)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// covers: spec 0006 AC-9, AC-12. Atomic publication cannot replace a destination created during preparation.
func TestOutputStage_PublicationPreservesRacingDestinations(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			_, stateDir, _ := seedOutputState(t)
			parent := t.TempDir()
			destination := filepath.Join(parent, "result")
			stage, err := newOutputStage(destination, stateDir)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stage.cleanup()) })
			info, err := os.Stat(stage.directory)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0700), info.Mode().Perm())
			info, err = stage.file.Stat()
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), info.Mode().Perm())
			_, err = stage.file.WriteString("prepared private data\n")
			require.NoError(t, err)
			sentinel := filepath.Join(parent, "sentinel")
			require.NoError(t, os.WriteFile(sentinel, []byte("other owner"), 0600))
			switch kind {
			case "file":
				require.NoError(t, os.WriteFile(destination, []byte("other owner"), 0600))
			case "directory":
				require.NoError(t, os.Mkdir(destination, 0700))
			default:
				require.NoError(t, os.Symlink(sentinel, destination))
			}
			before, err := os.Lstat(destination)
			require.NoError(t, err)
			published, err := stage.publish(t.Context())
			require.False(t, published)
			require.Equal(t, "export_exists", Diagnostic(err))
			require.NoError(t, stage.cleanup())
			after, err := os.Lstat(destination)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after))
			if kind != "directory" {
				data, err := os.ReadFile(destination)
				require.NoError(t, err)
				require.Equal(t, "other owner", string(data))
			}
			data, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "other owner", string(data))
		})
	}
}

// covers: spec 0006 AC-5, AC-9, AC-11. Canonical parents exclude state aliases but permit sibling prefixes.
func TestExport_DestinationAliasesRespectStateBoundary(t *testing.T) {
	configPath, stateDir, _ := seedOutputState(t)
	alias := filepath.Join(t.TempDir(), "state-alias")
	require.NoError(t, os.Symlink(stateDir, alias))
	for _, parent := range []string{stateDir, alias} {
		out, _, err := runOutputCommand(t.Context(), configPath, stateDir, nil,
			"export", "--output", filepath.Join(parent, "result.json"))
		require.Equal(t, "export_destination_invalid", Diagnostic(err))
		require.Empty(t, out)
	}
	sibling := stateDir + "-exports"
	require.NoError(t, os.Mkdir(sibling, 0700))
	destination := filepath.Join(sibling, "result.json")
	out, diagnostic, err := runOutputCommand(t.Context(), configPath, stateDir, nil,
		"export", "--output", destination)
	require.NoError(t, err)
	require.Empty(t, out)
	require.Contains(t, diagnostic, "published=true")
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.True(t, json.Valid(data))
	info, err := os.Stat(destination)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	_, _, err = runOutputCommand(t.Context(), configPath, stateDir, nil, "export", "--output", destination)
	require.Equal(t, "export_exists", Diagnostic(err))
	after, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, data, after)
}

// covers: spec 0006 AC-9, AC-10. A real directory sync error follows publication,
// preserves the complete file, and reports publication without a success summary.
func TestExport_DirectorySyncFailurePreservesPublishedFile(t *testing.T) {
	configPath, stateDir, key := seedOutputState(t)
	parent := filepath.Join(t.TempDir(), "output")
	require.NoError(t, os.Mkdir(parent, 0700))
	parent, err := filepath.EvalSymlinks(parent)
	require.NoError(t, err)
	destination := filepath.Join(parent, "results.json")
	backup := parent + ".saved"
	ctx := &publicationContext{Context: t.Context(), destination: destination}
	ctx.afterPublication = func() error {
		if err := os.Rename(parent, backup); err != nil {
			return err
		}
		// publish opens this path after linking the complete file. /dev/null
		// opens successfully, but its actual File.Sync returns an error.
		return os.Symlink(os.DevNull, parent)
	}
	restored := false
	restore := func() {
		observed, _ := ctx.status()
		if !observed || restored {
			return
		}
		require.NoError(t, os.Remove(parent))
		require.NoError(t, os.Rename(backup, parent))
		restored = true
	}
	t.Cleanup(restore)
	stdout, stderr, err := runOutputCommand(ctx, configPath, stateDir, io.Discard,
		"export", "--output", destination)
	observed, hookErr := ctx.status()
	require.NoError(t, hookErr)
	require.True(t, observed, "the fault must occur after atomic publication")
	require.Empty(t, stdout)
	require.Empty(t, stderr)
	var failure *deliveryError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "export_commit_uncertain", failure.code)
	require.True(t, failure.published)
	require.False(t, failure.delivered)
	require.Equal(t, destination, failure.destination)
	require.NotEmpty(t, failure.remaining, "cleanup also fails while the parent is replaced")
	require.Contains(t, Diagnostic(err), "published=true")
	require.NotContains(t, Diagnostic(err), key)
	restore()
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	var document struct {
		Items         []json.RawMessage `json:"items"`
		ReturnedCount int64             `json:"returned_count"`
	}
	require.NoError(t, json.Unmarshal(data, &document))
	require.Len(t, document.Items, 1)
	require.Equal(t, int64(1), document.ReturnedCount)
	require.NotContains(t, string(data), key)
	prepared, err := os.ReadFile(filepath.Join(failure.remaining, "prepared"))
	require.NoError(t, err)
	require.Equal(t, data, prepared)
}

// covers: spec 0006 AC-10. Cancellation following publication retains a complete
// destination and never claims that the output failed before publication.
func TestExport_CancellationAfterPublicationPreservesFile(t *testing.T) {
	configPath, stateDir, _ := seedOutputState(t)
	destination := filepath.Join(t.TempDir(), "results.json")
	base, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	ctx := &publicationContext{Context: base, destination: destination}
	ctx.afterPublication = func() error { cancel(); return nil }
	stdout, stderr, err := runOutputCommand(ctx, configPath, stateDir, io.Discard,
		"export", "--output", destination)
	_, hookErr := ctx.status()
	require.NoError(t, hookErr)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
	require.Contains(t, Diagnostic(err), "cancelled")
	require.Contains(t, Diagnostic(err), "published=true")
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.True(t, json.Valid(data))
	stages, err := filepath.Glob(filepath.Join(filepath.Dir(destination), "wpc-output-*"))
	require.NoError(t, err)
	require.Empty(t, stages)
}

// covers: spec 0006 AC-10. Cleanup failure preserves primary errors, records
// partial stdout delivery, and leaves foreign files for their owner.
func TestExport_CleanupFailurePreservesDeliveryAndPrimaryError(t *testing.T) {
	for _, mode := range []string{"success", "short_write", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			configPath, stateDir, key := seedOutputStateCount(t, 200)
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			writer := &obstructCleanupWriter{parent: temporary, mode: mode, cancel: cancel}
			_, stderr, err := runOutputCommand(ctx, configPath, stateDir, writer, "export")
			require.NoError(t, writer.err)
			require.Empty(t, stderr, "cleanup failure must not follow a success summary")
			var failure *deliveryError
			require.ErrorAs(t, err, &failure)
			want := "export_cleanup_failed"
			switch mode {
			case "short_write":
				want = "output_error"
			case "cancelled":
				want = "cancelled"
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Equal(t, want, failure.code)
			if mode == "success" {
				require.Greater(t, writer.writes, 1, "the prepared output must require more than one copy chunk")
				require.True(t, json.Valid(writer.data.Bytes()))
			} else {
				require.Equal(t, 1, writer.writes, "no further writes follow failure or cancellation")
				require.Equal(t, "{", writer.data.String(), "the accepted prefix cannot be withdrawn")
			}
			require.True(t, failure.delivered)
			require.False(t, failure.published)
			require.Contains(t, Diagnostic(err), "delivered=true")
			require.NotContains(t, Diagnostic(err), key)
			require.Equal(t, filepath.Dir(writer.foreign), failure.remaining)
			sentinel, err := os.ReadFile(writer.foreign)
			require.NoError(t, err)
			require.Equal(t, "foreign owner", string(sentinel))
			_, err = os.Lstat(filepath.Join(failure.remaining, "prepared"))
			require.ErrorIs(t, err, os.ErrNotExist)
			require.NoError(t, os.Remove(writer.foreign))
			require.NoError(t, os.Remove(failure.remaining))
		})
	}
}

type publicationContext struct {
	context.Context
	mu               sync.Mutex
	destination      string
	afterPublication func() error
	observed         bool
	hookErr          error
}

func (c *publicationContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.observed {
		if _, err := os.Lstat(c.destination); err == nil {
			c.observed = true
			c.hookErr = c.afterPublication()
		}
	}
	return c.Context.Err()
}

func (c *publicationContext) status() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.observed, c.hookErr
}

type obstructCleanupWriter struct {
	parent  string
	mode    string
	cancel  context.CancelFunc
	writes  int
	foreign string
	err     error
	data    bytes.Buffer
}

func (w *obstructCleanupWriter) Write(p []byte) (int, error) {
	w.writes++
	directories, err := filepath.Glob(filepath.Join(w.parent, "wpc-output-*"))
	if err != nil || len(directories) != 1 {
		w.err = errors.New("expected one private staging directory")
		return 0, w.err
	}
	w.foreign = filepath.Join(directories[0], "foreign")
	w.err = os.WriteFile(w.foreign, []byte("foreign owner"), 0600)
	if w.err != nil {
		return 0, w.err
	}
	if w.mode == "cancelled" {
		_, _ = w.data.Write(p[:1])
		w.cancel()
		return 1, io.ErrClosedPipe
	}
	if w.mode == "short_write" {
		_, _ = w.data.Write(p[:1])
		return 1, nil
	}
	return w.data.Write(p)
}

func seedOutputState(t *testing.T) (configPath, stateDir, key string) {
	t.Helper()
	return seedOutputStateCount(t, 1)
}

func seedOutputStateCount(t *testing.T, count int) (configPath, stateDir, key string) {
	t.Helper()
	root := t.TempDir()
	configPath, stateDir = filepath.Join(root, "config.json"), filepath.Join(root, "state")
	require.NoError(t, os.WriteFile(configPath, []byte(`{}`), 0600))
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	w, err := store.OpenWriter(t.Context(), stateDir, func() time.Time { return at })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	run, err := w.StartRun(t.Context(), state.RunInput{
		RequestID: "output-seed", Kind: state.Crawl, StartedAt: at,
	})
	require.NoError(t, err)
	key = "00000001-12345678-AbCdEf01"
	keys := make([]string, count)
	for i := range keys {
		keys[i] = fmt.Sprintf("%08d-12345678-AbCdEf01", i+1)
	}
	_, err = w.RecordMessage(t.Context(), state.MessageInput{
		RunID: run.ID, SourceID: "fixture", MessageID: "1", ObservedAt: at, Keys: keys,
	})
	require.NoError(t, err)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{
		RunID: run.ID, Status: state.Completed, FinishedAt: at,
	})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return configPath, stateDir, key
}

func runOutputCommand(
	ctx context.Context, configPath, stateDir string, output io.Writer, args ...string,
) (string, string, error) {
	cmd := NewRootCommand("test", "")
	var stdout, stderr bytes.Buffer
	if output == nil {
		output = &stdout
	}
	cmd.SetOut(output)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append(args, "--config", configPath, "--state-dir", stateDir))
	err := cmd.ExecuteContext(ctx)
	return stdout.String(), stderr.String(), err
}
