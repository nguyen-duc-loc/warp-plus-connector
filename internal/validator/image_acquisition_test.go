package validator

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// covers: spec 0007 AC-8, AC-14. Acquisition phases share one budget rather than
// each receiving another five minutes, and failure releases ownership.
func TestAcquisition_PhasesShareOneFiveMinuteBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d, fixture := dockerFixture(t)
		started := time.Now()
		var imageStarted time.Time
		d.run = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
			switch args[2] {
			case "info":
				time.Sleep(2 * time.Minute)
			case "image":
				imageStarted = time.Now()
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return fixture.run(ctx, input, args...)
		}
		acq, err := d.AcquireScheduled(t.Context(), time.Now, time.Hour)
		require.EqualError(t, err, "validator_backend_unavailable")
		require.Equal(t, 2*time.Minute, imageStarted.Sub(started))
		require.Equal(t, 5*time.Minute, time.Since(started))
		require.True(t, acq.CleanupInspected)
		require.Nil(t, acq.Owner)
		require.Equal(t, time.Now().UTC().Add(time.Hour), acq.Wait.RetryAt)
		require.Zero(t, fixture.creates)
		require.Zero(t, fixture.checks)
		d.run = fixture.run
		acq, err = d.AcquireScheduled(t.Context(), time.Now, time.Hour)
		require.NoError(t, err)
		require.NoError(t, acq.Owner.Close())
	})
}

// Spec 0007 AC-8: image inspection timeouts retain cleanup proof and a future
// recovery wait, while cancellation and missing images keep distinct outcomes.
func TestImageAcquisitionFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
		wait bool
	}{
		{"deadline", fmt.Errorf("inspect image: %w", context.DeadlineExceeded), "validator_backend_unavailable", true},
		{"cancelled", fmt.Errorf("inspect image: %w", context.Canceled), "", false},
		{"missing", &Error{Code: "validator_runtime_error"}, "validator_image_unavailable", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, fixture := dockerFixture(t)
			at := time.Now().UTC().Truncate(time.Microsecond)
			clock := func() time.Time { return at }
			d.run = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
				if len(args) > 2 && args[2] == "image" {
					return nil, tc.err
				}
				return fixture.run(ctx, input, args...)
			}
			acq, err := d.AcquireScheduled(t.Context(), clock, time.Hour)
			if tc.code == "" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.EqualError(t, err, tc.code)
			}
			require.Nil(t, acq.Owner)
			require.True(t, acq.CleanupInspected)
			if tc.wait {
				require.NotNil(t, acq.Wait)
				require.Equal(t, "backend_unavailable", acq.Wait.Reason)
				require.Equal(t, at, acq.Wait.BasisAt)
				require.Equal(t, at, acq.Wait.ObservedAt)
				require.Equal(t, time.Hour, acq.Wait.Interval)
				require.Equal(t, at.Add(time.Hour), acq.Wait.RetryAt)
			} else {
				require.Nil(t, acq.Wait)
			}
			require.Zero(t, fixture.creates)
			require.Zero(t, fixture.checks)

			// Every failed acquisition releases ownership for a later attempt.
			d.run = fixture.run
			acq, err = d.AcquireScheduled(t.Context(), clock, time.Hour)
			require.NoError(t, err)
			require.NoError(t, acq.Owner.Close())
		})
	}
}
