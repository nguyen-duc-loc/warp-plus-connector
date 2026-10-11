package validator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/stretchr/testify/require"
)

// replaceWithoutSync reproduces a visible rename whose directory sync failed.
// privatefs separately exercises that interval inside its real replacement path.
func replaceWithoutSync(t *testing.T, path string, value any) error {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	f, err := os.CreateTemp(filepath.Dir(path), ".fault-")
	require.NoError(t, err)
	_, err = f.Write(data)
	require.NoError(t, err)
	require.NoError(t, f.Sync())
	require.NoError(t, f.Close())
	require.NoError(t, os.Rename(f.Name(), path))
	return privatefs.ErrAccess
}

func fixtureHooks() Hooks {
	return Hooks{
		Commit:       func(fn func() error) error { return fn() },
		Admit:        func(ctx context.Context, _ string) error { return ctx.Err() },
		MutationDone: func(Result, error) error { return nil },
		Observe:      func(Result) error { return nil },
		CleanupContext: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Second)
		},
	}
}

func TestJournalLostAcknowledgementPoisonsOwnerAndRecoversVisiblePayload(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		write                     int
		creates, checks, removals int
	}{
		{"create intent", 1, 0, 0, 0},
		{"created identity", 2, 1, 0, 0},
		{"registration intent", 3, 1, 0, 0},
		{"attachment intent", 4, 1, 0, 0},
		{"cleaned receipt", 5, 1, 1, 0},
		{"entry removal", 6, 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, f := dockerFixture(t)
			owner, err := d.acquire(t.Context(), true)
			require.NoError(t, err)
			writes := 0
			d.writeJournal = func(path string, value any) error {
				writes++
				if writes == tc.write {
					return replaceWithoutSync(t, path, value)
				}
				return privatefs.Write(path, value)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			in := Input{StateDir: "/profile", RunID: 1, CandidateID: 1, AttemptID: 1}
			_, err = owner.Run(ctx, in, fixtureHooks())
			require.ErrorContains(t, err, "validator_state_error")
			require.True(t, owner.poisoned)
			require.Equal(t, tc.write, writes)
			require.Equal(t, tc.creates, f.creates)
			require.Equal(t, tc.checks, f.checks)
			require.Equal(t, tc.removals, f.removals)
			visible, err := os.ReadFile(d.path())
			require.NoError(t, err)
			_, err = owner.Run(ctx, in, fixtureHooks())
			require.ErrorContains(t, err, "validator_state_error")
			require.Equal(t, tc.write, writes)
			require.Equal(t, tc.creates, f.creates)
			require.NoError(t, owner.Close())
			after, err := os.ReadFile(d.path())
			require.NoError(t, err)
			require.Equal(t, visible, after)
			// A fresh owner must use the visible replacement, not the stale memory.
			d.writeJournal = nil
			require.NoError(t, d.Recover(t.Context()))
			var recovered registry
			require.NoError(t, privatefs.Read(d.path(), &recovered))
			require.Equal(t, 3, recovered.Version)
			require.Empty(t, recovered.Sessions)
			require.Equal(t, tc.creates, f.creates)
			require.Equal(t, tc.checks, f.checks)
			require.Equal(t, tc.creates, f.removals)
		})
	}
}

func TestLegacyJournalLostAcknowledgementRecoversActualVersion(t *testing.T) {
	for _, cleaned := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy receipt", true: "registry conversion"}[cleaned], func(t *testing.T) {
			d, f := dockerFixture(t)
			savedManifest(t, d, f, cleaned)
			f.ids = []string{testContainer}
			writes := 0
			d.writeJournal = func(path string, value any) error {
				writes++
				return replaceWithoutSync(t, path, value)
			}
			require.ErrorContains(t, d.Recover(t.Context()), "validator_state_error")
			require.Equal(t, 1, writes)
			var visible struct {
				Version int `json:"version"`
			}
			data, err := os.ReadFile(d.path())
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(data, &visible))
			if cleaned {
				require.Equal(t, 3, visible.Version)
				require.Equal(t, 1, f.removals)
			} else {
				require.Equal(t, 1, visible.Version)
				require.Zero(t, f.removals)
			}
			d.writeJournal = nil
			require.NoError(t, d.Recover(t.Context()))
			var recovered registry
			require.NoError(t, privatefs.Read(d.path(), &recovered))
			require.Equal(t, 3, recovered.Version)
			require.Empty(t, recovered.Sessions)
			require.Equal(t, 1, f.removals)
			require.Zero(t, f.creates)
			require.Zero(t, f.checks)
		})
	}
}
