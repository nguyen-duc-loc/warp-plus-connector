// Package diagnostic provides bounded, private application log presentation.
package diagnostic

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/rs/zerolog"
)

// Field is one explicitly selected scalar diagnostic attribute.
type Field struct {
	Name  string
	Value any
}

// String constructs a string attribute.
func String(name, value string) Field { return Field{name, value} }

// Int constructs an integer attribute without floating point conversion.
func Int(name string, value int64) Field { return Field{name, value} }

// Bool constructs a boolean attribute.
func Bool(name string, value bool) Field { return Field{name, value} }

// Record is a validated log event. Service adapters supply original event times.
type Record struct {
	At             time.Time
	Level          zerolog.Level
	Event, Message string
	Fields         []Field
}

// NewLogger supplies event boundary timestamps without changing global zerolog settings.
func NewLogger(w io.Writer, clock func() time.Time) *zerolog.Logger {
	if clock == nil {
		clock = time.Now
	}
	l := zerolog.New(w).Hook(
		zerolog.HookFunc(
			func(e *zerolog.Event, _ zerolog.Level, _ string) {
				e.Str("time", clock().UTC().Format("2006-01-02T15:04:05.000000Z"))
			},
		),
	)
	return &l
}

// Emit writes only selected primitive fields. The receiving sink validates the event catalogue.
func Emit(l *zerolog.Logger, event string, fields ...Field) {
	if l == nil {
		return
	}
	r := Record{Event: event, Fields: fields}
	r.Level, r.Message = describe(r)
	emitRecord(l, r)
}

func emitRecord(l *zerolog.Logger, r Record) {
	event, fields := r.Event, r.Fields
	e := l.WithLevel(r.Level).Str("event", event)
	for _, f := range fields {
		switch v := f.Value.(type) {
		case string:
			e.Str(f.Name, v)
		case int64:
			e.Int64(f.Name, v)
		case bool:
			e.Bool(f.Name, v)
		default:
			e.Str("unsupported_attribute", "invalid")
		}
	}
	e.Msg(r.Message)
}

func (r Record) text(name string) string {
	for _, f := range r.Fields {
		if f.Name == name {
			v, _ := f.Value.(string)
			return v
		}
	}
	return ""
}
func (r Record) number(name string) int64 {
	for _, f := range r.Fields {
		if f.Name == name {
			v, _ := f.Value.(int64)
			return v
		}
	}
	return 0
}
func (r Record) boolean(name string) bool {
	for _, f := range r.Fields {
		if f.Name == name {
			v, _ := f.Value.(bool)
			return v
		}
	}
	return false
}

// CodeLevel classifies the closed operational code vocabulary.
func CodeLevel(code string) zerolog.Level {
	switch code {
	case "batch_admitted", "running", "completed", "preview_end", "page_limit",
		"schedule_starting", "schedule_resumed", "schedule_stopping", "schedule_stopped", "schedule_waiting":
		return zerolog.InfoLevel
	case "cancelled", "interrupted", "interrupted_before_start", "deadline_exceeded", "clock_paused",
		"cleanup_pending", "attempt_limit", "candidate_timeout", "backend_unavailable", "backend_busy",
		"cooldown", "clock_regressed", "probe_required", "rate_limited", "temporary_error", "timeout",
		"source_rate_limited", "source_timeout", "state_busy", "validation_batch_inconclusive",
		"validation_batch_stopped", "validation_cleanup_pending", "validation_interrupted",
		"validation_observation_timeout", "validation_rate_limited", "validation_timeout",
		"validator_backend_unavailable", "validator_busy", "validator_clock_regressed", "validator_cooldown",
		"validator_probe_required":
		return zerolog.WarnLevel
	default:
		return zerolog.ErrorLevel
	}
}

// ResultLevel uses saved status first, then unresolved validation counts.
func ResultLevel(status string, incomplete bool) zerolog.Level {
	switch status {
	case "failed":
		return zerolog.ErrorLevel
	case "cancelled", "interrupted":
		return zerolog.WarnLevel
	}
	if incomplete {
		return zerolog.WarnLevel
	}
	return zerolog.InfoLevel
}

func describe(r Record) (zerolog.Level, string) {
	code := r.text("code")
	switch r.Event {
	case "work_diagnostic":
		msg := "Work update"
		if code == "batch_admitted" {
			msg = "Validation started"
		}
		switch code {
		case "completed", "failed", "cancelled", "interrupted":
			if r.text("status") == code {
				msg = "Crawl " + code
			}
		}
		return CodeLevel(code), msg
	case "crawl_started":
		return zerolog.InfoLevel, "Crawl started"
	case "run_completed":
		incomplete := r.number("inconclusive") > 0 || r.number("unresolved") > 0 || r.number("unstarted") > 0
		return ResultLevel(r.text("status"), incomplete), "Validation " + r.text("status")
	case "crawl_page":
		if r.text("result") == "completed" {
			return zerolog.DebugLevel, "Source page saved"
		}
		if r.text("result") == "source_rate_limited" {
			return zerolog.ErrorLevel, "Source collection stopped"
		}
		return zerolog.WarnLevel, "Source page failed"
	case "attempt_admitted":
		return zerolog.DebugLevel, "Validation attempt started"
	case "attempt_completed", "mutation_completed":
		level := zerolog.DebugLevel
		if (r.text("outcome") == "" && r.text("code") != "") || r.text("outcome") == "inconclusive" ||
			!r.boolean("cleanup_proven") || !r.boolean("removed") {
			level = zerolog.WarnLevel
		}
		if r.Event == "mutation_completed" {
			return level, "Validator mutation completed"
		}
		return level, "Validation attempt completed"
	case "mutation_admitted":
		return zerolog.DebugLevel, "Validator mutation started"
	case "retry_planned":
		return zerolog.WarnLevel, "Validation retry scheduled"
	case "scheduler":
		messages := map[string]string{
			"schedule_starting": "Scheduler started", "schedule_stopping": "Scheduler stopping",
			"schedule_stopped": "Scheduler stopped", "schedule_waiting": "Scheduler waiting",
			"schedule_resumed": "Scheduler resumed", "clock_paused": "Scheduler paused for clock recovery",
		}
		if msg, ok := messages[code]; ok {
			return CodeLevel(code), msg
		}
		return CodeLevel(code), "Scheduler update"
	case "command_cancelled":
		return zerolog.WarnLevel, "Command cancelled"
	case "terminal_export":
		level := zerolog.InfoLevel
		if code != "completed" {
			level = zerolog.ErrorLevel
		}
		switch r.text("publication") {
		case "true":
			return level, "File created"
		case "false":
			return level, "This operation did not create a file"
		default:
			return zerolog.ErrorLevel, "File may have been created; check the destination before retrying"
		}
	case "command_error":
		return zerolog.ErrorLevel, "Command or output failed"
	}
	return zerolog.ErrorLevel, "Invalid diagnostic event"
}

type schema struct{ required, optional string }

var schemas = map[string]schema{
	"terminal_export": {"code output publication durability selected completed", "temporary_directory exit_code"},
	"work_diagnostic": {"run_id code", "result origin status selected saved_candidates saved_sightings"},
	"crawl_started":   {"run_id origin", ""},
	"run_completed": {
		"run_id origin status selected pending active completed skipped interrupted confirmed invalid " +
			"inconclusive unresolved unstarted", "code",
	},
	"crawl_page":       {"run_id source_id page_number result", ""},
	"attempt_admitted": {"run_id candidate_id attempt_id attempt_number active ceiling", ""},
	"attempt_completed": {
		"run_id candidate_id attempt_id attempt_number active ceiling elapsed cleanup_proven removed",
		"outcome failure_class code",
	},
	"retry_planned":     {"run_id candidate_id attempt_id attempt_number wait", "code failure_class"},
	"mutation_admitted": {"run_id candidate_id attempt_id operation", ""},
	"mutation_completed": {
		"run_id candidate_id attempt_id elapsed cleanup_proven removed",
		"outcome failure_class code",
	},
	"scheduler": {"code", ""},
	"command_error": {
		"code", "candidate_id published delivered output temporary_directory changed " +
			"operation_incomplete action installation_id state",
	},
	"command_cancelled": {"exit_code", ""},
}

var errRecord = errors.New("invalid diagnostic record")

// Decode validates a bounded zerolog transport record, retaining exact integers.
func Decode(p []byte) (Record, error) {
	var r Record
	if len(p) > 16*1024 {
		return r, errRecord
	}
	d := json.NewDecoder(bytes.NewReader(p))
	d.UseNumber()
	var raw map[string]any
	if err := d.Decode(&raw); err != nil {
		return r, errRecord
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return r, errRecord
	}
	at, ok := raw["time"].(string)
	if !ok {
		return r, errRecord
	}
	var err error
	r.At, err = time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return r, errRecord
	}
	r.Event, ok = raw["event"].(string)
	if !ok {
		return r, errRecord
	}
	s, ok := schemas[r.Event]
	if !ok {
		return r, errRecord
	}
	r.Message, ok = raw["message"].(string)
	if !ok {
		return r, errRecord
	}
	level, ok := raw["level"].(string)
	if !ok {
		return r, errRecord
	}
	r.Level, err = zerolog.ParseLevel(level)
	if err != nil || r.Level < zerolog.DebugLevel || r.Level > zerolog.ErrorLevel {
		return r, errRecord
	}
	delete(raw, "time")
	delete(raw, "event")
	delete(raw, "message")
	delete(raw, "level")
	for _, key := range strings.Fields(s.required) {
		if _, ok := raw[key]; !ok {
			return r, errRecord
		}
	}
	for key, value := range raw {
		if !contains(s.required+" "+s.optional, key) {
			return r, errRecord
		}
		if n, ok := value.(json.Number); ok {
			value, err = n.Int64()
			if err != nil {
				return r, errRecord
			}
		}
		if !validField(key, value) {
			return r, errRecord
		}
		r.Fields = append(r.Fields, Field{key, value})
	}
	if r.Event == "work_diagnostic" && r.text("result") != "" && r.text("result") != r.text("code") {
		return r, errRecord
	}
	if r.Event != "command_error" {
		expected, message := describe(r)
		if r.Level != expected || r.Message != message {
			return r, errRecord
		}
	}
	return r, nil
}
func contains(words, word string) bool {
	return word != "" && !strings.ContainsAny(word, " \t\r\n") && strings.Contains(" "+words+" ", " "+word+" ")
}

func validField(k string, v any) bool {
	switch k {
	case "run_id", "candidate_id", "attempt_id", "attempt_number", "page_number", "ceiling":
		n, ok := v.(int64)
		return ok && n > 0
	case "exit_code":
		n, ok := v.(int64)
		return ok && (n == 130 || n == 143)
	case "active", "saved_candidates", "saved_sightings", "selected", "pending", "completed", "skipped",
		"interrupted", "confirmed", "invalid", "inconclusive", "unresolved", "unstarted":
		n, ok := v.(int64)
		return ok && n >= 0
	case "cleanup_proven", "removed", "published", "delivered", "changed", "operation_incomplete":
		_, ok := v.(bool)
		return ok
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return false
	}
	switch k {
	case "publication":
		return contains("true false unknown", s)
	case "durability":
		return contains("not_published confirmed uncertain", s)
	case "code", "result":
		return SafeCode(s)
	case "origin":
		return contains("manual scheduled", s)
	case "status":
		return contains("running completed failed cancelled interrupted", s)
	case "operation":
		return contains("register attach", s)
	case "outcome":
		return contains("confirmed invalid inconclusive", s)
	case "failure_class":
		return contains("temporary throttled terminal unsafe", s)
	case "elapsed", "wait":
		d, e := time.ParseDuration(s)
		return e == nil && (k != "wait" || d > 0)
	case "source_id":
		if !strings.HasPrefix(s, "telegram:") {
			return false
		}
		channel := strings.TrimPrefix(s, "telegram:")
		normalized, ok := state.NormalizeChannel(channel)
		return ok && normalized == channel
	case "action":
		return contains("install start stop restart enable disable uninstall", s)
	}
	return true
}

// CommandError renders a mapped safe category, retaining only typed delivery evidence.
func CommandError(l *zerolog.Logger, code, explanation string, omitOutput bool, fields ...Field) {
	message := "Command failed"
	switch code {
	case "command_failed":
		message = "Command or output failed"
	case "arguments_invalid":
		message = "Invalid arguments, see command help"
		switch explanation {
		case "unknown command, see command help", "unknown flag, see command help",
			"unknown shorthand flag, see command help", "invalid argument, see command help",
			"flag needs an argument, see command help":
			message = explanation
		}
	case "configuration_invalid":
		message = "Invalid configuration"
	case "configuration_access":
		message = "Configuration access failed"
	case "output_error":
		message = "Output delivery failed"
	case "cancelled":
		message = "Command cancelled"
	case "ui_shutdown_timeout":
		message = "Terminal cleanup timed out; exiting"
	}
	if omitOutput {
		message += ", omit --output for stdout"
	}
	fields = append([]Field{String("code", code)}, fields...)
	emitRecord(l, Record{Event: "command_error", Level: zerolog.ErrorLevel, Message: message, Fields: fields})
}
