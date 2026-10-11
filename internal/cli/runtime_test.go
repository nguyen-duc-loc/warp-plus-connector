package cli_test

import (
	"bytes"
	"errors"
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

// covers: spec 0003 AC-3, AC-6. Absent state is not an empty collection;
// broken output is reported even when the saved collection is empty.
func TestRuntime_DistinguishesAbsentEmptyAndFailedOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
	stateDir := filepath.Join(dir, "state")
	execute := func(fail bool) (string, error) {
		cmd := cli.NewRootCommand("test", "")
		var out, diagnostic bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&diagnostic)
		if fail {
			cmd.SetOut(failingWriter{err: errors.New("private-secret")})
		}
		cmd.SetArgs([]string{"list", "--config", path, "--state-dir", stateDir})
		err := cmd.ExecuteContext(t.Context())
		return out.String(), err
	}
	out, err := execute(false)
	require.EqualError(t, err, "state_not_initialized")
	require.Empty(t, out)
	_, err = os.Stat(stateDir)
	require.ErrorIs(t, err, os.ErrNotExist)
	w, err := store.OpenWriter(t.Context(), stateDir, time.Now)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	out, err = execute(false)
	require.NoError(t, err)
	require.Equal(t, "No candidates saved.\n", out)
	_, err = execute(true)
	require.Error(t, err)
	require.Equal(t, "output_error", cli.Diagnostic(err))
}

// covers: spec 0005 AC-1, AC-10. Invalid selections and policy flags fail before creating state.
func TestCheck_InvalidArgumentsAndPolicyHaveNoStateEffects(t *testing.T) {
	for _, args := range [][]string{
		{"check"}, {"check", "0"}, {"check", "1", "-2"}, {"check", "1", "+2"},
		{"check", "9223372036854775808"}, {"check", "1", "abc"}, {"check", "1.0"},
		{"check", "show"}, {"check", "show", "1", "2"},
		{"check", "1", "--workers", "0"}, {"check", "1", "--workers", "5"},
		{"check", "1", "--max-attempts", "0"}, {"check", "1", "--max-attempts", "6"},
		{"check", "1", "--mutation-interval", "1500ms"}, {"check", "1", "--mutation-interval", "61s"},
		{"check", "1", "--candidate-timeout", "59s"}, {"check", "1", "--candidate-timeout", "61m"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := t.TempDir()
			path, stateDir := filepath.Join(dir, "config.json"), filepath.Join(dir, "state")
			require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
			cmd := cli.NewRootCommand("test", "")
			var out, diagnostic bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&diagnostic)
			cmd.SetArgs(append(append([]string{}, args...), "--config", path, "--state-dir", stateDir))
			require.Error(t, cmd.ExecuteContext(t.Context()))
			require.Empty(t, out.String())
			_, err := os.Stat(stateDir)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

// covers: spec 0005 AC-1, AC-9, AC-10. Actual commands save effective flags and render history after restart.
func TestCheck_SavedPolicyResultsAndOutputErrors(t *testing.T) {
	dir := t.TempDir()
	path, stateDir := filepath.Join(dir, "config.json"), filepath.Join(dir, "state")
	require.NoError(t, os.WriteFile(path, []byte(`{"check_workers":4,"check_max_attempts":5}`), 0600))
	w, err := store.OpenWriter(t.Context(), stateDir, time.Now)
	require.NoError(t, err)
	at := time.Now()
	seed, err := w.StartRun(t.Context(), state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: at})
	require.NoError(t, err)
	keys := []string{"00000001-12345678-AbCdEf01", "00000002-12345678-AbCdEf01"}
	_, err = w.RecordMessage(t.Context(), state.MessageInput{
		RunID: seed.ID, SourceID: "fixture", MessageID: "1", ObservedAt: at, Keys: keys,
	})
	require.NoError(t, err)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{RunID: seed.ID, Status: state.Completed, FinishedAt: at})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	execute := func(args []string, fail bool) (string, error) {
		cmd := cli.NewRootCommand("test", "")
		var out, diagnostic bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&diagnostic)
		if fail {
			cmd.SetOut(failingWriter{err: errors.New(keys[0])})
		}
		cmd.SetArgs(append(args, "--config", path, "--state-dir", stateDir))
		err := cmd.ExecuteContext(t.Context())
		message := out.String() + diagnostic.String()
		if err != nil {
			message += cli.Diagnostic(err)
		}
		for _, key := range keys {
			require.NotContains(t, message, key)
		}
		return out.String(), err
	}
	out, err := execute([]string{"check", "2", "1", "2", "--workers", "2", "--max-attempts", "3"}, false)
	require.EqualError(t, err, "validation_batch_inconclusive")
	require.Equal(t, 2, strings.Count(out, "item_status=completed"))
	require.Contains(t, out, "selected=2 pending=0 active=0 completed=2")
	require.Contains(t, out, "inconclusive=2 unresolved=0 unstarted=0")
	r, err := store.OpenReader(t.Context(), stateDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	runs, err := r.ListRuns(t.Context(), state.Validation, state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, runs.Items, 1)
	id := runs.Items[0].ID
	report, err := r.GetValidationReport(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, 2, report.Policy.Workers)
	require.Equal(t, 3, report.Policy.MaxAttempts)
	// A configured Docker backend with an absent socket must not affect saved reads.
	body := fmt.Sprintf(`{"validator":"docker","validator_image":"sha256:%s",`+
		`"validator_socket":%q,"check_workers":1}`, strings.Repeat("a", 64), filepath.Join(dir, "absent.sock"))
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))
	args := []string{"check", "show", fmt.Sprint(id)}
	out, err = execute(args, false)
	require.NoError(t, err)
	require.Contains(t, out, `"workers":2`)
	require.Contains(t, out, `"max_attempts":3`)
	require.Equal(t, 2, strings.Count(out, "attempt_number=1 failure_class=terminal retry_wait=none"))
	require.Less(t, strings.Index(out, "candidate_id=2"), strings.Index(out, "candidate_id=1"))
	_, err = execute(args, true)
	require.Equal(t, "output_error", cli.Diagnostic(err))
	after, err := r.GetValidationReport(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, report, after)
}
