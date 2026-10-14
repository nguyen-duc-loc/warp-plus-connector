package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
)

const logLimit = 4 * 1024 * 1024

// Event is a closed catalogue of controlled operational fields.
type Event struct {
	Type    string           `json:"type"`
	Status  string           `json:"status,omitempty"`
	Code    string           `json:"code,omitempty"`
	Lane    string           `json:"lane,omitempty"`
	RunID   int64            `json:"run_id,omitempty"`
	Origin  string           `json:"origin,omitempty"`
	Channel string           `json:"channel,omitempty"`
	Counts  map[string]int64 `json:"counts,omitempty"`
}

// Entry contains no raw output, arbitrary messages, or candidate identity.
type Entry struct {
	SchemaVersion  int       `json:"schema_version"`
	InstallationID string    `json:"installation_id"`
	InvocationID   string    `json:"invocation_id"`
	Sequence       int64     `json:"sequence"`
	At             time.Time `json:"at"`
	Stream         string    `json:"stream"`
	Event          Event     `json:"event"`
}

// LogView is one accepted archive snapshot in append order.
type LogView struct {
	boundaries        map[string]bool
	SchemaVersion     int     `json:"schema_version"`
	View              string  `json:"view"`
	RequestedLines    int     `json:"requested_lines"`
	Returned          int     `json:"returned"`
	OlderAvailable    bool    `json:"older_available"`
	TruncatedTail     bool    `json:"truncated_tail"`
	ContinuityUnknown bool    `json:"continuity_unknown"`
	Entries           []Entry `json:"entries"`
}

func validCode(code string) bool {
	if code == "" {
		return true
	}
	return strings.Contains(
		" "+strings.Join(
			[]string{
				"cancelled deadline_exceeded interrupted interrupted_before_start clock_paused cleanup_pending",
				"running failed completed batch_admitted nonretryable attempt_limit candidate_timeout",
				"not_unlimited capacity_exceeded",
				"backend_unavailable backend_busy cooldown clock_regressed probe_required rate_limited",
				"temporary_error provider_error rejected malformed_key device_limit license_invalid license_expired",
				"preview_end page_limit completed invalid_key unknown_response timeout output_error",
				"service_access_error service_record_invalid service_config_invalid service_binding_changed",
				"service_observation_failed service_log_invalid service_log_failed service_runner_failed service_running",
				"service_not_installed service_operation_incomplete service_executable_invalid",
				"source_body_too_large source_failed source_http_error source_pagination_error source_parse_error",
				"source_rate_limited source_selection_empty source_selection_invalid source_timeout source_transport_error",
				"state_busy state_error state_invalid_input state_migration_required state_not_initialized",
				"state_record_not_found state_schema_too_new validation_batch_inconclusive validation_batch_stopped",
				"validation_cleanup_pending validation_interrupted validation_invalid_input validation_observation_timeout",
				"validation_probe_failed validation_provider_error validation_rate_limited validation_timeout",
				"validation_unattributed validation_unsupported validator_backend_unavailable validator_busy",
				"validator_clock_regressed validator_cooldown validator_daemon_mismatch validator_identity_mismatch",
				"validator_image_required validator_image_unavailable validator_local_socket_required validator_not_ready",
				"validator_orphaned_session validator_output_error validator_permission_error validator_platform_unsupported",
				"validator_probe_required validator_protocol_error validator_runtime_error validator_state_error",
				"validator_version_mismatch",
			}, " ",
		)+" ", " "+code+" ",
	) && !strings.ContainsAny(code, " \t\r\n")
}

func validateEntry(e Entry) error {
	if e.SchemaVersion != 1 || !identityPattern.MatchString(e.InstallationID) ||
		!identityPattern.MatchString(e.InvocationID) || e.Sequence <= 0 || !validTime(e.At) || !validCode(e.Event.Code) {
		return failure("service_log_invalid")
	}
	v := e.Event
	result := oneOf(v.Type, "crawl_result", "source_result", "validation_result")
	if result != (e.Stream == "result") || (!result && e.Stream != "diagnostic") {
		return failure("service_log_invalid")
	}
	if !result && v.Counts != nil {
		return failure("service_log_invalid")
	}
	if !result && (v.Channel != "" || v.Origin != "") {
		return failure("service_log_invalid")
	}
	switch v.Type {
	case "runner_phase":
		if v.RunID != 0 || v.Lane != "" || !oneOf(
			v.Status,
			"starting",
			"running",
			"clock_paused",
			"stopping",
			"stopped",
			"failed",
		) ||
			(v.Status == "failed" && v.Code == "") {
			return failure("service_log_invalid")
		}
	case "lane_phase":
		if v.RunID != 0 || !oneOf(v.Lane, "crawl", "validation") {
			return failure("service_log_invalid")
		}
		allowed := []string{"unknown", "disabled", "idle", "clock_paused", "stopping"}
		if v.Lane == "crawl" {
			allowed = append(allowed, "crawling")
		} else {
			allowed = append(allowed, "acquiring", "checking", "waiting")
		}
		if !oneOf(v.Status, allowed...) {
			return failure("service_log_invalid")
		}
	case "work_diagnostic":
		if v.RunID <= 0 || v.Code == "" || v.Status != "" || v.Lane != "" {
			return failure("service_log_invalid")
		}
	case "crawl_result", "source_result", "validation_result":
		if v.RunID <= 0 || v.Lane != "" {
			return failure("service_log_invalid")
		}
		if v.Type == "source_result" {
			channel, ok := state.NormalizeChannel(v.Channel)
			if !ok || channel != v.Channel || v.Origin != "" ||
				!oneOf(
					v.Status,
					"pending",
					"running",
					"completed",
					"failed",
					"cancelled",
					"interrupted",
					"skipped",
				) {
				return failure("service_log_invalid")
			}
		} else if v.Channel != "" || !oneOf(v.Origin, "manual", "scheduled") ||
			!oneOf(v.Status, "completed", "failed", "cancelled", "interrupted") {
			return failure("service_log_invalid")
		}
		keys := []string{"saved_candidates", "saved_sightings"}
		if v.Type == "validation_result" {
			keys = []string{
				"selected", "pending", "active", "completed", "skipped", "interrupted",
				"confirmed", "invalid", "inconclusive", "unresolved", "unstarted",
			}
		}
		if len(v.Counts) != len(keys) {
			return failure("service_log_invalid")
		}
		for _, key := range keys {
			n, ok := v.Counts[key]
			if !ok || n < 0 {
				return failure("service_log_invalid")
			}
		}
	default:
		return failure("service_log_invalid")
	}
	return nil
}

func entryKey(e Entry) string {
	return e.InstallationID + ":" + e.InvocationID + ":" + strconv.FormatInt(e.Sequence, 10)
}
func sameInvocation(a, b Entry) bool {
	return a.InstallationID == b.InstallationID && a.InvocationID == b.InvocationID
}

func decodeLogEntry(data []byte, entry *Entry) error {
	if err := json.Unmarshal(data, entry); err != nil {
		return failure("service_log_invalid")
	}
	// Our own encoder emits a canonical record. Exact equality proves that no
	// fields were omitted, aliased, duplicated, or discarded by Unmarshal.
	// Other valid JSON layouts still receive the full strict schema check.
	canonical, err := json.Marshal(entry)
	if err == nil && bytes.Equal(data, canonical) {
		return nil
	}
	if err := decode(data, entry); err != nil {
		return err
	}
	var raw struct {
		Event map[string]json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return failure("service_log_invalid")
	}
	allowed := []string{"type", "code"}
	switch entry.Event.Type {
	case "runner_phase":
		allowed = append(allowed, "status")
	case "lane_phase":
		allowed = append(allowed, "status", "lane")
	case "work_diagnostic":
		allowed = append(allowed, "run_id")
	case "crawl_result", "validation_result":
		allowed = append(allowed, "run_id", "origin", "status", "counts")
	case "source_result":
		allowed = append(allowed, "run_id", "channel", "status", "counts")
	}
	for field := range raw.Event {
		if !oneOf(field, allowed...) {
			return failure("service_log_invalid")
		}
	}
	return nil
}

type logArchive struct {
	mu                            sync.Mutex
	dir, installation, invocation string
	sequence                      int64
	file                          *os.File
	size                          int64
	failed                        error
}

func openArchive(root, installation, invocation string) (*logArchive, error) {
	dir := filepath.Join(root, "logs")
	if err := directory(dir); err != nil {
		return nil, err
	}
	view, err := readArchive(context.Background(), root, 10000)
	if err != nil {
		return nil, err
	}
	f, err := privateOpen(filepath.Join(dir, "current.jsonl"), os.O_RDWR|os.O_CREATE)
	if err != nil {
		return nil, err
	}
	if view.TruncatedTail {
		data, err := io.ReadAll(io.LimitReader(f, logLimit+1))
		if err != nil || f.Truncate(int64(bytes.LastIndexByte(data, '\n')+1)) != nil {
			_ = f.Close()
			return nil, failure("service_log_invalid")
		}
	}
	n, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		_ = f.Close()
		return nil, failure("service_access_error")
	}
	if err := errors.Join(f.Sync(), privatefs.SyncDirectory(dir)); err != nil {
		_ = f.Close()
		return nil, failure("service_access_error")
	}
	return &logArchive{dir: dir, installation: installation, invocation: invocation, file: f, size: n}, nil
}

// latch prevents typed emitters from writing after a transport formatting failure.
func (a *logArchive) latch(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failed == nil {
		a.failed = err
	}
}

func (a *logArchive) append(event Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failed != nil {
		return a.failed
	}
	a.sequence++
	e := Entry{
		SchemaVersion: 1, InstallationID: a.installation, InvocationID: a.invocation,
		Sequence: a.sequence, At: time.Now().UTC().Truncate(time.Microsecond), Stream: "diagnostic", Event: event,
	}
	if oneOf(event.Type, "crawl_result", "source_result", "validation_result") {
		e.Stream = "result"
	}
	if err := validateEntry(e); err != nil {
		a.failed = err
		return err
	}
	data, err := json.Marshal(e)
	if err != nil || len(data)+1 > 16*1024 {
		a.failed = failure("service_log_invalid")
		return a.failed
	}
	data = append(data, '\n')
	if a.size+int64(len(data)) > logLimit {
		if err := a.rotate(); err != nil {
			a.failed = failure("service_log_failed")
			return a.failed
		}
	}
	n, err := a.file.Write(data)
	if err != nil || n != len(data) {
		a.failed = failure("service_log_failed")
		return a.failed
	}
	a.size += int64(n)
	return nil
}

func (a *logArchive) rotate() error {
	if err := errors.Join(a.file.Sync(), a.file.Close()); err != nil {
		a.file = nil
		return err
	}
	a.file = nil
	if err := privatefs.Remove(filepath.Join(a.dir, "4.jsonl")); err != nil {
		return err
	}
	for i := 3; i >= 0; i-- {
		name := strconv.Itoa(i) + ".jsonl"
		if i == 0 {
			name = "current.jsonl"
		}
		source := filepath.Join(a.dir, name)
		if info, err := os.Lstat(source); missing(err) {
			continue
		} else if err != nil || !private(info, false) {
			return failure("service_log_invalid")
		}
		if err := os.Rename(source, filepath.Join(a.dir, strconv.Itoa(i+1)+".jsonl")); err != nil {
			return err
		}
		if err := privatefs.SyncDirectory(a.dir); err != nil {
			return err
		}
	}
	f, err := privateOpen(filepath.Join(a.dir, "current.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	a.file, a.size = f, 0
	return errors.Join(f.Sync(), privatefs.SyncDirectory(a.dir))
}

func (a *logArchive) close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file == nil {
		return a.failed
	}
	err := errors.Join(a.failed, a.file.Close())
	a.file = nil
	return err
}

// ReadLogs captures and validates at most five bounded files without taking writer locks.
func ReadLogs(ctx context.Context, a Account, lines int) (LogView, error) {
	if lines < 1 || lines > 10000 {
		return LogView{}, failure("invalid_arguments")
	}
	return readArchive(ctx, a.Root, lines)
}

func readArchive(parent context.Context, root string, lines int) (LogView, error) {
	return readArchiveWithSnapshot(parent, root, lines, archiveSnapshot)
}

func readArchiveWithSnapshot(
	parent context.Context, root string, lines int,
	snapshot func(context.Context, string, int) (LogView, error),
) (LogView, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return LogView{}, failure("service_log_busy")
		}
		view, err := snapshot(ctx, root, lines)
		// An entered read can finish after its budget. Never accept its late result.
		if ctx.Err() != nil {
			return LogView{}, failure("service_log_busy")
		}
		if err == nil {
			return view, nil
		}
		var own *Error
		if !errors.As(err, &own) || own.Code != "service_log_busy" {
			return LogView{}, err
		}
		last = err
	}
	return LogView{}, last
}

func archiveSnapshot(ctx context.Context, root string, lines int) (LogView, error) {
	out := LogView{SchemaVersion: 1, View: "service_logs", RequestedLines: lines, Entries: []Entry{}}
	dir := filepath.Join(root, "logs")
	for _, p := range []string{root, dir} {
		if err := checkDirectory(p); err != nil {
			if missing(err) {
				return out, nil
			}
			return out, err
		}
	}
	type slot struct {
		path string
		info os.FileInfo
		file *os.File
	}
	slots := make([]slot, 5)
	defer func() {
		for _, s := range slots {
			if s.file != nil {
				_ = s.file.Close()
			}
		}
	}()
	for i := range slots {
		name := strconv.Itoa(4-i) + ".jsonl"
		if i == 4 {
			name = "current.jsonl"
		}
		s := &slots[i]
		s.path = filepath.Join(dir, name)
		info, err := os.Lstat(s.path)
		if missing(err) {
			continue
		}
		if err != nil || !private(info, false) {
			return out, failure("service_access_error")
		}
		if info.Size() > logLimit {
			return out, failure("service_log_invalid")
		}
		f, err := privateOpen(s.path, os.O_RDONLY)
		if missing(err) {
			return out, failure("service_log_busy")
		}
		if err != nil {
			return out, err
		}
		s.file, s.info = f, info
		actual, err := f.Stat()
		if err != nil || !os.SameFile(info, actual) {
			return out, failure("service_log_busy")
		}
	}
	seen := map[string]bool{}
	hadSlot, hole := false, false
	out.boundaries = map[string]bool{}
	for i, s := range slots {
		if s.info == nil {
			if hadSlot {
				hole = true
			}
			continue
		}
		hadSlot = true
		if ctx.Err() != nil {
			return out, failure("service_log_busy")
		}
		data, err := io.ReadAll(io.LimitReader(s.file, s.info.Size()))
		if err != nil || int64(len(data)) != s.info.Size() {
			return out, failure("service_log_busy")
		}
		if len(data) > 0 && data[len(data)-1] != '\n' {
			if i != 4 {
				return out, failure("service_log_invalid")
			}
			out.TruncatedTail = true
			data = data[:bytes.LastIndexByte(data, '\n')+1]
		}
		for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'}) {
			if ctx.Err() != nil {
				return out, failure("service_log_busy")
			}
			if len(data) == 0 {
				break
			}
			if len(line) == 0 {
				return out, failure("service_log_invalid")
			}
			var e Entry
			if len(line)+1 > 16*1024 || decodeLogEntry(line, &e) != nil || validateEntry(e) != nil {
				return out, failure("service_log_invalid")
			}
			key := entryKey(e)
			if seen[key] {
				return out, failure("service_log_invalid")
			}
			seen[key] = true
			if hole {
				out.boundaries[key] = true
				hole = false
			}
			if n := len(out.Entries); n > 0 {
				previous := out.Entries[n-1]
				if !sameInvocation(previous, e) || e.Sequence != previous.Sequence+1 {
					out.boundaries[key] = true
				}
			}
			out.Entries = append(out.Entries, e)
		}
	}
	for i, s := range slots {
		current, err := os.Lstat(s.path)
		if s.info == nil && missing(err) {
			continue
		}
		if err != nil || s.info == nil || !os.SameFile(current, s.info) || current.Size() < s.info.Size() ||
			(i != 4 && current.Size() != s.info.Size()) {
			return out, failure("service_log_busy")
		}
	}
	if len(out.Entries) > lines {
		out.OlderAvailable = true
		out.Entries = out.Entries[len(out.Entries)-lines:]
	}
	for i, e := range out.Entries {
		if i > 0 && out.boundaries[entryKey(e)] {
			out.ContinuityUnknown = true
		}
	}
	out.Returned = len(out.Entries)
	return out, nil
}
