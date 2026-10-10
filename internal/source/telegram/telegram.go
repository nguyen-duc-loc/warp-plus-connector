// Package telegram reads the single public warpplus preview page.
package telegram

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"golang.org/x/net/html"
)

// Fixed request identity and decoded body bound for this source.
const (
	SourceID   = "telegram:warpplus"
	PreviewURL = "https://t.me/s/warpplus"
	BodyLimit  = 2 * 1024 * 1024
)

// Error exposes a stable reason without upstream text or content.
type Error struct {
	Reason string
	cause  error
}

func (e *Error) Error() string { return e.Reason }
func (e *Error) Unwrap() error { return e.cause }

// Page contains fully validated posts and observed coverage, never raw HTML.
type Page struct {
	Messages []state.MessageInput
	Facts    state.CrawlPage
}

// Adapter owns a dedicated HTTP client, with transport replacement for fixtures.
type Adapter struct {
	client  *http.Client
	clock   func() time.Time
	version string
}

// New constructs a bounded client. A nil transport uses the standard transport.
func New(version string, clock func() time.Time, transport http.RoundTripper) *Adapter {
	if version == "" {
		version = "dev"
	}
	if transport == nil {
		// A dedicated fresh connection avoids the standard transport's automatic
		// replay of an idempotent GET when a reused connection fails.
		dedicated := http.DefaultTransport.(*http.Transport).Clone()
		dedicated.DisableKeepAlives = true
		transport = dedicated
	}
	return &Adapter{clock: clock, version: version, client: &http.Client{
		Transport: transport, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Fetch retrieves and fully parses one page, without retries or pagination.
func (a *Adapter) Fetch(ctx context.Context) (Page, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var zero Page
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, PreviewURL, nil)
	if err != nil {
		return zero, sourceError(ctx, "source_transport_error", err)
	}
	req.Header.Set("User-Agent", "wpc/"+a.version)
	resp, err := a.client.Do(req)
	if err != nil {
		return zero, sourceError(ctx, "source_transport_error", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return zero, &Error{Reason: "source_http_error"}
	}
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "text/html" {
		return zero, &Error{Reason: "source_parse_error"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, BodyLimit+1))
	if err != nil {
		return zero, sourceError(ctx, "source_transport_error", err)
	}
	if len(body) > BodyLimit {
		return zero, &Error{Reason: "source_body_too_large"}
	}
	at := a.clock().UTC().Truncate(time.Microsecond)
	page, err := parse(body, at)
	if ctx.Err() != nil {
		return zero, sourceError(ctx, "source_transport_error", ctx.Err())
	}
	return page, err
}

func sourceError(ctx context.Context, reason string, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return &Error{Reason: "cancelled", cause: context.Canceled}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Reason: "source_timeout", cause: context.DeadlineExceeded}
	}
	return &Error{Reason: reason}
}

var tokenPattern = regexp.MustCompile(`[A-Za-z0-9]{8}-[A-Za-z0-9]{8}-[A-Za-z0-9]{8}`)
var postPattern = regexp.MustCompile(`^warpplus/([0-9]+)$`)

func parse(body []byte, at time.Time) (Page, error) {
	bad := &Error{Reason: "source_parse_error"}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Page{}, bad
	}
	identity := doc.Find(".tgme_channel_info_header_username")
	if identity.Length() != 1 || strings.TrimSpace(identity.Text()) != "@warpplus" {
		return Page{}, bad
	}
	feed := doc.Find(".tgme_channel_history")
	if feed.Length() != 1 {
		return Page{}, bad
	}
	out := Page{Facts: state.CrawlPage{FetchedAt: at}}
	seen := map[int64]bool{}
	var parseErr error
	// Only direct feed entries are source posts. Nested quoted widgets cannot
	// acquire source provenance merely through a data-post attribute.
	posts := feed.ChildrenFiltered(".tgme_widget_message")
	feed.ChildrenFiltered(".tgme_widget_message_wrap").Each(func(_ int, wrap *goquery.Selection) {
		entry := wrap.ChildrenFiltered(".tgme_widget_message")
		if entry.Length() != 1 {
			parseErr = bad
		}
		posts = posts.AddSelection(entry)
	})
	if parseErr != nil {
		return Page{}, parseErr
	}
	if posts.Length() == 0 && (feed.Children().Length() != 0 || strings.TrimSpace(feed.Text()) != "") {
		return Page{}, bad
	}
	posts.Each(func(_ int, s *goquery.Selection) {
		if parseErr != nil {
			return
		}
		match := postPattern.FindStringSubmatch(s.AttrOr("data-post", ""))
		if len(match) != 2 {
			parseErr = bad
			return
		}
		id, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || id <= 0 || seen[id] {
			parseErr = bad
			return
		}
		seen[id] = true
		messageID := strconv.FormatInt(id, 10)
		url := "https://t.me/warpplus/" + messageID
		message := state.MessageInput{SourceID: SourceID, MessageID: messageID, MessageURL: &url, ObservedAt: at}
		text := strings.Builder{}
		s.Find(".tgme_widget_message_text").Each(func(_ int, part *goquery.Selection) {
			if part.Closest(".tgme_widget_message").Get(0) != s.Get(0) || hiddenAncestors(part.Get(0)) {
				return
			}
			text.WriteByte(' ')
			visibleText(&text, part.Get(0))
			text.WriteByte(' ')
		})
		message.Keys = extract(text.String())
		s.Find(".tgme_widget_message_date time[datetime]").EachWithBreak(func(_ int, date *goquery.Selection) bool {
			if date.Closest(".tgme_widget_message").Get(0) != s.Get(0) {
				return true
			}
			if t, err := time.Parse(time.RFC3339Nano, date.AttrOr("datetime", "")); err == nil {
				t = t.UTC().Truncate(time.Microsecond)
				message.PublishedAt = &t
			}
			return false
		})
		out.Messages = append(out.Messages, message)
	})
	if parseErr != nil {
		return Page{}, parseErr
	}
	sort.Slice(out.Messages, func(i, j int) bool {
		a, _ := strconv.ParseInt(out.Messages[i].MessageID, 10, 64)
		b, _ := strconv.ParseInt(out.Messages[j].MessageID, 10, 64)
		return a < b
	})
	out.Facts.ParsedPostCount = len(out.Messages)
	if len(out.Messages) > 0 {
		minID, _ := strconv.ParseInt(out.Messages[0].MessageID, 10, 64)
		maxID, _ := strconv.ParseInt(out.Messages[len(out.Messages)-1].MessageID, 10, 64)
		out.Facts.MinMessageID, out.Facts.MaxMessageID = &minID, &maxID
	}
	return out, nil
}

func hidden(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "script", "style", "template", "noscript":
		return true
	}
	for _, a := range n.Attr {
		if a.Key == "hidden" || (a.Key == "aria-hidden" && strings.EqualFold(a.Val, "true")) {
			return true
		}
		if a.Key == "style" {
			for _, declaration := range strings.Split(strings.ToLower(a.Val), ";") {
				name, value, ok := strings.Cut(declaration, ":")
				value = strings.TrimSpace(strings.Split(value, "!")[0])
				if ok && ((strings.TrimSpace(name) == "display" && value == "none") || (strings.TrimSpace(name) == "visibility" && (value == "hidden" || value == "collapse"))) {
					return true
				}
			}
		}
	}
	return false
}

func hiddenAncestors(n *html.Node) bool {
	for ; n != nil; n = n.Parent {
		if hidden(n) {
			return true
		}
	}
	return false
}

func visibleText(b *strings.Builder, n *html.Node) {
	if hidden(n) {
		b.WriteByte(' ')
		return
	}
	if n.Type == html.ElementNode {
		for _, a := range n.Attr {
			if a.Key == "class" {
				for _, c := range strings.Fields(a.Val) {
					if c == "tgme_widget_message" {
						b.WriteByte(' ')
						return
					}
				}
			}
		}
	}
	block := false
	if n.Type == html.ElementNode {
		switch n.Data {
		case "address", "article", "aside", "blockquote", "br", "div", "dl", "dt", "dd", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "nav", "ol", "p", "pre", "section", "table", "tbody", "td", "th", "tr", "ul":
			block = true
		}
	}
	if block {
		b.WriteByte(' ')
	}
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		visibleText(b, c)
	}
	if block {
		b.WriteByte(' ')
	}
}

func extract(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, pos := range tokenPattern.FindAllStringIndex(text, -1) {
		if pos[0] > 0 {
			r, _ := utf8.DecodeLastRuneInString(text[:pos[0]])
			if boundary(r) {
				continue
			}
		}
		if pos[1] < len(text) {
			r, _ := utf8.DecodeRuneInString(text[pos[1]:])
			if boundary(r) {
				continue
			}
		}
		key := text[pos[0]:pos[1]]
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func boundary(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' }
