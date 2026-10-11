package validator

import (
	"context"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/stretchr/testify/require"
)

// covers: spec 0007 AC-9. Trusted provider waits are lower bounds and repeated
// throttle reports cannot move the original deadline.
func TestGate_ThrottleRetainsDeadlineAndOriginatingInterval(t *testing.T) {
	for _, delay := range []time.Duration{0, 30 * time.Minute, 2 * time.Hour} {
		t.Run(delay.String(), func(t *testing.T) {
			d, fixture := dockerFixture(t)
			at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
			acq, err := d.AcquireScheduled(t.Context(), func() time.Time { return at }, time.Hour)
			require.NoError(t, err)
			gate := acq.Owner.(GateOwner)
			require.NoError(t, gate.Begin(at, time.Hour))
			require.NoError(t, gate.Throttle(at, delay))
			require.NoError(t, gate.Throttle(at.Add(time.Minute), 24*time.Hour))
			require.NoError(t, gate.Resolve(at.Add(time.Minute), true, true))
			require.NoError(t, gate.Close())
			acq, err = d.AcquireScheduled(t.Context(), func() time.Time { return at }, time.Minute)
			require.EqualError(t, err, "validator_cooldown")
			require.Nil(t, acq.Owner)
			require.Equal(t, at.Add(max(time.Hour, delay)), acq.Wait.RetryAt)
			require.Equal(t, at, acq.Wait.BasisAt)
			require.Equal(t, time.Hour, acq.Wait.Interval)
			require.Zero(t, fixture.creates)
		})
	}
}

// covers: spec 0007 AC-9, AC-13, AC-14. Abandoned work and failed probes survive
// owner changes as cooldowns, even when a later profile requests a shorter interval.
func TestGate_AbandonedWorkAndUnhealthyProbeStayClosed(t *testing.T) {
	for _, probe := range []bool{false, true} {
		name := "abandoned active batch"
		if probe {
			name = "unhealthy acknowledged probe"
		}
		t.Run(name, func(t *testing.T) {
			d, _ := dockerFixture(t)
			at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
			clock := func() time.Time { return at }
			acq, err := d.AcquireScheduled(t.Context(), clock, time.Hour)
			require.NoError(t, err)
			gate := acq.Owner.(GateOwner)
			require.NoError(t, gate.Begin(at, time.Hour))
			if probe {
				require.NoError(t, gate.Throttle(at, 0))
				require.NoError(t, gate.Close())
				at = at.Add(time.Hour)
				acq, err = d.AcquireScheduled(t.Context(), clock, time.Minute)
				require.NoError(t, err)
				require.True(t, acq.Probe)
				gate = acq.Owner.(GateOwner)
				require.NoError(t, gate.Begin(at, time.Minute))
				require.NoError(t, gate.Resolve(at, true, false))
			}
			require.NoError(t, gate.Close())
			acq, err = d.AcquireScheduled(t.Context(), clock, time.Minute)
			require.EqualError(t, err, "validator_cooldown")
			reason := "validation_interrupted"
			if probe {
				reason = "validation_probe_failed"
			}
			require.Equal(t, reason, acq.Wait.Reason)
			require.Equal(t, at.Add(time.Hour), acq.Wait.RetryAt)
			require.Equal(t, time.Hour, acq.Wait.Interval)
		})
	}
}

// covers: spec 0007 AC-7, AC-9. Clock regression blocks mutations without
// discarding durable admission intent, and clock repair permits continuation.
func TestGate_MutationClockRegressionPreservesIntent(t *testing.T) {
	d, _ := dockerFixture(t)
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	acq, err := d.AcquireScheduled(t.Context(), func() time.Time { return at }, time.Hour)
	require.NoError(t, err)
	gate := acq.Owner.(GateOwner)
	defer func() { require.NoError(t, gate.Close()) }()
	require.NoError(t, gate.Begin(at, time.Hour))
	var before, after registry
	require.NoError(t, privatefs.Read(d.path(), &before))
	err = gate.BeforeMutation(at.Add(-time.Microsecond))
	require.EqualError(t, err, "validator_clock_regressed")
	require.Equal(t, at, AdmissionWait(err).RetryAt)
	require.NoError(t, privatefs.Read(d.path(), &after))
	require.Equal(t, before, after)
	require.NoError(t, gate.BeforeMutation(at))
	require.NoError(t, gate.Resolve(at, true, true))
}

// Spec 0007 AC-9, AC-13: ownership changes preserve cooldown and its originating interval.
func TestSharedCooldownRequiresOneAcknowledgedProbe(t *testing.T) {
	d, _ := dockerFixture(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	clock := func() time.Time { return at }
	acq, err := d.AcquireScheduled(t.Context(), clock, time.Hour)
	require.NoError(t, err)
	require.True(t, acq.CleanupInspected)
	gate := acq.Owner.(GateOwner)
	require.NoError(t, gate.Begin(at, time.Hour))
	require.NoError(t, gate.Throttle(at, 0))
	require.NoError(t, gate.Resolve(at, true, true))
	require.NoError(t, gate.Close())
	var saved registry
	require.NoError(t, privatefs.Read(d.path(), &saved))
	require.Equal(t, 3, saved.Version)
	require.Equal(t, "cooldown", saved.Gate.Phase)
	at = at.Add(30 * time.Minute)
	acq, err = d.AcquireScheduled(t.Context(), clock, time.Minute)
	require.ErrorContains(t, err, "validator_cooldown")
	require.Nil(t, acq.Owner)
	require.True(t, acq.CleanupInspected)
	require.Equal(t, time.Hour, acq.Wait.Interval)
	require.Equal(t, *saved.Gate.NotBefore, acq.Wait.RetryAt)
	at = *saved.Gate.NotBefore
	acq, err = d.AcquireScheduled(t.Context(), clock, time.Minute)
	require.NoError(t, err)
	require.True(t, acq.Probe)
	gate = acq.Owner.(GateOwner)
	require.NoError(t, gate.Begin(at, time.Minute))
	require.NoError(t, gate.Resolve(at, false, true))
	require.NoError(t, gate.Close())
	// A lost local acknowledgement is an abandoned probe, never an open gate.
	at = at.Add(time.Minute)
	acq, err = d.AcquireScheduled(t.Context(), clock, time.Minute)
	require.ErrorContains(t, err, "validator_cooldown")
	require.Equal(t, "validation_probe_failed", acq.Wait.Reason)
	require.Equal(t, time.Hour, acq.Wait.Interval)
	at = acq.Wait.RetryAt
	acq, err = d.AcquireScheduled(t.Context(), clock, time.Minute)
	require.NoError(t, err)
	gate = acq.Owner.(GateOwner)
	require.NoError(t, gate.Begin(at, time.Minute))
	require.NoError(t, gate.Resolve(at, true, true))
	require.NoError(t, gate.Close())
	require.NoError(t, privatefs.Read(d.path(), &saved))
	require.Nil(t, saved.Gate)
}

// Spec 0007 AC-7, AC-9: a clock regression cannot be erased by repeated acquisition.
func TestRegistryClockRegressionAndRecovery(t *testing.T) {
	d, _ := dockerFixture(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	checkpoint := at
	clock := func() time.Time { return at }
	acq, err := d.AcquireScheduled(t.Context(), clock, time.Hour)
	require.NoError(t, err)
	require.NoError(t, acq.Owner.Close())
	at = at.Add(-time.Minute)
	acq, err = d.AcquireScheduled(context.Background(), clock, time.Hour)
	require.ErrorContains(t, err, "validator_clock_regressed")
	require.Equal(t, checkpoint, acq.Wait.RetryAt)
	require.True(t, acq.CleanupInspected)
	at = checkpoint
	acq, err = d.AcquireScheduled(t.Context(), clock, time.Hour)
	require.NoError(t, err)
	require.NoError(t, acq.Owner.Close())
}
