package app_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/diagnostic"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// covers: spec 0004 AC-4, AC-5, AC-6, AC-7, AC-9, AC-10.
// Real parsing and storage preserve earlier pages and never probe past the budget.
func TestCrawl_PageBudgetsAndLaterPageFailures(t *testing.T) {
	for _, tc := range []struct {
		name           string
		limit, failure int
		older          bool
		stop           string
		requests       int
	}{
		{"budget", 1, 0, true, "page_limit", 1},
		{"preview wins at budget", 1, 0, false, "preview_end", 1},
		{"two overlapping pages", 2, 0, true, "page_limit", 2},
		{"later page failure", 2, 500, true, "source_http_error", 3},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				a := crawlApp(t)
				a.Settings.Sources[0].PageLimit = tc.limit
				if tc.failure != 0 {
					a.Settings.Sources = append(
						a.Settings.Sources,
						config.Source{Channel: "later", Enabled: true, PageLimit: 1},
					)
				}
				var logs bytes.Buffer
				a.Logger = diagnostic.NewLogger(&logs, time.Now)
				var urls []string
				a.Source = telegram.New(
					"test", a.Clock, httpFunc(
						func(r *http.Request) (*http.Response, error) {
							urls = append(urls, r.URL.String())
							channel := strings.TrimPrefix(r.URL.Path, "/s/")
							before := r.URL.Query().Get("before")
							if before != "" {
								require.Equal(t, "7", before)
								if tc.failure != 0 {
									return &http.Response{
										StatusCode: tc.failure, Body: io.NopCloser(strings.NewReader(crawlKey)),
									}, nil
								}
							}
							post := func(id int, text string) string {
								return fmt.Sprintf(
									`<div class="tgme_widget_message" data-post="%s/%d"><div `+
										`class="tgme_widget_message_text">%s</div></div>`,
									channel,
									id,
									text,
								)
							}
							content := post(7, crawlKey) + post(8, "no candidate")
							cursor := 7
							if before != "" {
								content = post(6, crawlKey) + post(7, crawlKey)
								cursor = 6
							}
							if tc.older {
								content += fmt.Sprintf(
									`<div class="js-messages_more_wrap"><a class="tme_messages_more `+
										`js-messages_more" href="?before=%d"></a></div>`,
									cursor,
								)
							}
							body := `<div class="tgme_channel_info_header_username">@` + channel +
								`</div><div class="tgme_channel_history">` + content + `</div>`
							return &http.Response{
								StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}},
								Body: io.NopCloser(strings.NewReader(body)),
							}, nil
						},
					),
				)
				report, err := a.Crawl(t.Context())
				if tc.failure != 0 {
					require.EqualError(t, err, "source_failed")
					require.Equal(t, "completed", report.Sources[1].Status)
					require.Nil(t, report.Sources[0].Pages[1].FetchedAt)
				} else {
					require.NoError(t, err)
				}
				require.Len(t, urls, tc.requests)
				require.Equal(t, tc.stop, *report.Sources[0].StopReason)
				require.Equal(t, int64(1), report.SavedCandidates)
				require.Equal(t, 2, *report.Sources[0].Pages[0].ParsedPostCount)
				require.Equal(t, int64(1), report.Sources[0].Pages[0].SavedSightings)
				if tc.limit == 2 {
					require.Equal(t, int64(2), report.SavedSightings)
					if tc.failure == 0 {
						require.Equal(t, int64(1), report.Sources[0].Pages[1].SavedSightings)
					}
				}
				require.NotContains(t, logs.String(), crawlKey)
				a.Settings.Sources = nil
				saved, err := a.ShowCrawl(t.Context(), report.Run.ID)
				require.NoError(t, err)
				require.Equal(t, report, saved)
				r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
				require.NoError(t, err)
				checks, err := r.ListRuns(t.Context(), state.Validation, state.PageRequest{})
				require.NoError(t, err)
				require.Empty(t, checks.Items, "crawl must not create validation work")
				require.NoError(t, r.Close())
			},
		)
	}
}

type cancelBeforeNextRequest struct {
	cancel context.CancelFunc
	calls  int
}

func (s *cancelBeforeNextRequest) FetchPage(context.Context, string, *int64) (telegram.Page, error) {
	s.calls++
	return telegram.Page{Facts: state.CrawlPage{FetchedAt: crawlTime}}, nil
}

func (s *cancelBeforeNextRequest) RequestStartedAt() time.Time {
	// Cancellation at the external timing boundary is observed before any parse write.
	s.cancel()
	return time.Now()
}

// covers: spec 0004 AC-8. Cancellation saves explicit outcomes and the next run starts fresh.
func TestCrawl_CancellationDoesNotResumeThePreviousPlan(t *testing.T) {
	a := crawlApp(t)
	a.Settings.Sources = append(a.Settings.Sources, config.Source{Channel: "later", Enabled: true, PageLimit: 1})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := &cancelBeforeNextRequest{cancel: cancel}
	a.Source = source
	report, err := a.Crawl(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, source.calls)
	require.Equal(t, state.Cancelled, report.Run.Status)
	require.Equal(t, "cancelled", report.Sources[0].Pages[0].Status)
	require.Nil(t, report.Sources[0].Pages[0].FetchedAt)
	require.Equal(t, "skipped", report.Sources[1].Status)
	require.Equal(t, "cancelled", *report.Sources[1].StopReason)
	a.Settings.Sources = a.Settings.Sources[:1]
	a.Source = sourceFunc(
		func(context.Context) (telegram.Page, error) {
			return telegram.Page{Facts: state.CrawlPage{FetchedAt: crawlTime}}, nil
		},
	)
	next, err := a.Crawl(t.Context())
	require.NoError(t, err)
	require.NotEqual(t, report.Run.ID, next.Run.ID)
	require.Nil(t, next.Sources[0].Pages[0].BeforeID)
	saved, err := a.ShowCrawl(t.Context(), report.Run.ID)
	require.NoError(t, err)
	require.Equal(t, report, saved)
}

// Spec 0004 AC-4, AC-5, AC-7: durable requests, global pacing and isolated outcomes.
func TestMultipleSourceOutcomesAndRateLimitAbort(t *testing.T) {
	for _, status := range []int{500, 429} {
		t.Run(
			http.StatusText(status), func(t *testing.T) {
				a := crawlApp(t)
				a.Settings.Sources = []config.Source{
					{Channel: "first", Enabled: true, PageLimit: 1}, {Channel: "broken", Enabled: true, PageLimit: 1},
					{Channel: "last", Enabled: true, PageLimit: 1},
				}
				var starts []time.Time
				a.Source = telegram.New(
					"test", a.Clock, httpFunc(
						func(request *http.Request) (*http.Response, error) {
							starts = append(starts, time.Now())
							r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
							require.NoError(t, err)
							runs, err := r.ListRuns(t.Context(), state.Crawl, state.PageRequest{})
							require.NoError(t, err)
							report, err := r.GetCrawlReport(t.Context(), runs.Items[0].ID)
							require.NoError(t, err)
							require.Len(t, report.Sources, 3)
							require.Equal(t, "requested", report.Sources[len(starts)-1].Pages[0].Status)
							require.NoError(t, r.Close())
							channel := strings.TrimPrefix(request.URL.Path, "/s/")
							if channel == "broken" {
								return &http.Response{
									StatusCode: status, Body: io.NopCloser(strings.NewReader("private upstream text")),
								}, nil
							}
							body := `<div class="tgme_channel_info_header_username">@` + channel +
								`</div><div class="tgme_channel_history"><div ` +
								`class="tgme_widget_message" data-post="` + channel +
								`/1"><div class="tgme_widget_message_text">` + crawlKey + `</div></div></div>`
							return &http.Response{
								StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}},
								Body: io.NopCloser(strings.NewReader(body)),
							}, nil
						},
					),
				)
				report, err := a.Crawl(t.Context())
				require.Error(t, err)
				require.Equal(t, state.Failed, report.Run.Status)
				require.Equal(t, "completed", report.Sources[0].Status)
				require.Equal(t, "failed", report.Sources[1].Status)
				require.Equal(t, int64(1), report.SavedCandidates)
				if status == 429 {
					require.EqualError(t, err, "source_rate_limited")
					require.Len(t, starts, 2)
					require.Equal(t, "skipped", report.Sources[2].Status)
					require.Equal(t, "rate_limit_abort", *report.Sources[2].StopReason)
					require.Nil(t, report.Sources[2].StartedAt)
				} else {
					require.EqualError(t, err, "source_failed")
					require.Len(t, starts, 3)
					require.Equal(t, "completed", report.Sources[2].Status)
					require.Equal(t, int64(2), report.SavedSightings)
				}
				for i := 1; i < len(starts); i++ {
					require.GreaterOrEqual(t, starts[i].Sub(starts[i-1]), time.Second)
				}
			},
		)
	}
}

// Spec 0004 AC-8: a failed message mutation stops every subsequent mutation.
func TestMutationFailureLeavesChildrenForRecovery(t *testing.T) {
	a := crawlApp(t)
	a.Settings.Sources = append(a.Settings.Sources, config.Source{Channel: "later", Enabled: true, PageLimit: 1})
	calls := 0
	a.Source = sourceFunc(
		func(context.Context) (telegram.Page, error) {
			calls++
			n := int64(1)
			return telegram.Page{
				Facts: state.CrawlPage{
					FetchedAt: crawlTime, ParsedPostCount: 1, MinMessageID: &n, MaxMessageID: &n,
				}, Messages: []state.MessageInput{
					{
						SourceID: "telegram:warpplus", MessageID: "1", ObservedAt: crawlTime, Keys: []string{" "},
					},
				},
			}, nil
		},
	)
	report, err := a.Crawl(t.Context())
	require.Error(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, state.Running, report.Run.Status)
	require.Equal(t, "parsed", report.Sources[0].Pages[0].Status)
	require.Equal(t, "pending", report.Sources[1].Status)
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	saved, err := w.GetCrawlReport(t.Context(), report.Run.ID)
	require.NoError(t, err)
	require.Equal(t, state.Interrupted, saved.Run.Status)
	require.Equal(t, "interrupted", saved.Sources[0].Pages[0].Status)
	require.Nil(t, saved.Sources[0].Pages[0].FinishedAt)
	require.NotNil(t, saved.Sources[0].Pages[0].FetchedAt)
	require.Equal(t, "interrupted_before_start", *saved.Sources[1].StopReason)
}
