// Package terminal owns the private UI process connection and terminal lifecycle.
package terminal

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Error contains only a closed terminal diagnostic code.
type Error struct{ Code, Remaining string }

func (e *Error) Error() string  { return e.Code }
func failure(code string) error { return &Error{Code: code} }

type manifest struct {
	Schema     int               `json:"schema_version"`
	Protocol   int               `json:"protocol_version"`
	Release    string            `json:"release"`
	Commit     string            `json:"commit"`
	OS         string            `json:"os"`
	Arch       string            `json:"arch"`
	Node       string            `json:"node"`
	LockDigest string            `json:"lock_sha256"`
	Files      map[string]string `json:"files"`
}

func bundle(release, commit string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", failure("ui_installation_invalid")
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", failure("ui_installation_invalid")
	}
	root := filepath.Join(filepath.Dir(exe), "lib", "wpc-ui")
	f, err := regular(filepath.Dir(exe), "lib/wpc-ui/manifest.json")
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	closeErr := f.Close()
	if err != nil || closeErr != nil || len(data) > 1024*1024 {
		return "", failure("ui_installation_invalid")
	}
	var m manifest
	if strictJSON(data, &m) != nil || m.Schema != 1 || m.Protocol != 3 || m.Release != release ||
		m.Commit != commit || m.OS != runtime.GOOS || m.Arch != runtime.GOARCH || m.Node != "24.20.0" ||
		len(m.Files) == 0 || len(m.Files) > 20000 {
		return "", failure("ui_version_mismatch")
	}
	for _, required := range []string{
		"node/bin/node", "app/package.json", "app/dist/main.js", "app/pnpm-lock.yaml",
	} {
		if m.Files[required] == "" {
			return "", failure("ui_installation_invalid")
		}
	}
	if m.LockDigest != m.Files["app/pnpm-lock.yaml"] {
		return "", failure("ui_installation_invalid")
	}
	for path, expected := range m.Files {
		if len(expected) != 64 {
			return "", failure("ui_installation_invalid")
		}
		f, err := regular(root, path)
		if err != nil {
			return "", err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		closeErr := f.Close()
		if err != nil || closeErr != nil || hex.EncodeToString(h.Sum(nil)) != expected {
			return "", failure("ui_installation_invalid")
		}
	}
	// Reject unlisted runtime modules as well as path escapes and symlinks.
	err = filepath.WalkDir(
		root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return failure("ui_installation_invalid")
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil || (rel != "manifest.json" && m.Files[filepath.ToSlash(rel)] == "") {
				return failure("ui_installation_invalid")
			}
			return nil
		},
	)
	if err != nil {
		return "", failure("ui_installation_invalid")
	}
	return root, nil
}

func regular(root, path string) (*os.File, error) {
	if path == "" || filepath.IsAbs(path) || filepath.ToSlash(filepath.Clean(path)) != path ||
		strings.Contains(path, "\\") || path == ".." || strings.HasPrefix(path, "../") {
		return nil, failure("ui_installation_invalid")
	}
	current := root
	info, err := os.Lstat(current)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, failure("ui_installation_invalid")
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err = os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 ||
			(i < len(parts)-1 && !info.IsDir()) || (i == len(parts)-1 && !info.Mode().IsRegular()) {
			return nil, failure("ui_installation_invalid")
		}
	}
	f, err := os.Open(current)
	if err != nil {
		return nil, failure("ui_installation_invalid")
	}
	return f, nil
}
