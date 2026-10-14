package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/diagnostic"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/service"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

type serviceDisplay struct {
	sink   *diagnostic.Sink
	writer io.Writer
	json   bool
}

func newServiceDisplay(cmd *cobra.Command, format string) *serviceDisplay {
	o := presentation(cmd)
	o.Format = format
	w := cmd.OutOrStdout()
	return &serviceDisplay{
		sink: diagnostic.NewSink(w, o, diagnostic.Detect(w), time.Local, nil), writer: w, json: format == "json",
	}
}
func (d *serviceDisplay) notice(b *bytes.Buffer) error {
	if d.sink.Notice(b.Bytes()) != nil {
		return &app.Error{Code: "output_error"}
	}
	return nil
}
func (d *serviceDisplay) snapshot(v service.LogView, prefix string) error {
	if d.json {
		return deliverService(d.writer, "json", v)
	}
	displayed := 0
	for _, entry := range v.Entries {
		if d.sink.Visible(serviceRecord(entry).Level) {
			displayed++
		}
	}
	var b bytes.Buffer
	if d.sink.Format() == "text" {
		metadata := map[string]any{
			"schema_version": v.SchemaVersion, "view": v.View, "requested_lines": v.RequestedLines,
			"returned": v.Returned, "older_available": v.OlderAvailable, "truncated_tail": v.TruncatedTail,
			"continuity_unknown": v.ContinuityUnknown, "displayed": displayed, "filtered": v.Returned - displayed,
		}
		if err := renderDiagnosticText(&b, prefix, metadata); err != nil {
			return err
		}
	} else {
		if v.Returned == 0 {
			b.WriteString("No service log entries\n")
		} else if displayed == 0 {
			b.WriteString("No entries at this level\n")
		}
		fmt.Fprintf(
			&b,
			"requested_lines=%d returned=%d displayed=%d filtered=%d "+
				"older_available=%t truncated_tail=%t continuity_unknown=%t\n",
			v.RequestedLines,
			v.Returned,
			displayed,
			v.Returned-displayed,
			v.OlderAvailable,
			v.TruncatedTail,
			v.ContinuityUnknown,
		)
		if v.OlderAvailable {
			b.WriteString("Older entries are available outside this selection\n")
		}
		if v.TruncatedTail {
			b.WriteString("Incomplete current tail was not included\n")
		}
		if v.ContinuityUnknown {
			b.WriteString("Archive continuity is unknown\n")
		}
	}
	if err := d.notice(&b); err != nil {
		return err
	}
	for i, entry := range v.Entries {
		name := "entries[" + strconv.Itoa(i) + "]"
		if prefix != "" {
			name = prefix + "." + name
		}
		if err := d.entry(entry, name); err != nil {
			return err
		}
	}
	return nil
}
func (d *serviceDisplay) entry(e service.Entry, prefix string) error {
	r := serviceRecord(e)
	if !d.sink.Visible(r.Level) {
		return nil
	}
	if d.sink.Format() == "pretty" {
		if err := d.sink.Display(r); err != nil {
			return &app.Error{Code: "output_error"}
		}
		return nil
	}
	// Keep the original selected archive index in flattened text.
	data, err := json.Marshal(e)
	if err != nil {
		return &app.Error{Code: "output_error"}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return &app.Error{Code: "output_error"}
	}
	var b bytes.Buffer
	if err := renderDiagnosticText(&b, prefix, value); err != nil {
		return err
	}
	return d.notice(&b)
}
func (d *serviceDisplay) envelope(e service.LogEnvelope) error {
	if d.json {
		return deliverService(d.writer, "json", e)
	}
	switch e.Type {
	case "snapshot":
		if d.sink.Format() == "text" {
			var b bytes.Buffer
			fmt.Fprintf(&b, "schema_version=%d\ntype=snapshot\n", e.SchemaVersion)
			if err := d.notice(&b); err != nil {
				return err
			}
		}
		return d.snapshot(*e.Snapshot, "snapshot")
	case "entry":
		if d.sink.Format() == "text" && d.sink.Visible(serviceRecord(*e.Entry).Level) {
			var b bytes.Buffer
			fmt.Fprintf(&b, "schema_version=%d\ntype=entry\n", e.SchemaVersion)
			if err := d.notice(&b); err != nil {
				return err
			}
		}
		return d.entry(*e.Entry, "entry")
	case "gap":
		var b bytes.Buffer
		if d.sink.Format() == "text" {
			fmt.Fprintf(&b, "schema_version=%d\ntype=gap\nreason=%s\n", e.SchemaVersion, e.Reason)
		} else if e.Reason == "retention" {
			b.WriteString("Archive gap: entries are no longer retained\n")
		} else {
			b.WriteString("Archive continuity is unknown\n")
		}
		return d.notice(&b)
	}
	return &app.Error{Code: "output_error"}
}

func serviceRecord(e service.Entry) diagnostic.Record {
	v := e.Event
	r := diagnostic.Record{At: e.At, Event: v.Type, Level: zerolog.InfoLevel}
	switch v.Type {
	case "runner_phase":
		r.Message = "Service " + v.Status
		switch v.Status {
		case "failed":
			r.Level = zerolog.ErrorLevel
		case "clock_paused":
			r.Level = zerolog.WarnLevel
		}
	case "lane_phase":
		lane := "Crawl"
		if v.Lane == "validation" {
			lane = "Validation"
		}
		r.Message = lane + " " + v.Status
		switch v.Status {
		case "unknown", "clock_paused":
			r.Level = zerolog.WarnLevel
		case "idle", "disabled":
			r.Level = zerolog.DebugLevel
		}
		if v.Code != "" && diagnostic.CodeLevel(v.Code) > r.Level {
			r.Level = diagnostic.CodeLevel(v.Code)
		}
	case "work_diagnostic":
		r.Message = "Work update"
		r.Level = diagnostic.CodeLevel(v.Code)
	case "crawl_result":
		r.Message = "Crawl " + v.Status
		r.Level = diagnostic.ResultLevel(v.Status, false)
	case "source_result":
		r.Message = "Source " + v.Status
		r.Level = diagnostic.ResultLevel(v.Status, v.Status == "skipped")
	case "validation_result":
		r.Message = "Validation " + v.Status
		r.Level = diagnostic.ResultLevel(
			v.Status,
			v.Counts["inconclusive"] > 0 || v.Counts["unresolved"] > 0 || v.Counts["unstarted"] > 0,
		)
	}
	add := func(name, value string) {
		if value != "" {
			r.Fields = append(r.Fields, diagnostic.String(name, value))
		}
	}
	if v.RunID > 0 {
		r.Fields = append(r.Fields, diagnostic.Int("run_id", v.RunID))
	}
	add("origin", v.Origin)
	add("status", v.Status)
	add("code", v.Code)
	add("lane", v.Lane)
	add("channel", v.Channel)
	for name, n := range v.Counts {
		r.Fields = append(r.Fields, diagnostic.Int("counts."+name, n))
	}
	add("installation_id", e.InstallationID)
	add("invocation_id", e.InvocationID)
	r.Fields = append(r.Fields, diagnostic.Int("sequence", e.Sequence))
	add("stream", e.Stream)
	return r
}
