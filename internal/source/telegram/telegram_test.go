package telegram_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"github.com/stretchr/testify/require"
)

const key = "ABCDEFGH-12345678-AbCdEf01"

var observed = time.Date(2026, 10, 10, 12, 0, 0, 123456789, time.FixedZone("fixture", 3600))

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func feed(posts string) string {
	return `<div class="tgme_channel_info_header_username">@warpplus</div><div ` +
		`class="tgme_channel_history">` + posts + `</div>`
}

func post(id, text string) string {
	return `<div class="tgme_widget_message" data-post="warpplus/` + id +
		`"><div class="tgme_widget_message_text">` + text + `</div></div>`
}

func fetch(t *testing.T, body string) (telegram.Page, error) {
	t.Helper()
	a := telegram.New(
		"test", func() time.Time { return observed }, transport(
			func(r *http.Request) (*http.Response, error) {
				require.Equal(t, telegram.PreviewURL, r.URL.String())
				return &http.Response{
					StatusCode: 200, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
					Body: io.NopCloser(strings.NewReader(body)),
				}, nil
			},
		),
	)
	return a.Fetch(t.Context())
}

// covers: spec 0003 AC-1, AC-2. Only visible source text supplies exact keys.
func TestFetch_ExtractsVisibleKeysAndCanonicalProvenance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, text string
		want       []string
	}{
		{"duplicates and case", key + " " + key + " " + strings.ToLower(key), []string{key, strings.ToLower(key)}},
		{"inline", "ABCDEFGH-<b>12345678</b>-AbCdEf01", []string{key}},
		{"block boundary", "ABCDEFGH-<div>12345678</div>-AbCdEf01", nil},
		{"line boundary", "ABCDEFGH-<br>12345678-AbCdEf01", nil},
		{"unicode and token boundaries", "é" + key + " " + key + "字 _" + key + " " + key + "-", nil},
		{"entities", "&quot;" + key + "&quot;", []string{key}},
		{"attributes", `<a href="` + key + `">link</a>`, nil},
		{
			"hidden",
			`<script>` + key + `</script><span hidden>` + key + `</span><i aria-hidden="true">` +
				key + `</i><b style="DISPLAY: none !important">` + key + `</b>`,
			nil,
		},
		{"nested widget", post("999", key), nil},
		{"media only", `<img alt="` + key + `">`, nil},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				page, err := fetch(t, feed(post("00042", tc.text)))
				require.NoError(t, err)
				require.Len(t, page.Messages, 1)
				m := page.Messages[0]
				require.Equal(t, tc.want, m.Keys)
				require.Equal(t, "42", m.MessageID)
				require.Equal(t, telegram.SourceID, m.SourceID)
				require.Equal(t, "https://t.me/warpplus/42", *m.MessageURL)
				require.Equal(t, observed.UTC().Truncate(time.Microsecond), m.ObservedAt)
				require.Nil(t, m.PublishedAt)
				require.Equal(t, 1, page.Facts.ParsedPostCount)
			},
		)
	}
}

// covers: spec 0003 AC-1, AC-2, AC-6. Empty feeds differ from unknown markup.
func TestFetch_ValidatesCompletePageBeforeReturningMessages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body string }{
		{"missing identity", `<div class="tgme_channel_history"></div>`},
		{"wrong channel", strings.Replace(feed(""), "@warpplus", "@other", 1)},
		{"unknown markup", feed(`<section>changed markup</section>`)},
		{"duplicate ID", feed(post("1", key) + post("01", key))},
		{"zero ID", feed(post("0", key))},
		{"overflow ID", feed(post("9223372036854775808", key))},
		{"malformed ID", feed(post("1x", key))},
		{"missing wrapped post", feed(`<div class="tgme_widget_message_wrap"></div>`)},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				page, err := fetch(t, tc.body)
				require.EqualError(t, err, "source_parse_error")
				require.Empty(t, page.Messages)
				require.Zero(t, page.Facts.FetchedAt)
			},
		)
	}
	page, err := fetch(t, feed(""))
	require.NoError(t, err)
	require.Zero(t, page.Facts.ParsedPostCount)
	require.Nil(t, page.Facts.MinMessageID)
	require.Nil(t, page.Facts.MaxMessageID)
	page, err = fetch(
		t,
		feed(
			post("20", "")+strings.Replace(
				post("3", key),
				"</div></div>",
				`</div><a class="tgme_widget_message_date"><time `+
					`datetime="2026-10-10T08:00:00+02:00"></time></a></div>`,
				1,
			),
		),
	)
	require.NoError(t, err)
	require.Equal(t, 2, page.Facts.ParsedPostCount)
	require.Equal(t, int64(3), *page.Facts.MinMessageID)
	require.Equal(t, int64(20), *page.Facts.MaxMessageID)
	require.Equal(t, "3", page.Messages[0].MessageID)
	require.Equal(t, time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC), *page.Messages[0].PublishedAt)
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

// covers: spec 0003 AC-6. Requests have one deadline, no replay, and safe errors.
func TestFetch_BoundsRequestsAndSanitizesFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		status             int
		mime, body, reason string
		err                error
	}{
		{"success", 200, "text/html", feed(""), "", nil},
		{"redirect", 302, "text/html", "private response", "source_http_error", nil},
		{"server error", 503, "text/html", key, "source_http_error", nil},
		{"wrong MIME", 200, "application/json", key, "source_parse_error", nil},
		{"oversized", 200, "text/html", strings.Repeat(" ", telegram.BodyLimit+1), "source_body_too_large", nil},
		{"exact limit", 200, "text/html", feed("") + strings.Repeat(" ", telegram.BodyLimit-len(feed(""))), "", nil},
		{"transport", 0, "", "", "source_transport_error", errors.New(key)},
		{"deadline", 0, "", "", "source_timeout", context.DeadlineExceeded},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				calls := 0
				body := &trackedBody{Reader: strings.NewReader(tc.body)}
				a := telegram.New(
					"test", func() time.Time { return observed }, transport(
						func(r *http.Request) (*http.Response, error) {
							calls++
							require.Equal(t, http.MethodGet, r.Method)
							require.Equal(t, telegram.PreviewURL, r.URL.String())
							require.Equal(t, "wpc/test", r.UserAgent())
							deadline, ok := r.Context().Deadline()
							require.True(t, ok)
							require.InDelta(t, 20, time.Until(deadline).Seconds(), 1)
							if tc.err != nil {
								return nil, tc.err
							}
							return &http.Response{
								StatusCode: tc.status, Header: http.Header{
									"Content-Type": {tc.mime}, "Location": {"https://example.invalid/secret"},
								}, Body: body,
							}, nil
						},
					),
				)
				_, err := a.Fetch(t.Context())
				if tc.reason == "" {
					require.NoError(t, err)
				} else {
					require.EqualError(t, err, tc.reason)
					require.NotContains(t, err.Error(), key)
					if tc.err == context.DeadlineExceeded {
						require.ErrorIs(t, err, context.DeadlineExceeded)
					}
				}
				require.Equal(t, 1, calls)
				require.Equal(t, tc.err == nil, body.closed)
			},
		)
	}
}

// covers: spec 0003 AC-6. Caller cancellation survives source classification.
func TestFetch_PreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	a := telegram.New(
		"", func() time.Time { return observed }, transport(
			func(r *http.Request) (*http.Response, error) {
				cancel()
				return nil, r.Context().Err()
			},
		),
	)
	_, err := a.Fetch(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualError(t, err, "cancelled")
}
