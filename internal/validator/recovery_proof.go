package validator

import (
	"context"
	"path/filepath"
	"time"
)

// CleanupBinding is an opaque parent only binding to unresolved remote obligations.
type CleanupBinding struct {
	path, daemon string
	sessions     []string
	unknown      bool
}

// CleanupOwner exposes retained obligations before shared ownership is released.
type CleanupOwner interface{ CleanupBinding() *CleanupBinding }

func (o *dockerOwner) CleanupBinding() *CleanupBinding {
	if len(o.registry.Sessions) == 0 && len(o.uncertainSessions) == 0 {
		return nil
	}
	path, err := filepath.EvalSymlinks(filepath.Dir(o.docker.path()))
	b := &CleanupBinding{path: path, daemon: o.daemon, unknown: o.poisoned || err != nil}
	seen := map[string]bool{}
	for _, entries := range [][]manifest{o.registry.Sessions, o.uncertainSessions} {
		for _, m := range entries {
			if !seen[m.SessionID] {
				b.sessions = append(b.sessions, m.SessionID)
				seen[m.SessionID] = true
			}
		}
	}
	return b
}

// AcquireReviewed requires authoritative reconciliation of the retained scope
// before new attachments. Resolution is reported while exclusive ownership is held.
func (d *Docker) AcquireReviewed(
	ctx context.Context, clock func() time.Time, interval time.Duration,
	commit func(func() error) error, retained *CleanupBinding, resolved func(),
) (Acquisition, error) {
	clone := *d
	clone.mutation, clone.retained, clone.resolved = commit, retained, resolved
	result, err := clone.AcquireScheduled(ctx, clock, interval)
	if owner, ok := result.Owner.(*dockerOwner); ok {
		// Once admitted, worker hooks already serialize journal writes. Retaining
		// the acquisition wrapper would reenter the same mutation executor.
		owner.docker = d
	}
	return result, err
}

func (o *dockerOwner) checkRetained() error {
	b := o.docker.retained
	if b == nil {
		return nil
	}
	path, err := filepath.EvalSymlinks(filepath.Dir(o.docker.path()))
	if err != nil || b.path != path || b.daemon != o.daemon {
		return &Error{Code: "validation_cleanup_pending"}
	}
	// A missing or replaced journal is not proof. Every known originating
	// obligation must still be accounted for before this reconciliation begins.
	for _, id := range b.sessions {
		found := false
		for _, m := range o.registry.Sessions {
			if m.SessionID == id {
				found = true
				break
			}
		}
		if !found {
			return &Error{Code: "validation_cleanup_pending"}
		}
	}
	if b.unknown && len(o.registry.Sessions) == 0 {
		return &Error{Code: "validation_cleanup_pending"}
	}
	return nil
}
