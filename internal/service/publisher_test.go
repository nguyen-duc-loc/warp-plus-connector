package service

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// Spec 0008 AC-7, AC-8, AC-14: timed out writes retain ownership and poison the sink.
func TestObservationDeadlineRetainsEnteredOperation(t *testing.T) {
	synctest.Test(
		t, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			p := newPublisher("unused", observationFixture(), cancel)
			release := make(chan struct{})
			var writes atomic.Int32
			p.write = func(string, any) error { writes.Add(1); <-release; return nil }
			started := time.Now()
			err := p.publish(t.Context(), nil)
			require.EqualError(t, err, "service_observation_failed")
			require.Equal(t, 5*time.Second, time.Since(started))
			require.Error(t, ctx.Err())
			closed := make(chan error, 1)
			go func() { closed <- p.close() }()
			synctest.Wait()
			select {
			case <-closed:
				t.Fatal("released ownership before entered write returned")
			default:
			}
			require.Error(t, p.publish(t.Context(), nil))
			require.Equal(t, int32(1), writes.Load())
			close(release)
			require.Error(t, <-closed)
			require.Error(t, p.publish(t.Context(), nil))
			require.Equal(t, int32(1), writes.Load())
		},
	)
}
