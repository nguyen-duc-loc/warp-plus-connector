// Package config resolves private JSON settings before application side effects.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
)

// Stable configuration errors never include supplied values or file contents.
var (
	ErrInvalid          = errors.New("invalid configuration")
	ErrScheduleDisabled = errors.New("schedule_disabled")
	ErrAccess           = errors.New(
		"configuration requires a readable regular file owned by your " +
			"account with private permissions and no symlink",
	)
)

// Settings are fully validated runtime inputs.
type Settings struct {
	CrawlEnabled, ValidationEnabled bool
	CrawlInterval, FailureInterval  time.Duration
	CheckPolicy                     state.CheckPolicy
	Sources                         []Source
	CrawlPageLimit                  int
	StateDir                        string
	RecheckInterval                 time.Duration
	Validator                       string
	ValidatorImage                  string
	ValidatorSocket                 string
	ValidatorStateDir               string
}

// Overrides distinguishes explicitly supplied flags from absent flags.
type Overrides struct {
	CrawlEnabled, ValidationEnabled                                              *bool
	CrawlInterval, FailureInterval                                               *string
	CheckWorkers, CheckMaxAttempts, CheckMutationInterval, CheckCandidateTimeout *string
	ConfigPath                                                                   *string
	StateDir                                                                     *string
	RecheckInterval                                                              *string
	Validator                                                                    *string
	ValidatorImage                                                               *string
	ValidatorSocket                                                              *string
}

// Resolve reads settings once, using defaults, JSON, then explicit flags.
func Resolve(in Overrides) (Settings, error) {
	var zero Settings
	home, err := os.UserHomeDir()
	if err != nil {
		return zero, ErrAccess
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return zero, ErrAccess
	}
	stateDir := filepath.Join(home, ".local", "state", "warp-plus-connector")
	if runtime.GOOS == "darwin" {
		stateDir = filepath.Join(home, "Library", "Application Support", "warp-plus-connector")
	} else if xdg := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(xdg) {
		stateDir = filepath.Join(xdg, "warp-plus-connector")
	}
	out := Settings{
		CheckPolicy:  state.DefaultCheckPolicy(),
		CrawlEnabled: true, ValidationEnabled: true, CrawlInterval: time.Hour, FailureInterval: time.Hour,
		Sources: defaultSources(), CrawlPageLimit: 1, StateDir: stateDir, RecheckInterval: 6 * time.Hour,
		Validator: "unsupported", ValidatorStateDir: filepath.Join(stateDir, "validator"),
	}
	path := filepath.Join(base, "warp-plus-connector", "config.json")
	if in.ConfigPath != nil {
		if *in.ConfigPath == "" {
			return zero, ErrInvalid
		}
		path = *in.ConfigPath
	}
	data, err := readPrivate(path)
	if errors.Is(err, ErrInvalid) {
		return zero, ErrInvalid
	}
	if err != nil && (in.ConfigPath != nil || !errors.Is(err, os.ErrNotExist)) {
		return zero, ErrAccess
	}
	if err == nil {
		values, err := decode(data)
		if err != nil {
			return zero, err
		}
		if err := applyJSON(&out, values); err != nil {
			return zero, err
		}
	}
	for _, override := range []struct {
		name  string
		value *string
	}{
		{"check_workers", in.CheckWorkers}, {"check_max_attempts", in.CheckMaxAttempts},
		{"check_mutation_interval", in.CheckMutationInterval}, {"check_candidate_timeout", in.CheckCandidateTimeout},
		{"crawl_interval", in.CrawlInterval}, {"failure_interval", in.FailureInterval},
		{"state_dir", in.StateDir}, {"recheck_interval", in.RecheckInterval}, {"validator", in.Validator},
		{"validator_image", in.ValidatorImage}, {"validator_socket", in.ValidatorSocket},
	} {
		if override.value != nil {
			if err := apply(&out, override.name, *override.value); err != nil {
				return zero, err
			}
		}
	}
	if in.CrawlEnabled != nil {
		out.CrawlEnabled = *in.CrawlEnabled
	}
	if in.ValidationEnabled != nil {
		out.ValidationEnabled = *in.ValidationEnabled
	}
	out.StateDir, err = filepath.Abs(out.StateDir)
	if err != nil {
		return zero, ErrInvalid
	}
	if out.Validator == "docker" && !validator.ValidImage(out.ValidatorImage) {
		return zero, ErrInvalid
	}
	return out, nil
}

func readPrivate(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !private(info) {
		return nil, ErrAccess
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrAccess
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !private(opened) || !os.SameFile(info, opened) {
		return nil, ErrAccess
	}
	data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil {
		return nil, ErrAccess
	}
	if len(data) > 64*1024 {
		return nil, ErrInvalid
	}
	return data, nil
}

func private(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0 && s.Uid == uint32(os.Geteuid())
}

// Source is one configured channel, including disabled entries.
type Source struct {
	Channel   string
	Enabled   bool
	PageLimit int
}

func defaultSources() []Source {
	return []Source{{"warpplus", true, 1}, {"warppluscn", true, 1}, {"warpplushome", true, 1}, {"warp_veyke", true, 1}}
}

func decode(data []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueJSON(d); err != nil {
		return nil, err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalid
	}
	var out map[string]json.RawMessage
	if json.Unmarshal(data, &out) != nil || out == nil {
		return nil, ErrInvalid
	}
	return out, nil
}

// Walk every object before decoding so nested duplicate fields cannot disappear.
func uniqueJSON(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return ErrInvalid
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			t, err := d.Token()
			if err != nil {
				return ErrInvalid
			}
			key, ok := t.(string)
			if !ok || seen[key] {
				return ErrInvalid
			}
			seen[key] = true
			if err := uniqueJSON(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueJSON(d); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	if _, err := d.Token(); err != nil {
		return ErrInvalid
	}
	return nil
}

func pageLimit(raw json.RawMessage) (int, error) {
	var n int
	if string(raw) == "null" || json.Unmarshal(raw, &n) != nil || n < 1 || n > 100 {
		return 0, ErrInvalid
	}
	return n, nil
}

func applyJSON(out *Settings, values map[string]json.RawMessage) error {
	if raw, ok := values["crawl_page_limit"]; ok {
		n, err := pageLimit(raw)
		if err != nil {
			return err
		}
		out.CrawlPageLimit = n
	}
	for i := range out.Sources {
		out.Sources[i].PageLimit = out.CrawlPageLimit
	}
	for k, raw := range values {
		switch k {
		case "crawl_enabled", "validation_enabled":
			var enabled bool
			if string(raw) == "null" || json.Unmarshal(raw, &enabled) != nil {
				return ErrInvalid
			}
			if k == "crawl_enabled" {
				out.CrawlEnabled = enabled
			} else {
				out.ValidationEnabled = enabled
			}
		case "check_workers", "check_max_attempts":
			var n int
			if string(raw) == "null" || json.Unmarshal(raw, &n) != nil {
				return ErrInvalid
			}
			if err := apply(out, k, strconv.Itoa(n)); err != nil {
				return err
			}
		case "crawl_page_limit":
		case "sources":
			var entries []map[string]json.RawMessage
			if string(raw) == "null" || json.Unmarshal(raw, &entries) != nil || len(entries) > 32 {
				return ErrInvalid
			}
			out.Sources = []Source{}
			seen := map[string]bool{}
			for _, entry := range entries {
				item := Source{Enabled: true, PageLimit: out.CrawlPageLimit}
				for name, v := range entry {
					switch name {

					case "channel":
						if string(v) == "null" || json.Unmarshal(v, &item.Channel) != nil {
							return ErrInvalid
						}
					case "enabled":
						if string(v) == "null" || json.Unmarshal(v, &item.Enabled) != nil {
							return ErrInvalid
						}
					case "page_limit":
						n, err := pageLimit(v)
						if err != nil {
							return err
						}
						item.PageLimit = n
					default:
						return ErrInvalid
					}
				}
				channel, ok := state.NormalizeChannel(item.Channel)
				if !ok || seen[channel] {
					return ErrInvalid
				}
				seen[channel] = true
				item.Channel = channel
				out.Sources = append(out.Sources, item)
			}
		default:
			var v string
			if string(raw) == "null" || json.Unmarshal(raw, &v) != nil {
				return ErrInvalid
			}
			if err := apply(out, k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// Selection errors occur before state is opened and never include input values.
var (
	ErrSelectionInvalid = errors.New("source_selection_invalid")
	ErrSelectionEmpty   = errors.New("source_selection_empty")
)

// SelectSources preserves configured order and applies an explicit budget last.
func (s Settings) SelectSources(names []string, limit *int) ([]state.SourcePlan, error) {
	if limit != nil && (*limit < 1 || *limit > 100) {
		return nil, ErrSelectionInvalid
	}
	selected := map[string]bool{}
	for _, name := range names {
		channel, ok := state.NormalizeChannel(name)
		if !ok || selected[channel] {
			return nil, ErrSelectionInvalid
		}
		selected[channel] = true
	}
	var out []state.SourcePlan
	for _, source := range s.Sources {
		if len(names) > 0 && !selected[source.Channel] {
			continue
		}
		if !source.Enabled {
			if selected[source.Channel] {
				return nil, ErrSelectionInvalid
			}
			continue
		}
		n := source.PageLimit
		if limit != nil {
			n = *limit
		}
		out = append(out, state.SourcePlan{Channel: source.Channel, PageLimit: n})
		delete(selected, source.Channel)
	}
	if len(selected) > 0 {
		return nil, ErrSelectionInvalid
	}
	if len(out) == 0 {
		return nil, ErrSelectionEmpty
	}
	return out, nil
}

func apply(out *Settings, name, value string) error {
	switch name {
	case "check_workers", "check_max_attempts":
		n, err := strconv.Atoi(value)
		if err != nil {
			return ErrInvalid
		}
		if name == "check_workers" {
			out.CheckPolicy.Workers = n
		} else {
			out.CheckPolicy.MaxAttempts = n
		}
		if !out.CheckPolicy.Valid() {
			return ErrInvalid
		}
	case "check_mutation_interval", "check_candidate_timeout":
		d, err := time.ParseDuration(value)
		if err != nil || d%time.Microsecond != 0 {
			return ErrInvalid
		}
		if name == "check_mutation_interval" {
			out.CheckPolicy.MutationIntervalUS = d.Microseconds()
		} else {
			out.CheckPolicy.CandidateTimeoutUS = d.Microseconds()
		}
		if !out.CheckPolicy.Valid() {
			return ErrInvalid
		}
	case "state_dir":
		if value == "" {
			return ErrInvalid
		}
		out.StateDir = value
	case "recheck_interval", "crawl_interval", "failure_interval":
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return ErrInvalid
		}
		switch name {
		case "recheck_interval":
			out.RecheckInterval = d
		case "crawl_interval":
			out.CrawlInterval = d
		case "failure_interval":
			out.FailureInterval = d
		}
	case "validator":
		if value != "unsupported" && value != "docker" {
			return ErrInvalid
		}
		out.Validator = value
	case "validator_image":
		if !validator.ValidImage(value) {
			return ErrInvalid
		}
		out.ValidatorImage = value
	case "validator_socket":
		if !filepath.IsAbs(value) {
			return ErrInvalid
		}
		out.ValidatorSocket = filepath.Clean(value)
	default:
		return ErrInvalid
	}
	return nil
}

// SchedulePolicy supplies effective durations, including defaults for embedded callers.
func (s Settings) SchedulePolicy() state.SchedulePolicy {
	p := state.DefaultSchedulePolicy()
	if s.CrawlInterval != 0 {
		p.Crawl = s.CrawlInterval
	}
	if s.RecheckInterval != 0 {
		p.Routine = s.RecheckInterval
	}
	if s.FailureInterval != 0 {
		p.Failure = s.FailureInterval
	}
	return p
}

// ValidateRun rejects unusable scheduling settings before ownership or external work.
func (s Settings) ValidateRun() error {
	if !s.CrawlEnabled && !s.ValidationEnabled {
		return ErrScheduleDisabled
	}
	for _, d := range []time.Duration{s.CrawlInterval, s.RecheckInterval, s.FailureInterval} {
		if d < time.Minute || d > 720*time.Hour {
			return ErrInvalid
		}
	}
	if s.CrawlEnabled {
		if _, err := s.SelectSources(nil, nil); err != nil {
			return err
		}
	}
	if s.ValidationEnabled && s.Validator != "docker" {
		return &validator.Error{Code: "validation_unsupported"}
	}
	if s.ValidationEnabled && !validator.ValidImage(s.ValidatorImage) {
		return ErrInvalid
	}
	return nil
}
