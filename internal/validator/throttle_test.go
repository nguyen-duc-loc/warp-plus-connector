package validator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/protocol"
	"github.com/stretchr/testify/require"
)

// A completed attachment reports throttling before observation or cleanup can
// release the coordinator to authorize another session mutation.
func TestStagedDockerThrottleReachesGateBeforeCleanup(t *testing.T) {
	d, f := dockerFixture(t)
	attached := make(chan struct{})
	released := make(chan struct{})
	var events []string
	d.run = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
		op := args[len(args)-1]
		if args[2] == "exec" && op == "status" {
			<-attached
		}
		data, err := f.run(ctx, input, args...)
		if err != nil || args[2] != "exec" {
			return data, err
		}
		if op == "attach" {
			close(attached)
			<-ctx.Done()
			close(released)
			return nil, ctx.Err()
		}
		if op != "status" && op != "recover" {
			return data, nil
		}
		var receipt protocol.Receipt
		require.NoError(t, json.Unmarshal(data, &receipt))
		receipt.Result = protocol.Result{
			Outcome: state.Inconclusive, ReasonCode: "validation_rate_limited", FailureClass: state.Throttled,
		}
		operation := receipt.Result
		receipt.OperationResult = &operation
		if op == "status" {
			receipt.Phase = "attachment_pending"
		} else {
			events = append(events, "recovery")
		}
		return json.Marshal(receipt)
	}
	owner, err := d.Acquire(t.Context())
	require.NoError(t, err)
	defer func() { require.NoError(t, owner.Close()) }()
	hooks := fixtureHooks()
	hooks.MutationDone = func(result Result, err error) error {
		if result.FailureClass == state.Throttled {
			require.NoError(t, err)
			require.Zero(t, result.ServerDelay)
			require.Zero(t, f.removals)
			events = append(events, "throttle")
			return &Error{Code: "validation_rate_limited"}
		}
		return nil
	}
	hooks.Observe = func(Result) error {
		t.Fatal("throttle must stop the gate before observation")
		return nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := owner.Run(ctx, Input{StateDir: "/profile", CandidateID: 1, RunID: 1, AttemptID: 1}, hooks)
	require.ErrorContains(t, err, "validation_rate_limited")
	require.Equal(t, []string{"throttle", "recovery"}, events)
	require.Equal(t, state.Throttled, result.FailureClass)
	require.True(t, result.CleanupProven)
	require.True(t, result.Removed)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("attachment command was not cancelled")
	}
}
