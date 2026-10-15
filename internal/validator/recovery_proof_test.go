package validator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/protocol"
	"github.com/stretchr/testify/require"
)

// Work acquisition and worker hooks share one mutation owner, without reentry.
func TestReviewedAcquisition_DoesNotNestWorkerJournalWrites(t *testing.T) {
	d, _ := dockerFixture(t)
	entered := false
	commit := func(fn func() error) error {
		if entered {
			return errors.New("nested mutation executor")
		}
		entered = true
		defer func() { entered = false }()
		return fn()
	}
	acq, err := d.AcquireReviewed(t.Context(), time.Now, time.Hour, commit, nil, func() {})
	require.NoError(t, err)
	defer func() { require.NoError(t, acq.Owner.Close()) }()
	hooks := fixtureHooks()
	hooks.Commit = commit
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := acq.Owner.Run(ctx, Input{Key: "ABCDEFGH-12345678-AbCdEf01",
		StateDir: "/profile", CandidateID: 1, RunID: 1, AttemptID: 1}, hooks)
	require.NoError(t, err)
	require.True(t, result.CleanupProven)
	require.True(t, result.Removed)
}

// Spec 0012 AC-15: real reconciliation can clear a retained obligation before
// image admission fails, while a missing journal never counts as cleanup proof.
func TestReviewedAcquisition_RequiresOriginatingRecoveryProof(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "reconciled before image failure"
		if missing {
			name = "missing journal"
		}
		t.Run(name, func(t *testing.T) {
			d, f := dockerFixture(t)
			owner, err := d.acquire(t.Context(), true)
			require.NoError(t, err)
			f.recoveryError = &Error{Code: "validation_cleanup_pending"}
			d.run = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
				data, err := f.run(ctx, input, args...)
				if err != nil || args[2] != "exec" || args[len(args)-1] != "attach" {
					return data, err
				}
				var receipt protocol.Receipt
				if err = json.Unmarshal(data, &receipt); err != nil {
					return nil, err
				}
				receipt.Phase = "cleanup_pending"
				return json.Marshal(receipt)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err = owner.Run(ctx, Input{Key: "ABCDEFGH-12345678-AbCdEf01",
				StateDir: "/profile", CandidateID: 1, RunID: 1, AttemptID: 1}, fixtureHooks())
			require.Error(t, err)
			binding := owner.CleanupBinding()
			require.NotNil(t, binding)
			require.NoError(t, owner.Close())
			if missing {
				require.NoError(t, os.Remove(d.path()))
			}
			f.recoveryError = nil
			d.run = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
				if args[2] == "image" {
					return nil, &Error{Code: "validator_image_unavailable"}
				}
				return f.run(ctx, input, args...)
			}
			resolved := false
			_, err = d.AcquireReviewed(t.Context(), time.Now, time.Hour, func(fn func() error) error { return fn() },
				binding, func() { resolved = true })
			require.Error(t, err)
			require.Equal(t, !missing, resolved)
		})
	}
}
