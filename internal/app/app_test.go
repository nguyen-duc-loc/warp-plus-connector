package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

var crawlTime = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

const crawlKey = "ABCDEFGH-12345678-AbCdEf01"

type sourceFunc func(context.Context) (telegram.Page, error)

func (f sourceFunc) FetchPage(ctx context.Context, _ string, _ *int64) (telegram.Page, error) {
	return f(ctx)
}

type httpFunc func(*http.Request) (*http.Response, error)

func (f httpFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func crawlApp(t *testing.T) *app.App {
	t.Helper()
	return &app.App{Settings: config.Settings{Sources: []config.Source{{Channel: "warpplus", Enabled: true, PageLimit: 1}}, StateDir: filepath.Join(t.TempDir(), "state"), RecheckInterval: time.Hour}, Clock: func() time.Time { return crawlTime }}
}

// covers: spec 0003 AC-1, AC-2, AC-3. HTTP parsing and SQLite form one real path.
func TestCrawl_PersistsIntentBeforeFetchAndMergesRepeatedDiscoveries(t *testing.T) {
	a := crawlApp(t)
	calls := 0
	a.Source = telegram.New("test", a.Clock, httpFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		reader, err := store.OpenReader(r.Context(), a.Settings.StateDir)
		require.NoError(t, err)
		runs, err := reader.ListRuns(r.Context(), state.Crawl, state.PageRequest{})
		require.NoError(t, err)
		var active state.Run
		for _, run := range runs.Items {
			if run.Status == state.Running {
				active = run
			}
		}
		require.NotZero(t, active.ID)
		report, err := reader.GetCrawlReport(r.Context(), active.ID)
		require.NoError(t, err)
		require.Nil(t, report.Coverage)
		require.Len(t, report.Sources, 1)
		require.Equal(t, "requested", report.Sources[0].Pages[0].Status)
		require.Equal(t, 1, report.Sources[0].PageLimit)
		require.Nil(t, report.Sources[0].Pages[0].FetchedAt)
		require.Nil(t, report.Sources[0].Pages[0].ParsedPostCount)
		require.NoError(t, reader.Close())
		body := `<div class="tgme_channel_info_header_username">@warpplus</div><div class="tgme_channel_history"><div class="tgme_widget_message" data-post="warpplus/7"><div class="tgme_widget_message_text">` + crawlKey + ` ` + crawlKey + `</div></div><div class="tgme_widget_message" data-post="warpplus/9"></div></div>`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	for i := 0; i < 2; i++ {
		report, err := a.Crawl(t.Context())
		require.NoError(t, err)
		require.Equal(t, state.Completed, report.Run.Status)
		require.Equal(t, int64(1), report.SavedCandidates)
		require.Equal(t, int64(1), report.SavedSightings)
		require.Equal(t, 2, *report.Sources[0].Pages[0].ParsedPostCount)
		require.Equal(t, int64(7), *report.Sources[0].Pages[0].MinMessageID)
		require.Equal(t, int64(9), *report.Sources[0].Pages[0].MaxMessageID)
		restarted := &app.App{Settings: a.Settings, Clock: a.Clock}
		saved, err := restarted.ShowCrawl(t.Context(), report.Run.ID)
		require.NoError(t, err)
		require.Equal(t, report, saved)
		reader, err := store.OpenReader(t.Context(), a.Settings.StateDir)
		require.NoError(t, err)
		observations, err := reader.ListObservations(t.Context(), report.Run.ID, state.PageRequest{})
		require.NoError(t, err)
		require.Len(t, observations.Items, 1)
		require.NoError(t, reader.Close())
	}
	var candidates []state.Candidate
	require.NoError(t, a.List(t.Context(), func(c state.Candidate) error { candidates = append(candidates, c); return nil }))
	require.Len(t, candidates, 1)
	require.Equal(t, crawlKey, candidates[0].Key)
	require.Equal(t, state.Unchecked, candidates[0].LatestOutcome)
	require.Equal(t, 2, calls)
}

// covers: spec 0003 AC-2, AC-6. Saved facts distinguish empty, failed and cancelled crawls.
func TestCrawl_PreservesProgressAndSanitizesFailures(t *testing.T) {
	for _, mode := range []string{"empty", "source failure", "partial message failure", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			a := crawlApp(t)
			var logs bytes.Buffer
			a.Logger = slog.New(slog.NewTextHandler(&logs, nil))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			a.Source = sourceFunc(func(context.Context) (telegram.Page, error) {
				switch mode {
				case "source failure":
					return telegram.Page{}, &telegram.Error{Reason: "source_http_error"}
				case "cancelled":
					cancel()
					return telegram.Page{}, context.Canceled
				case "partial message failure":
					minID, maxID := int64(1), int64(2)
					return telegram.Page{Facts: state.CrawlPage{FetchedAt: crawlTime, ParsedPostCount: 2, MinMessageID: &minID, MaxMessageID: &maxID}, Messages: []state.MessageInput{
						{SourceID: telegram.SourceID, MessageID: "1", ObservedAt: crawlTime, Keys: []string{crawlKey}},
						{SourceID: telegram.SourceID, MessageID: "2", ObservedAt: crawlTime, Keys: []string{" "}},
					}}, nil
				default:
					return telegram.Page{Facts: state.CrawlPage{FetchedAt: crawlTime}}, nil
				}
			})
			report, err := a.Crawl(ctx)
			if mode == "empty" {
				require.NoError(t, err)
				require.Equal(t, state.Completed, report.Run.Status)
			} else {
				require.Error(t, err)
				want := state.Failed
				if mode == "partial message failure" {
					want = state.Running
				}
				if mode == "cancelled" {
					want = state.Cancelled
				}
				require.Equal(t, want, report.Run.Status)
			}
			if mode == "cancelled" { // Cancellation has its own terminal status.
				require.ErrorIs(t, err, context.Canceled)
			}
			if mode == "partial message failure" {
				require.Equal(t, int64(1), report.SavedCandidates)
				require.Equal(t, 2, *report.Sources[0].Pages[0].ParsedPostCount)
			} else {
				require.Zero(t, report.SavedCandidates)
			}
			if mode == "source failure" || mode == "cancelled" {
				require.Nil(t, report.Sources[0].Pages[0].FetchedAt)
			}
			saved, readErr := a.ShowCrawl(t.Context(), report.Run.ID)
			require.NoError(t, readErr)
			require.Equal(t, report, saved)
			require.NotContains(t, logs.String(), crawlKey)
		})
	}
}

// covers: spec 0003 AC-6. Store ownership stops fetching, and output failures stop traversal.
func TestCrawl_ContentionPreventsSourceWork(t *testing.T) {
	a := crawlApp(t)
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	a.Source = sourceFunc(func(context.Context) (telegram.Page, error) {
		t.Fatal("source called while writer is busy")
		return telegram.Page{}, nil
	})
	_, err = a.Crawl(t.Context())
	require.ErrorIs(t, err, store.ErrBusy)
	require.EqualError(t, err, "state_busy")
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: crawlTime})
	require.NoError(t, err)
	_, err = w.RecordMessage(t.Context(), state.MessageInput{RunID: run.ID, SourceID: "fixture", MessageID: "1", ObservedAt: crawlTime, Keys: []string{crawlKey, "second"}})
	require.NoError(t, err)
	calls := 0
	err = a.List(t.Context(), func(state.Candidate) error { calls++; return errors.New(crawlKey) })
	require.EqualError(t, err, "output_error")
	require.Equal(t, 1, calls)
}
