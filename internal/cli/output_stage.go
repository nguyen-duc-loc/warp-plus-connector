package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type outputStage struct {
	directory   string
	path        string
	destination string
	file        *os.File
	fileCreated bool
	cleaned     bool
	cleanupErr  error
	observe     func(string, *bool, string)
}

func newOutputStage(output, stateDir string) (*outputStage, error) {
	destination, err := outputDestination(output, stateDir)
	if err != nil {
		return nil, err
	}
	return stageDestination(destination)
}

func stageDestination(destination string) (*outputStage, error) {
	parent := ""
	if destination != "" {
		parent = filepath.Dir(destination)
	}
	directory, err := os.MkdirTemp(parent, "wpc-output-")
	if err != nil {
		return nil, &deliveryError{code: "export_prepare_failed"}
	}
	stage := &outputStage{directory: directory, path: filepath.Join(directory, "prepared"), destination: destination}
	stage.file, err = os.OpenFile(stage.path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		failure := &deliveryError{code: "export_prepare_failed"}
		if err := stage.cleanup(); err != nil {
			failure.remaining = directory
		}
		return nil, failure
	}
	stage.fileCreated = true
	return stage, nil
}

func outputDestination(output, stateDir string) (string, error) {
	if output == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(output)
	if err != nil || strings.HasSuffix(output, string(os.PathSeparator)) {
		return "", &deliveryError{code: "export_destination_invalid"}
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", &deliveryError{code: "export_destination_invalid"}
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return "", &deliveryError{code: "export_destination_invalid"}
	}
	canonicalState, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", &deliveryError{code: "state_not_initialized"}
		}
		return "", &deliveryError{code: "state_error"}
	}
	return excludedDestination(absolute, parent, []string{canonicalState})
}

func excludedDestination(absolute, parent string, exclusions []string) (string, error) {
	for _, canonicalState := range exclusions {
		relative, err := filepath.Rel(canonicalState, parent)
		if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))) {
			return "", &deliveryError{code: "export_destination_invalid"}
		}
	}
	destination := filepath.Join(parent, filepath.Base(absolute))
	if _, err := os.Lstat(destination); err == nil {
		return "", &deliveryError{code: "export_exists"}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", &deliveryError{code: "export_destination_invalid"}
	}
	return destination, nil
}

func (s *outputStage) cleanup() (err error) {
	if s.cleaned {
		return s.cleanupErr
	}
	s.cleaned = true
	defer func() { s.cleanupErr = err }()
	if s.file != nil {
		err = s.file.Close()
		s.file = nil
	}
	if s.fileCreated {
		if e := os.Remove(s.path); e != nil && !errors.Is(e, os.ErrNotExist) {
			err = errors.Join(err, e)
		}
	}
	if e := os.Remove(s.directory); e != nil && !errors.Is(e, os.ErrNotExist) {
		err = errors.Join(err, e)
	}
	return err
}

func (s *outputStage) publish(ctx context.Context) (bool, error) {
	if err := s.file.Sync(); err != nil {
		return false, &deliveryError{code: "export_prepare_failed"}
	}
	err := s.file.Close()
	s.file = nil
	if err != nil {
		return false, &deliveryError{code: "export_prepare_failed"}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.observation("publication", nil, "uncertain")
	if err := os.Link(s.path, s.destination); err != nil {
		s.observation("publication", new(false), "not_published")
		if errors.Is(err, os.ErrExist) {
			return false, &deliveryError{code: "export_exists"}
		}
		return false, &deliveryError{code: "export_publish_failed"}
	}
	s.observation("publication", new(true), "uncertain")
	// Publication has happened. Every later failure preserves the complete file.
	if err := ctx.Err(); err != nil {
		return true, err
	}
	directory, err := os.Open(filepath.Dir(s.destination))
	if err != nil {
		return true, &deliveryError{code: "export_commit_uncertain"}
	}
	syncErr := directory.Sync()
	if syncErr == nil {
		s.observation("cleanup", new(true), "confirmed")
	}
	err = errors.Join(syncErr, directory.Close())
	if err != nil {
		return true, &deliveryError{code: "export_commit_uncertain"}
	}
	return true, ctx.Err()
}

func (s *outputStage) copy(ctx context.Context, destination io.Writer) (bool, error) {
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		return false, &deliveryError{code: "export_prepare_failed"}
	}
	buffer := make([]byte, 32*1024)
	delivered := false
	for {
		if err := ctx.Err(); err != nil {
			return delivered, err
		}
		n, err := s.file.Read(buffer)
		if n > 0 {
			// A failed writer can still accept a prefix. Record delivery once an
			// entered write reports bytes, including cancellation during that write.
			if err := ctx.Err(); err != nil {
				return delivered, err
			}
			written, writeErr := destination.Write(buffer[:n])
			delivered = delivered || written > 0
			if err := ctx.Err(); err != nil {
				return delivered, err
			}
			if writeErr != nil || written != n {
				return delivered, &deliveryError{code: "output_error"}
			}
		}
		if errors.Is(err, io.EOF) {
			return true, nil
		}
		if err != nil {
			return delivered, &deliveryError{code: "output_error"}
		}
	}
}

func (s *outputStage) observation(phase string, published *bool, durability string) {
	if s.observe != nil {
		s.observe(phase, published, durability)
	}
}
