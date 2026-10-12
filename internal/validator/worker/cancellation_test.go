package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/protocol"
	"github.com/stretchr/testify/require"
)

// Cancellation during the durable intent write must win over the next mutation.
func TestCancellationDuringIntentWriteStopsMutation(t *testing.T) {
	for _, phase := range []string{"registration_pending", "attachment_pending"} {
		t.Run(phase, func(t *testing.T) {
			v := &vendor{accountType: "unlimited"}
			e := engine(t, v)
			e.afterSave = func(saved string) error {
				if saved == phase {
					return e.Cancel()
				}
				return nil
			}
			r, err := stagedCheck(e, t.Context(), protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
			require.NoError(t, err)
			require.Equal(t, "cleaned", r.Phase)
			require.Equal(t, "cancelled", r.Result.ReasonCode)
			require.Equal(t, state.Inconclusive, r.Result.Outcome)
			require.NotContains(t, v.calls, "registration license")
			if phase == "registration_pending" {
				require.NotContains(t, v.calls, "registration new")
			}
		})
	}
}

type controlledRunner func(context.Context, ...string) Reply

func (r controlledRunner) Run(ctx context.Context, args ...string) Reply { return r(ctx, args...) }

func TestCancellationWhileVendorCommandHoldsWorkerLock(t *testing.T) {
	for _, operation := range []string{"registration new", "registration license"} {
		t.Run(operation, func(t *testing.T) {
			v := &vendor{accountType: "unlimited"}
			e := engine(t, v)
			entered := make(chan struct{})
			e.Runner = controlledRunner(func(ctx context.Context, args ...string) Reply {
				reply := v.Run(ctx, args...)
				if strings.HasPrefix(strings.Join(args, " "), operation) {
					close(entered)
					<-ctx.Done()
					return Reply{Code: "validation_timeout"}
				}
				return reply
			})
			_, err := e.Initialize(protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
			require.NoError(t, err)
			op := protocol.Operation{RemainingUS: 5000000}
			if operation == "registration license" {
				_, err = e.Register(t.Context(), op)
				require.NoError(t, err)
			}
			finished := make(chan error, 1)
			go func() {
				if operation == "registration new" {
					_, err := e.Register(t.Context(), op)
					finished <- err
				} else {
					_, err := e.Attach(t.Context(), op)
					finished <- err
				}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("vendor command did not enter")
			}
			status, err := e.Status()
			require.NoError(t, err)
			require.Contains(t, []string{"registration_pending", "attachment_pending"}, status.Phase)
			_, err = e.Recover(t.Context())
			require.ErrorIs(t, err, privatefs.ErrBusy)
			require.NoError(t, e.Cancel())
			select {
			case err = <-finished:
				if operation == "registration new" {
					require.ErrorIs(t, err, ErrPending)
				} else {
					require.NoError(t, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cancel did not release vendor command")
			}
			r, err := e.Recover(t.Context())
			require.NoError(t, err)
			require.Equal(t, "cleaned", r.Phase)
			require.False(t, v.registered)
			calls := append([]string{}, v.calls...)
			_, err = e.Register(t.Context(), op)
			require.ErrorIs(t, err, ErrPending)
			_, err = e.Attach(t.Context(), op)
			require.ErrorIs(t, err, ErrPending)
			require.Equal(t, calls, v.calls)
		})
	}
}

func TestCancellationAtEverySavedWorkerPhase(t *testing.T) {
	for _, phase := range []string{
		"prepared", "registration_pending", "registration_created", "registration_identified",
		"registered", "attachment_pending", "observed", "cleanup_pending", "cleaned",
	} {
		t.Run(phase, func(t *testing.T) {
			v := &vendor{accountType: "unlimited"}
			e := engine(t, v)
			cancelled := false
			e.afterSave = func(saved string) error {
				if saved == phase && !cancelled {
					cancelled = true
					return e.Cancel()
				}
				return nil
			}
			_, err := stagedCheck(e, t.Context(), protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
			if phase == "registration_created" || phase == "registration_identified" {
				require.ErrorIs(t, err, ErrPending)
			} else {
				require.NoError(t, err)
			}
			require.True(t, cancelled)
			r, err := e.Recover(t.Context())
			require.NoError(t, err)
			require.Equal(t, "cleaned", r.Phase)
			require.False(t, v.registered)
			if phase == "observed" || phase == "cleanup_pending" || phase == "cleaned" {
				require.Equal(t, state.Confirmed, r.Result.Outcome)
				require.NotNil(t, r.Result.Evidence)
			} else {
				require.NotContains(t, v.calls, "registration license")
			}
			if phase == "prepared" || phase == "registration_pending" {
				require.Empty(t, v.calls)
			}
		})
	}
}

func TestDeadlineDuringIntentWriteStopsMutation(t *testing.T) {
	for _, phase := range []string{"registration_pending", "attachment_pending"} {
		t.Run(phase, func(t *testing.T) {
			v := &vendor{accountType: "unlimited"}
			e := engine(t, v)
			_, err := e.Initialize(protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
			require.NoError(t, err)
			operation := protocol.Operation{RemainingUS: 5000000}
			if phase == "attachment_pending" {
				_, err = e.Register(t.Context(), operation)
				require.NoError(t, err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			e.afterSave = func(saved string) error {
				if saved == phase {
					<-ctx.Done()
				}
				return nil
			}
			var receipt protocol.Receipt
			if phase == "registration_pending" {
				receipt, err = e.Register(ctx, operation)
			} else {
				receipt, err = e.Attach(ctx, operation)
			}
			require.NoError(t, err)
			require.Equal(t, "cleaned", receipt.Phase)
			require.NotContains(t, v.calls, "registration license")
			if phase == "registration_pending" {
				require.Empty(t, v.calls)
			}
		})
	}
}

// delayedDeadline models a timer whose deadline has passed while cancellation
// delivery is still waiting for the scheduler or parent propagation.
type delayedDeadline struct {
	context.Context
	at time.Time
}

func (c delayedDeadline) Deadline() (time.Time, bool) { return c.at, true }

func TestExpiredDeadlinePreventsVendorProcess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	marker := filepath.Join(dir, "called")
	script := "#!/bin/sh\n: > '" + marker + "'\nprintf '%s' '{\"status\":\"Success\"}'\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "warp-cli"), []byte(script), 0700))
	ctx := delayedDeadline{Context: t.Context(), at: time.Now().Add(-time.Second)}
	reply := (ProcessRunner{}).Run(ctx, "registration", "delete")
	require.Equal(t, "validation_timeout", reply.Code)
	_, err := os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestExpiredDeadlineBeforeCancellationDeliveryStopsMutation(t *testing.T) {
	for _, phase := range []string{"registration_pending", "attachment_pending"} {
		t.Run(phase, func(t *testing.T) {
			v := &vendor{accountType: "unlimited"}
			e := engine(t, v)
			_, err := e.Initialize(protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
			require.NoError(t, err)
			operation := protocol.Operation{RemainingUS: 5000000}
			if phase == "attachment_pending" {
				_, err = e.Register(t.Context(), operation)
				require.NoError(t, err)
			}
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx := delayedDeadline{Context: parent, at: time.Now().Add(20 * time.Millisecond)}
			e.afterSave = func(saved string) error {
				if saved == phase {
					time.Sleep(max(time.Until(ctx.at), 0))
					require.NoError(t, ctx.Err(), "deadline delivery is intentionally delayed")
				}
				return nil
			}
			var receipt protocol.Receipt
			if phase == "registration_pending" {
				receipt, err = e.Register(ctx, operation)
			} else {
				receipt, err = e.Attach(ctx, operation)
			}
			require.NoError(t, err)
			require.Equal(t, "cleaned", receipt.Phase)
			require.NotContains(t, v.calls, "registration license")
			if phase == "registration_pending" {
				require.Empty(t, v.calls)
			}
		})
	}
}

func TestCancellationDuringObservationStillCleansRegistration(t *testing.T) {
	v := &vendor{accountType: "unlimited", delay: 100}
	e := engine(t, v)
	e.ObservationTimeout = 2 * time.Second
	cancelled := false
	e.Runner = controlledRunner(func(ctx context.Context, args ...string) Reply {
		reply := v.Run(ctx, args...)
		if strings.Join(args, " ") == "registration show" && v.pendingKey != "" && !cancelled {
			cancelled = true
			require.NoError(t, e.Cancel())
		}
		return reply
	})
	r, err := stagedCheck(e, t.Context(), protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
	require.NoError(t, err)
	require.True(t, cancelled)
	require.Equal(t, "cleaned", r.Phase)
	require.Equal(t, "cancelled", r.Result.ReasonCode)
	require.Equal(t, state.Inconclusive, r.Result.Outcome)
	require.False(t, v.registered)
	require.Nil(t, r.Result.Evidence)
}

// The operation lock and cleaned journal reject commands delivered after recovery.
func TestLateCommandsCannotReplayAfterRecovery(t *testing.T) {
	for _, phase := range []string{"prepared", "registered"} {
		t.Run(phase, func(t *testing.T) {
			v := &vendor{accountType: "unlimited"}
			e := engine(t, v)
			_, err := e.Initialize(protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
			require.NoError(t, err)
			operation := protocol.Operation{RemainingUS: 240000000}
			if phase == "registered" {
				_, err = e.Register(t.Context(), operation)
				require.NoError(t, err)
			}
			_, err = e.Recover(t.Context())
			require.NoError(t, err)
			calls := append([]string{}, v.calls...)
			_, err = e.Register(context.Background(), operation)
			require.ErrorIs(t, err, ErrPending)
			_, err = e.Attach(context.Background(), operation)
			require.ErrorIs(t, err, ErrPending)
			_, err = e.Initialize(protocol.Request{SessionID: fixtureSession, Key: fixtureKey})
			require.ErrorIs(t, err, ErrPending)
			require.Equal(t, calls, append([]string{}, v.calls...))
		})
	}
}
