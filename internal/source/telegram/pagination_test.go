package telegram_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"github.com/stretchr/testify/require"
)

// Spec 0004 AC-5 and AC-9: sanitized live controls establish both page grammars.
func TestLiveNavigationFixtures(t *testing.T) {
	var before *int64
	for _, name := range []string{"live_latest.html", "live_older.html"} {
		body, err := os.ReadFile("testdata/" + name)
		require.NoError(t, err)
		adapter := telegram.New(
			"test", func() time.Time { return observed }, transport(
				func(r *http.Request) (*http.Response, error) {
					require.Equal(t, "/s/warpplus", r.URL.Path)
					if before != nil {
						require.Equal(t, fmt.Sprint(*before), r.URL.Query().Get("before"))
					}
					return &http.Response{
						StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}},
						Body: io.NopCloser(strings.NewReader(string(body))),
					}, nil
				},
			),
		)
		page, err := adapter.FetchPage(t.Context(), "WarpPlus", before)
		require.NoError(t, err)
		require.Equal(t, 20, page.Facts.ParsedPostCount)
		require.NotNil(t, page.NextBeforeID)
		if before != nil {
			require.Less(t, *page.NextBeforeID, *before)
		}
		before = page.NextBeforeID
	}
}

// covers: spec 0004 AC-5, AC-9. Only dedicated, consistent older controls count.
func TestFetchPage_NavigationContract(t *testing.T) {
	control := func(href, attrs string) string {
		return `<div class="js-messages_more_wrap"><a class="tme_messages_more ` +
			`js-messages_more" href="` + href + `" ` + attrs + `></a></div>`
	}
	for _, tc := range []struct {
		name, content string
		before, next  int64
		bad           bool
	}{
		{"relative cursor", post("7", "text") + control("?before=0007", `data-before="7"`), 0, 7, false},
		{"absolute cursor", post("7", "text") + control("https://t.me/s/warpplus?before=7", ""), 0, 7, false},
		{"matching controls", post("7", "text") + control("?before=7", "") + control("?before=007", ""), 0, 7, false},
		{"conflicting controls", post("7", "text") + control("?before=7", "") + control("?before=6", ""), 0, 0, true},
		{"conflicting attribute", post("7", "text") + control("?before=7", `data-before="6"`), 0, 0, true},
		{"both directions", post("7", "text") + control("?before=7", `data-before="7" data-after="8"`), 0, 0, true},
		{"newer only", post("7", "text") + control("?after=7", `data-after="7"`), 0, 0, false},
		{"empty preview", "", 0, 0, false},
		{"empty preview with cursor", control("?before=7", ""), 0, 0, true},
		{"no guessed cursor", post("7", "text"), 0, 0, false},
		{"ordinary post link", post("7", `<a href="?before=7">older</a>`), 0, 0, false},
		{"nested dedicated link", post("7", control("?before=7", "")), 0, 0, false},
		{
			"malformed control", post("7", "text") + `<div class="js-messages_more_wrap"><span>older</span></div>`, 0,
			0, true,
		},
		{
			"missing href", post(
				"7",
				"text",
			) + `<div class="js-messages_more_wrap"><a class="tme_messages_more ` +
				`js-messages_more"></a></div>`, 0, 0, true,
		},
		{"equal later cursor", post("7", "text") + control("?before=7", ""), 7, 0, true},
		{"progressing later cursor", post("6", "text") + control("?before=6", ""), 7, 6, false},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				calls := 0
				adapter := telegram.New(
					"test", func() time.Time { return observed }, transport(
						func(*http.Request) (*http.Response, error) {
							calls++
							return &http.Response{
								StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}},
								Body: io.NopCloser(strings.NewReader(feed(tc.content))),
							}, nil
						},
					),
				)
				var before *int64
				if tc.before != 0 {
					before = &tc.before
				}
				page, err := adapter.FetchPage(t.Context(), "warpplus", before)
				require.Equal(t, 1, calls)
				if tc.bad {
					require.EqualError(t, err, "source_pagination_error")
					require.Equal(t, telegram.Page{}, page, "invalid navigation must discard all parsed content")
					return
				}
				require.NoError(t, err)
				if tc.next == 0 {
					require.Nil(t, page.NextBeforeID)
				} else {
					require.Equal(t, tc.next, *page.NextBeforeID)
				}
			},
		)
	}
}

// covers: spec 0004 AC-5, AC-6. Generalized requests retain canonical provenance.
func TestFetchPage_NormalizesCustomChannelsAndRejectsUnsafeRequests(t *testing.T) {
	calls := 0
	adapter := telegram.New(
		"test", func() time.Time { return observed }, transport(
			func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "https://t.me/s/custom_1?before=9", r.URL.String())
				deadline, ok := r.Context().Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), 20*time.Second)
				require.Greater(t, time.Until(deadline), time.Duration(0))
				body := strings.ReplaceAll(feed(post("0007", "ABCDEFGH-12345678-AbCdEf01")), "warpplus", "CuStOm_1")
				return &http.Response{
					StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}},
					Body: io.NopCloser(strings.NewReader(body)),
				}, nil
			},
		),
	)
	before := int64(9)
	page, err := adapter.FetchPage(t.Context(), "CUSTOM_1", &before)
	require.NoError(t, err)
	require.Equal(t, "telegram:custom_1", page.Messages[0].SourceID)
	require.Equal(t, "7", page.Messages[0].MessageID)
	require.Equal(t, "https://t.me/custom_1/7", *page.Messages[0].MessageURL)
	for _, channel := range []string{"", "@custom", "../custom", "https://t.me/custom", "custom secret"} {
		_, err = adapter.FetchPage(t.Context(), channel, nil)
		require.EqualError(t, err, "source_pagination_error")
	}
	for _, cursor := range []int64{0, -1} {
		_, err = adapter.FetchPage(t.Context(), "custom_1", &cursor)
		require.EqualError(t, err, "source_pagination_error")
	}
	require.Equal(t, 1, calls, "invalid inputs must not contact a source")
}

// Spec 0004 AC-5: hostile navigation never becomes a request destination.
func TestPaginationRejectsUnsafeAndNonprogressingControls(t *testing.T) {
	for _, link := range []string{
		"https://evil.test/s/warpplus?before=7", "http://t.me/s/warpplus?before=7",
		"https://t.me:443/s/warpplus?before=7", "https://u@t.me/s/warpplus?before=7", "/s/other?before=7",
		"?before=7&before=7", "?before=7&x=1", "?before=0", "?before=-1", "?before=8", "?before=7#",
		"?before=9223372036854775808",
	} {
		t.Run(
			link, func(t *testing.T) {
				body := feed(
					`<div class="js-messages_more_wrap"><a class="tme_messages_more ` +
						`js-messages_more" href="` + link + `"></a></div>` + post(
						"7",
						"text",
					),
				)
				adapter := telegram.New(
					"test", func() time.Time { return observed }, transport(
						func(*http.Request) (*http.Response, error) {
							return &http.Response{
								StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}},
								Body: io.NopCloser(strings.NewReader(body)),
							}, nil
						},
					),
				)
				_, err := adapter.FetchPage(t.Context(), "warpplus", nil)
				require.EqualError(t, err, "source_pagination_error")
			},
		)
	}
}
