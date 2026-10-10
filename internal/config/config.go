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
	"syscall"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
)

// Stable configuration errors never include supplied values or file contents.
var (
	ErrInvalid = errors.New("invalid configuration")
	ErrAccess  = errors.New("configuration requires a readable regular file owned by your account with private permissions and no symlink")
)

// Settings are fully validated runtime inputs.
type Settings struct {
	StateDir          string
	RecheckInterval   time.Duration
	Validator         string
	ValidatorImage    string
	ValidatorSocket   string
	ValidatorStateDir string
}

// Overrides distinguishes explicitly supplied flags from absent flags.
type Overrides struct {
	ConfigPath      *string
	StateDir        *string
	RecheckInterval *string
	Validator       *string
	ValidatorImage  *string
	ValidatorSocket *string
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
	out := Settings{StateDir: stateDir, RecheckInterval: 6 * time.Hour, Validator: "unsupported", ValidatorStateDir: filepath.Join(stateDir, "validator")}
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
		for k, v := range values {
			if err := apply(&out, k, v); err != nil {
				return zero, err
			}
		}
	}
	for _, override := range []struct {
		name  string
		value *string
	}{
		{"state_dir", in.StateDir}, {"recheck_interval", in.RecheckInterval}, {"validator", in.Validator},
		{"validator_image", in.ValidatorImage}, {"validator_socket", in.ValidatorSocket},
	} {
		if override.value != nil {
			if err := apply(&out, override.name, *override.value); err != nil {
				return zero, err
			}
		}
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

func decode(data []byte) (map[string]string, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return nil, ErrInvalid
	}
	values := map[string]string{}
	for d.More() {
		t, err = d.Token()
		if err != nil {
			return nil, ErrInvalid
		}
		name, ok := t.(string)
		if !ok {
			return nil, ErrInvalid
		}
		if _, exists := values[name]; exists {
			return nil, ErrInvalid
		}
		var value any
		if d.Decode(&value) != nil {
			return nil, ErrInvalid
		}
		s, ok := value.(string)
		if !ok {
			return nil, ErrInvalid
		}
		values[name] = s
	}
	if t, err = d.Token(); err != nil || t != json.Delim('}') {
		return nil, ErrInvalid
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalid
	}
	return values, nil
}

func apply(out *Settings, name, value string) error {
	switch name {
	case "state_dir":
		if value == "" {
			return ErrInvalid
		}
		out.StateDir = value
	case "recheck_interval":
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return ErrInvalid
		}
		out.RecheckInterval = d
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
