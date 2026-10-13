package service

import (
	"context"
	"time"
)

// LogEnvelope keeps a following JSON stream complete at every record boundary.
type LogEnvelope struct {
	SchemaVersion int      `json:"schema_version"`
	Type          string   `json:"type"`
	Snapshot      *LogView `json:"snapshot,omitempty"`
	Entry         *Entry   `json:"entry,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

// FollowLogs reports discontinuities before resuming from available complete entries.
func FollowLogs(ctx context.Context, a Account, lines int, emit func(LogEnvelope) error) error {
	if lines < 1 || lines > 10000 {
		return failure("invalid_arguments")
	}
	initial, err := readArchive(ctx, a.Root, int(^uint(0)>>1))
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, e := range initial.Entries {
		seen[entryKey(e)] = true
	}
	if len(initial.Entries) > lines {
		initial.OlderAvailable = true
		initial.Entries = initial.Entries[len(initial.Entries)-lines:]
	}
	initial.RequestedLines, initial.Returned, initial.ContinuityUnknown = lines, len(initial.Entries), false
	for i, e := range initial.Entries {
		if i > 0 && initial.boundaries[entryKey(e)] {
			initial.ContinuityUnknown = true
		}
	}
	if err := emit(LogEnvelope{SchemaVersion: 1, Type: "snapshot", Snapshot: &initial}); err != nil {
		return err
	}
	var last *Entry
	if len(initial.Entries) > 0 {
		copy := initial.Entries[len(initial.Entries)-1]
		last = &copy
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		view, err := readArchive(ctx, a.Root, int(^uint(0)>>1))
		if err != nil {
			return err
		}
		start := 0
		unknownIdentity := false
		if last != nil {
			found := false
			for i, e := range view.Entries {
				if entryKey(e) == entryKey(*last) {
					start, found = i+1, true
					break
				}
			}
			unknownIdentity = !found && len(view.Entries) > 0
		}
		for _, e := range view.Entries[start:] {
			if err := ctx.Err(); err != nil {
				return err
			}
			if seen[entryKey(e)] {
				continue
			}
			if last != nil {
				if sameInvocation(*last, e) && e.Sequence <= last.Sequence {
					continue
				}
				reason := ""
				if unknownIdentity || view.boundaries[entryKey(e)] {
					reason = "continuity_unknown"
				}
				unknownIdentity = false
				if !sameInvocation(*last, e) {
					reason = "continuity_unknown"
				} else if e.Sequence != last.Sequence+1 {
					reason = "retention"
				}
				if reason != "" {
					if err := emit(LogEnvelope{SchemaVersion: 1, Type: "gap", Reason: reason}); err != nil {
						return err
					}
				}
			}
			entry := e
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := emit(LogEnvelope{SchemaVersion: 1, Type: "entry", Entry: &entry}); err != nil {
				return err
			}
			last = &entry
			seen[entryKey(entry)] = true
		}
		nextSeen := map[string]bool{}
		for _, e := range view.Entries {
			if seen[entryKey(e)] {
				nextSeen[entryKey(e)] = true
			}
		}
		if last != nil {
			nextSeen[entryKey(*last)] = true
		}
		seen = nextSeen
	}
}
