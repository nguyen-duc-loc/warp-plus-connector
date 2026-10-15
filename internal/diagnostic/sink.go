package diagnostic

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
)

// Options are invocation flags, independent of application configuration.
type Options struct{ Level, Format, Color string }

// Validate checks presentation before application work.
func (o Options) Validate() error {
	if !contains("debug info warn error", o.Level) || !contains("auto pretty text json", o.Format) ||
		!contains("auto never", o.Color) {
		return errRecord
	}
	return nil
}

// Capabilities describe the original output destination.
type Capabilities struct {
	Terminal      bool
	Term, NoColor string
}

// Detect examines the actual destination before any wrappers are installed.
func Detect(w io.Writer) Capabilities {
	c := Capabilities{Term: os.Getenv("TERM"), NoColor: os.Getenv("NO_COLOR")}
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		c.Terminal = isatty.IsTerminal(f.Fd())
	}
	return c
}

// Resolve selects presentation and styling independently for each stream.
func (o Options) Resolve(c Capabilities) (string, bool) {
	format := o.Format
	if format == "auto" {
		format = "text"
		if c.Terminal && c.Term != "dumb" {
			format = "pretty"
		}
	}
	return format, format == "pretty" && c.Terminal && c.Term != "dumb" && c.NoColor == "" && o.Color != "never"
}

// Capture serializes transport consumption and contains zerolog fallback errors.
// Its failure callback runs once, after the lock is released, and should only cancel work.
type Capture struct {
	mu      sync.Mutex
	consume func([]byte) error
	cancel  func()
	err     error
}

// NewCapture constructs a contained transport writer with no event filtering.
func NewCapture(consume func([]byte) error, cancel func()) *Capture {
	return &Capture{consume: consume, cancel: cancel}
}

// Write intentionally acknowledges transport bytes even after failure. Err remains authoritative.
func (c *Capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	var notify func()
	if c.err == nil {
		c.err = c.consume(p)
		if c.err != nil {
			notify = c.cancel
		}
	}
	c.mu.Unlock()
	if notify != nil {
		notify()
	}
	return len(p), nil
}

// Err returns the first formatting or delivery failure.
func (c *Capture) Err() error { c.mu.Lock(); defer c.mu.Unlock(); return c.err }

// Sink is one serialized foreground stream and its date context.
type Sink struct {
	*Capture
	writer   io.Writer
	format   string
	color    bool
	minimum  zerolog.Level
	location *time.Location
	context  string
}

// NewSink constructs presentation without writing any bytes.
func NewSink(w io.Writer, o Options, c Capabilities, location *time.Location, cancel func()) *Sink {
	if location == nil {
		location = time.Local
	}
	format, color := o.Resolve(c)
	minimum, _ := zerolog.ParseLevel(o.Level)
	s := &Sink{writer: w, format: format, color: color, minimum: minimum, location: location}
	s.Capture = NewCapture(
		func(p []byte) error {
			r, err := Decode(p)
			if err != nil {
				return err
			}
			if !s.Visible(r.Level) &&
				r.Event != "command_cancelled" && r.Event != "terminal_export" && r.Event != "terminal_work" {
				return nil
			}
			if s.format == "json" {
				return writeAll(s.writer, p)
			}
			return s.render(r)
		}, cancel,
	)
	return s
}

// Visible applies the selected minimum level without changing archive selection.
func (s *Sink) Visible(level zerolog.Level) bool { return level >= s.minimum }

// Format reports the resolved format.
func (s *Sink) Format() string { return s.format }

// Display renders an accepted historical record and returns failures to its reader.
func (s *Sink) Display(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if !s.Visible(r.Level) {
		return nil
	}
	s.err = s.render(r)
	return s.err
}

// Notice writes untimed archive metadata through the same failure latch.
func (s *Sink) Notice(p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = writeAll(s.writer, p)
	}
	return s.err
}

func writeAll(w io.Writer, p []byte) error {
	n, err := w.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return err
}

var firstFields = []string{
	"run_id", "candidate_id", "attempt_id", "attempt_number", "source_id", "page_number",
	"channel", "lane", "origin", "status", "code", "result",
}
var lastFields = []string{"installation_id", "invocation_id", "sequence", "stream"}

func orderFields(fields []Field) []Field {
	out := append([]Field(nil), fields...)
	rank := func(name string) int {
		for i, n := range firstFields {
			if n == name {
				return i
			}
		}
		for i, n := range lastFields {
			if n == name {
				return 100 + i
			}
		}
		return 50
	}
	sort.Slice(
		out, func(i, j int) bool {
			a, b := rank(out[i].Name), rank(out[j].Name)
			if a == b {
				return out[i].Name < out[j].Name
			}
			return a < b
		},
	)
	return out
}

// Quote escapes dynamic text, retaining the former structured text quoting rules.
func Quote(s string) string {
	if s == "" {
		return strconv.Quote(s)
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '=' || r == '"' || r == '\\' {
			return strconv.Quote(s)
		}
	}
	return s
}
func safeMessage(s string) string {
	for _, r := range s {
		if unicode.IsControl(r) {
			return strconv.Quote(s)
		}
	}
	return s
}
func valueText(v any) string {
	switch n := v.(type) {
	case string:
		return Quote(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case bool:
		return strconv.FormatBool(n)
	default:
		return "invalid"
	}
}
func (s *Sink) style(value, code string) string {
	if !s.color {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}
func (s *Sink) render(r Record) error {
	var b bytes.Buffer
	at := r.At.In(s.location)
	fields := orderFields(r.Fields)
	if s.format == "text" {
		fmt.Fprintf(
			&b, "time=%s level=%s msg=%s", at.Format("2006-01-02T15:04:05.000-07:00"),
			strings.ToUpper(r.Level.String()), Quote(r.Message),
		)
		for _, f := range fields {
			fmt.Fprintf(&b, " %s=%s", f.Name, valueText(f.Value))
		}
		b.WriteByte('\n')
	} else {
		context := fmt.Sprintf(
			"Date %s | Zone %s | UTC%s", at.Format("2006-01-02"),
			safeMessage(s.location.String()), at.Format("-07:00"),
		)
		if context != s.context {
			b.WriteString(context + "\n")
		}
		// ConsoleWriter owns the row. Explicit callbacks prevent defaults, abbreviation,
		// floating point conversion, and dynamic terminal control characters.
		row := zerolog.ConsoleWriter{
			Out: &b, NoColor: true, PartsOrder: []string{"time", "level", "message"},
			FormatTimestamp: func(any) string { return s.style(at.Format("15:04:05.000"), "2") },
			FormatLevel: func(any) string {
				colors := map[zerolog.Level]string{
					zerolog.DebugLevel: "36", zerolog.InfoLevel: "32",
					zerolog.WarnLevel: "33", zerolog.ErrorLevel: "31",
				}
				return s.style(fmt.Sprintf("%-6s", strings.ToUpper(r.Level.String())), colors[r.Level])
			},
			FormatMessage: func(any) string { return safeMessage(r.Message) },
			FormatExtra: func(_ map[string]any, b *bytes.Buffer) error {
				for _, f := range fields {
					b.WriteByte(' ')
					b.WriteString(s.style(f.Name+"="+valueText(f.Value), "2"))
				}
				return nil
			},
		}
		if _, err := row.Write([]byte(`{}`)); err != nil {
			return err
		}
		s.context = context
	}
	return writeAll(s.writer, b.Bytes())
}
