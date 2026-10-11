// Package service owns account scoped native supervision and private diagnostics.
package service

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
)

// Error exposes only a stable service code and known mutation uncertainty.
type Error struct {
	Code    string
	Changed bool
}

func (e *Error) Error() string {
	if e.Changed {
		return e.Code + " changed=true operation_incomplete=true"
	}
	return e.Code
}

func failure(code string) error { return &Error{Code: code} }

var identityPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func newIdentity() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", failure("service_access_error")
	}
	return hex.EncodeToString(data[:]), nil
}

func private(info os.FileInfo, directory bool) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0077 == 0 &&
		((directory && info.IsDir()) || (!directory && info.Mode().IsRegular()))
}

func privateOpen(path string, flags int) (*os.File, error) {
	f, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, failure("service_access_error")
	}
	info, err := f.Stat()
	if err != nil || !private(info, false) {
		_ = f.Close()
		return nil, failure("service_access_error")
	}
	return f, nil
}

func readBytes(path string, limit int64) ([]byte, error) {
	f, err := privateOpen(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	err = errors.Join(err, f.Close())
	if err != nil {
		return nil, failure("service_access_error")
	}
	if int64(len(data)) > limit {
		return nil, failure("service_record_invalid")
	}
	return data, nil
}

type jsonObject struct {
	fields   map[string]reflect.Type
	required []string
}

var jsonObjects sync.Map

func objectSchema(kind reflect.Type) *jsonObject {
	if kind == nil || kind.Kind() != reflect.Struct || kind == reflect.TypeOf(time.Time{}) {
		return nil
	}
	if cached, ok := jsonObjects.Load(kind); ok {
		return cached.(*jsonObject)
	}
	schema := &jsonObject{fields: map[string]reflect.Type{}}
	for i := 0; i < kind.NumField(); i++ {
		field := kind.Field(i)
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		required := !strings.Contains(options, "omitempty")
		schema.fields[name] = field.Type
		if required {
			schema.required = append(schema.required, name)
		}
	}
	actual, _ := jsonObjects.LoadOrStore(kind, schema)
	return actual.(*jsonObject)
}

// uniqueFields validates exact field names, required fields and duplicates in one walk.
func uniqueFields(d *json.Decoder, kind reflect.Type) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token == nil && kind != nil && kind.Kind() != reflect.Pointer {
		return failure("service_record_invalid")
	}
	for kind != nil && kind.Kind() == reflect.Pointer {
		kind = kind.Elem()
	}
	switch token {
	case json.Delim('{'):
		schema := objectSchema(kind)
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return failure("service_record_invalid")
			}
			seen[name] = true
			var child reflect.Type
			if schema != nil {
				field, ok := schema.fields[name]
				if !ok {
					return failure("service_record_invalid")
				}
				child = field
			} else if kind != nil && kind.Kind() == reflect.Map {
				child = kind.Elem()
			}
			if err := uniqueFields(d, child); err != nil {
				return err
			}
		}
		if schema != nil {
			for _, name := range schema.required {
				if !seen[name] {
					return failure("service_record_invalid")
				}
			}
		}
		_, err = d.Token()
	case json.Delim('['):
		var child reflect.Type
		if kind != nil && (kind.Kind() == reflect.Slice || kind.Kind() == reflect.Array) {
			child = kind.Elem()
		}
		for d.More() {
			if err := uniqueFields(d, child); err != nil {
				return err
			}
		}
		_, err = d.Token()
	}
	return err
}

func decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := uniqueFields(d, reflect.TypeOf(out).Elem()); err != nil {
		return failure("service_record_invalid")
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return failure("service_record_invalid")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return failure("service_record_invalid")
	}
	return nil
}

func readRecord(path string, out any) error {
	data, err := readBytes(path, 64*1024)
	if err != nil {
		return err
	}
	return decode(data, out)
}

func writeRecord(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > 64*1024 {
		return failure("service_record_invalid")
	}
	if err := privatefs.WriteBytes(path, data); err != nil {
		return failure("service_access_error")
	}
	return nil
}

func directory(path string) error {
	if err := privatefs.Directory(path); err != nil {
		return failure("service_access_error")
	}
	return nil
}

func checkDirectory(path string) error {
	i, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.ErrNotExist
	}
	if err != nil || !private(i, true) {
		return failure("service_access_error")
	}
	return nil
}

func controlLock(path string) (*os.File, error) {
	f, err := privatefs.Lock(path)
	if errors.Is(err, privatefs.ErrBusy) {
		return nil, failure("service_busy")
	}
	if err != nil {
		return nil, failure("service_access_error")
	}
	return f, nil
}

func runnerLock(path string) (*os.File, error) {
	f, err := privateOpen(path, os.O_RDWR|os.O_CREATE)
	if err != nil {
		return nil, err
	}
	lock := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: int16(io.SeekStart)}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &lock); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EAGAIN) {
			return nil, failure("service_running")
		}
		return nil, failure("service_access_error")
	}
	return f, nil
}

// ownerPID must never be called by the process holding runnerLock on this inode.
func ownerPID(path string) (*int, error) {
	f, err := privateOpen(path, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lock := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: int16(io.SeekStart)}
	err = syscall.FcntlFlock(f.Fd(), syscall.F_GETLK, &lock)
	err = errors.Join(err, f.Close())
	if err != nil {
		return nil, failure("service_access_error")
	}
	pid := 0
	if lock.Type != syscall.F_UNLCK {
		pid = int(lock.Pid)
	}
	return &pid, nil
}

func canonical(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", failure("service_config_invalid")
	}
	path = filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", failure("service_access_error")
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", failure("service_access_error")
	}
	resolved, err = canonical(parent)
	return filepath.Join(resolved, filepath.Base(path)), err
}
