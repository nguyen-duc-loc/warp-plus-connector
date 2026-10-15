package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// ErrCollectionChanged rejects mutation of a collection that replaced a reviewed one.
var ErrCollectionChanged = errors.New("reviewed collection changed")

// AdmissionUncertain means entered writer recovery or closure lost acknowledgement.
type AdmissionUncertain struct{ Cause error }

func (e *AdmissionUncertain) Error() string { return "reviewed writer persistence uncertain" }
func (e *AdmissionUncertain) Unwrap() error { return e.Cause }

// CollectionBinding records existing file identity or explicit absence at review.
type CollectionBinding struct {
	Identity CollectionIdentity
	Absent   bool
}

// BindCollection reads private metadata without initialization or recovery.
func BindCollection(path string) (CollectionBinding, error) {
	i, err := IdentifyCollection(path)
	if errors.Is(err, ErrNotInitialized) {
		directory, err := absentDirectory(path)
		return CollectionBinding{Absent: true, Identity: CollectionIdentity{Directory: directory}}, err
	}
	return CollectionBinding{Identity: i}, err
}

func (b CollectionBinding) check(path string) error {
	i, err := IdentifyCollection(path)
	if b.Absent && errors.Is(err, ErrNotInitialized) {
		directory, err := absentDirectory(path)
		if err == nil && directory == b.Identity.Directory {
			return nil
		}
		return ErrCollectionChanged
	}
	if !b.Absent && err == nil && b.Identity.Same(i.Directory) {
		return nil
	}
	return ErrCollectionChanged
}

func absentDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrInvalidInput
	}
	current, suffix := filepath.Clean(path), ""
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return filepath.Join(resolved, suffix), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", ErrStateAccess
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", ErrStateAccess
		}
		suffix = filepath.Join(filepath.Base(current), suffix)
		current = parent
	}
}

// OpenReviewedWriter checks identity under the writer lock before database creation,
// migration or recovery. It waits for entered admission and resource closure.
func OpenReviewedWriter(
	ctx context.Context, path string, clock func() time.Time,
	policy state.SchedulePolicy, expected CollectionBinding,
) (*Writer, error) {
	if !policy.Valid() {
		return nil, ErrInvalidInput
	}
	return openWriter(ctx, path, clock, policy, &expected)
}
