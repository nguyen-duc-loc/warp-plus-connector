package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/cli"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// covers: spec 0007 AC-1, AC-11. Invalid commands fail before creating state.
func TestSchedule_InvalidStartupHasNoStateEffects(t *testing.T) {
	for _, args := range [][]string{
		{"run", "unexpected"}, {"run", "--crawl-enabled=false", "--validation-enabled=false"},
		{"run", "--crawl-interval", "59s"}, {"run", "--failure-interval", "721h"},
		{"run", "--recheck-interval", "0s"}, {"run", "--workers", "5"},
		{"run", "--source", "warpplus"}, {"run", "--page-limit", "2"},
		{"run"}, {"schedule", "show", "0"}, {"schedule", "show", "1", "2"},
		{"schedule", "show", "--format", "yaml"}, {"schedule", "show", "9223372036854775808"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := t.TempDir()
			path, stateDir := filepath.Join(dir, "config.json"), filepath.Join(dir, "state")
			require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
			cmd := cli.NewRootCommand("test", "")
			var out, diagnostics bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&diagnostics)
			cmd.SetArgs(append(append([]string{}, args...), "--config", path, "--state-dir", stateDir))
			require.Error(t, cmd.ExecuteContext(t.Context()))
			require.Empty(t, out.String())
			_, err := os.Stat(stateDir)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

// covers: spec 0007 AC-10, AC-11. Inspection remains private and available under
// writer ownership, with a configured but unreachable Docker backend.
func TestSchedule_InspectionPreservesSavedStateAndHidesKeys(t *testing.T) {
	dir := t.TempDir()
	path, stateDir := filepath.Join(dir, "config.json"), filepath.Join(dir, "state")
	body := fmt.Sprintf(`{"validator":"docker","validator_image":"sha256:%s",`+
		`"validator_socket":%q,"failure_interval":"2h"}`, strings.Repeat("a", 64), filepath.Join(dir, "absent.sock"))
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w, err := store.OpenWriter(t.Context(), stateDir, func() time.Time { return at })
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: at})
	require.NoError(t, err)
	key := "SYNTHET1-12345678-AbCdEf01"
	_, err = w.RecordMessage(t.Context(), state.MessageInput{
		RunID: run.ID, SourceID: "fixture", MessageID: "1", ObservedAt: at, Keys: []string{key},
	})
	require.NoError(t, err)
	before, err := w.GetSchedule(t.Context(), 1, func() time.Time { return at })
	require.NoError(t, err)
	for _, format := range []string{"text", "json"} {
		cmd := cli.NewRootCommand("test", "")
		var out, diagnostics bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&diagnostics)
		cmd.SetArgs([]string{"schedule", "show", "1", "--format", format, "--config", path, "--state-dir", stateDir})
		require.NoError(t, cmd.ExecuteContext(t.Context()))
		require.Empty(t, diagnostics.String())
		require.NotContains(t, out.String(), key)
		if format == "json" {
			var object map[string]any
			require.NoError(t, json.Unmarshal(out.Bytes(), &object))
			require.Equal(t, float64(1), object["schema_version"])
			require.Equal(t, "candidate", object["view"])
			require.Equal(t, "first_check", object["reason"])
			require.Equal(t, "0s", object["interval"])
			require.Equal(t, "2026-01-01T00:00:00.000000Z", object["due_at"])
			require.Nil(t, object["basis_attempt_id"])
			require.Nil(t, object["active_run_id"])
		} else {
			require.Contains(t, out.String(), "candidate_id=1\n")
			require.Contains(t, out.String(), "reason=first_check\n")
		}
	}
	after, err := w.GetSchedule(t.Context(), 1, func() time.Time { return at })
	require.NoError(t, err)
	require.Equal(t, before, after)
}
