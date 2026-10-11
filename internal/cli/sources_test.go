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

// Spec 0004 AC-2, AC-3, AC-10: inspection and invalid selection create no state.
func TestSourcesAndSelectionHaveNoStateEffects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	stateDir := filepath.Join(dir, "state")
	execute := func(args []string, fail bool) (string, error) {
		root := cli.NewRootCommand("test", "")
		var out, diagnostic bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&diagnostic)
		if fail {
			root.SetOut(failingWriter{err: errors.New("private output")})
		}
		root.SetArgs(append(args, "--config", path, "--state-dir", stateDir))
		err := root.ExecuteContext(t.Context())
		return out.String(), err
	}
	require.NoError(
		t,
		os.WriteFile(
			path,
			[]byte(`{"crawl_page_limit":3,"sources":[{"channel":"Enabled"},`+
				`{"channel":"Disabled","enabled":false,"page_limit":4}]}`),
			0600,
		),
	)
	out, err := execute([]string{"sources"}, false)
	require.NoError(t, err)
	require.Contains(t, out, "enabled\ttelegram:enabled\ttrue\t3")
	require.Contains(t, out, "disabled\ttelegram:disabled\tfalse\t4")
	_, err = execute([]string{"sources"}, true)
	require.EqualError(t, err, "output_error")
	for _, args := range [][]string{
		{"crawl", "--source", "disabled"}, {"crawl", "--source", "unknown"}, {"crawl", "--source", ""},
		{"crawl", "--page-limit", "0"}, {"crawl", "--source", "enabled", "--source", "ENABLED"},
	} {
		_, err := execute(args, false)
		require.Equal(t, "source_selection_invalid", cli.Diagnostic(err))
	}
	_, err = execute([]string{"crawl", "show", "1", "--source", "enabled"}, false)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(path, []byte(`{"sources":[]}`), 0600))
	out, err = execute([]string{"sources"}, false)
	require.NoError(t, err)
	require.Equal(t, "No sources configured.\n", out)
	_, err = execute([]string{"crawl"}, false)
	require.Equal(t, "source_selection_empty", cli.Diagnostic(err))
	_, err = os.Stat(stateDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// covers: spec 0004 AC-3, AC-4, AC-8, AC-9, AC-10.
// Reports use the saved plan even when source settings have been removed.
func TestCrawlShow_SavedCoverageAndOutputFailure(t *testing.T) {
	dir := t.TempDir()
	path, stateDir := filepath.Join(dir, "config.json"), filepath.Join(dir, "state")
	require.NoError(t, os.WriteFile(path, []byte(`{"sources":[]}`), 0600))
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	w, err := store.OpenWriter(t.Context(), stateDir, func() time.Time { return at })
	require.NoError(t, err)
	plan, err := w.StartCrawlPlan(
		t.Context(),
		state.CrawlPlanInput{
			RequestID: "saved", StartedAt: at,
			Sources: []state.SourcePlan{{Channel: "first", PageLimit: 2}, {Channel: "unattempted", PageLimit: 1}},
		},
	)
	require.NoError(t, err)
	id := state.CrawlPageID{RunID: plan.Run.ID, SourceID: "telegram:first", PageNumber: 1}
	_, err = w.StartCrawlRequest(t.Context(), state.CrawlRequest{CrawlPageID: id, RequestedAt: at})
	require.NoError(t, err)
	n := int64(7)
	_, err = w.RecordCrawlParse(
		t.Context(),
		state.CrawlParse{
			CrawlPageID: id, FetchedAt: at, ParsedPostCount: 2, MinMessageID: &n, MaxMessageID: &n, NextBeforeID: &n,
		},
	)
	require.NoError(t, err)
	key := "ABCDEFGH-12345678-AbCdEf01"
	_, err = w.RecordCrawlMessage(
		t.Context(),
		id,
		state.MessageInput{RunID: id.RunID, SourceID: id.SourceID, MessageID: "7", ObservedAt: at, Keys: []string{key}},
	)
	require.NoError(t, err)
	_, err = w.FinishCrawlPage(
		t.Context(),
		state.CrawlPageCompletion{CrawlPageID: id, Status: "completed", FinishedAt: at},
	)
	require.NoError(t, err)
	id.PageNumber = 2
	_, err = w.StartCrawlRequest(t.Context(), state.CrawlRequest{CrawlPageID: id, RequestedAt: at})
	require.NoError(t, err)
	execute := func(fail bool) (string, error) {
		cmd := cli.NewRootCommand("test", "")
		var out, diagnostic bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&diagnostic)
		if fail {
			cmd.SetOut(failingWriter{err: errors.New(key)})
		}
		cmd.SetArgs([]string{"crawl", "show", fmt.Sprint(id.RunID), "--config", path, "--state-dir", stateDir})
		err := cmd.ExecuteContext(t.Context())
		require.NotContains(t, out.String()+diagnostic.String(), key)
		return out.String(), err
	}
	out, err := execute(false)
	require.NoError(t, err)
	require.Contains(
		t,
		out,
		"status=running reason=none counts_current=true selected_sources=2 saved_candidates=1 saved_sightings=1",
	)
	require.Contains(t, out, "source=telegram:first channel=first url=https://t.me/s/first page_limit=2")
	require.Contains(t, out, "pages_requested=2 pages_parsed=1 pages_completed=1 parsed_post_occurrences=2")
	require.Contains(t, out, "more_available=unknown history_complete=false")
	require.Contains(t, out, "parsed_post_occurrences=none min_message_id=none max_message_id=none")
	require.Contains(t, out, "page=2 status=requested requested_url=https://t.me/s/first?before=7")
	require.Equal(t, 2, strings.Count(out, "history_complete=false"))
	before, err := w.GetCrawlReport(t.Context(), id.RunID)
	require.NoError(t, err)
	_, err = execute(true)
	require.Equal(t, "output_error", cli.Diagnostic(err))
	after, err := w.GetCrawlReport(t.Context(), id.RunID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = w.FinishCrawlPlan(
		t.Context(),
		state.RunCompletion{RunID: id.RunID, Status: state.Cancelled, ReasonCode: "cancelled", FinishedAt: at},
	)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	out, err = execute(false)
	require.NoError(t, err)
	require.Contains(t, out, "status=cancelled reason=cancelled counts_current=false")
	require.Contains(t, out, "page=2 status=cancelled")
	require.Contains(t, out, "status=skipped stop_reason=cancelled")
}

// covers: spec 0004 AC-2, AC-3. Flags cannot hide an invalid disabled source.
func TestCrawl_InvalidConfigCannotBeRescuedBySelection(t *testing.T) {
	dir := t.TempDir()
	path, stateDir := filepath.Join(dir, "config.json"), filepath.Join(dir, "state")
	require.NoError(
		t,
		os.WriteFile(
			path,
			[]byte(`{"sources":[{"channel":"good"},{"channel":"bad","enabled":false,"page_limit":0}]}`),
			0600,
		),
	)
	cmd := cli.NewRootCommand("test", "")
	var out, diagnostic bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&diagnostic)
	cmd.SetArgs([]string{"crawl", "--source", "good", "--page-limit", "1", "--config", path, "--state-dir", stateDir})
	err := cmd.ExecuteContext(t.Context())
	require.Equal(t, "invalid configuration", cli.Diagnostic(err))
	require.Empty(t, out.String())
	_, err = os.Stat(stateDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}
