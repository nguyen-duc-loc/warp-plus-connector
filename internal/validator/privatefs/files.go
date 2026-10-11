// Package privatefs provides private durable files for external session recovery.
package privatefs

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// ErrAccess deliberately excludes paths, credentials, and operating system text.
var ErrAccess = errors.New("validator_state_error")

// ErrBusy means another process owns the recovery directory.
var ErrBusy = errors.New("validator_busy")

func private(info os.FileInfo, directory bool) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0077 == 0 &&
		((directory && info.IsDir()) || (!directory && info.Mode().IsRegular()))
}

// Directory creates or verifies an account private directory.
func Directory(path string) error {
	if !filepath.IsAbs(path) {
		return ErrAccess
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return ErrAccess
	}
	info, err := os.Lstat(path)
	if err != nil || !private(info, true) {
		return ErrAccess
	}
	return nil
}

func open(path string, flags int) (*os.File, error) {
	f, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, ErrAccess
	}
	info, err := f.Stat()
	if err != nil || !private(info, false) {
		_ = f.Close()
		return nil, ErrAccess
	}
	return f, nil
}

// Lock holds nonblocking exclusive ownership until its file is closed.
func Lock(path string) (*os.File, error) {
	f, err := open(path, os.O_RDWR|os.O_CREATE)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrBusy
		}
		return nil, ErrAccess
	}
	return f, nil
}

// Read decodes one bounded private JSON document.
func Read(path string, value any) error {
	f, err := open(path, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	d := json.NewDecoder(io.LimitReader(f, 1024*1024))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return ErrAccess
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrAccess
	}
	return nil
}

// Write atomically replaces a private file and synchronizes its directory.
func Write(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return ErrAccess
	}
	return WriteBytes(path, data)
}

// WriteBytes durably replaces a private file without following a destination link.
func WriteBytes(path string, data []byte) error {
	return writeBytes(path, data, SyncDirectory)
}

func writeBytes(path string, data []byte, syncDirectory func(string) error) error {
	if info, err := os.Lstat(path); err == nil {
		if !private(info, false) {
			return ErrAccess
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrAccess
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return ErrAccess
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return ErrAccess
	}
	if err := os.Rename(name, path); err != nil {
		return ErrAccess
	}
	return syncDirectory(dir)
}

// Remove durably removes a journal only after its caller has proved cleanup.
func Remove(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !private(info, false) || os.Remove(path) != nil {
		return ErrAccess
	}
	return SyncDirectory(filepath.Dir(path))
}

// SyncDirectory makes a preceding rename or removal durable.
func SyncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return ErrAccess
	}
	err = errors.Join(f.Sync(), f.Close())
	if err != nil {
		return ErrAccess
	}
	return nil
}
