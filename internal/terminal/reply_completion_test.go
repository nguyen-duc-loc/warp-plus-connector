package terminal

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type visibleReplyWriter struct {
	visible chan []byte
	release chan struct{}
}

func (w visibleReplyWriter) Write(p []byte) (int, error) {
	w.visible <- append([]byte(nil), p...)
	<-w.release
	return len(p), nil
}

// A child may consume a complete reply before the parent's Write returns.
// Admission must wait for the corresponding completion instead of rejecting it.
func TestReplyCompletion_VisibleReplyBeforeWriteReturns(t *testing.T) {
	c := replyCompletion{done: make(chan error, 1)}
	w := visibleReplyWriter{visible: make(chan []byte, 1), release: make(chan struct{})}
	defer close(w.release)
	go c.write(w, "1", record{})
	require.Contains(t, string(<-w.visible), `"id":"1"`)

	// Cancellation while the visible reply is still being published must win
	// over the old nonblocking overlap rejection, without waiting for Write.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err, ready := c.take(ctx, nil)
	require.True(t, ready)
	require.ErrorIs(t, err, context.Canceled)
}

func TestReplyCompletion_AcceptsFollowingRequest(t *testing.T) {
	c := replyCompletion{done: make(chan error, 1)}
	w := visibleReplyWriter{visible: make(chan []byte, 1), release: make(chan struct{})}
	go c.write(w, "1", record{})
	<-w.visible
	go func() { close(w.release) }()
	err, ready := c.take(t.Context(), nil)
	require.True(t, ready)
	require.NoError(t, err)
	require.False(t, c.publishing.Load())
	_, ready = c.take(t.Context(), nil)
	require.False(t, ready)
}

func TestReplyCompletion_OverlapAndPublicationDeadline(t *testing.T) {
	c := replyCompletion{done: make(chan error, 1)}
	_, ready := c.take(t.Context(), nil)
	require.False(t, ready, "dispatch without a reply remains outstanding")
	c.publishing.Store(true)
	deadline := make(chan time.Time, 1)
	deadline <- time.Now()
	err, ready := c.take(t.Context(), deadline)
	require.True(t, ready)
	require.ErrorIs(t, err, errReplyDeadline)

	c.publishing.Store(false)
	failed := errors.New("dispatch failed")
	c.done <- failed
	err, ready = c.take(t.Context(), nil)
	require.True(t, ready)
	require.ErrorIs(t, err, failed)
}

func TestReplyCompletion_ConcurrentPublicationClearsAdmission(t *testing.T) {
	for range 1000 {
		c := replyCompletion{done: make(chan error, 1)}
		go c.write(new(bytes.Buffer), "1", record{})
		for {
			err, ready := c.take(t.Context(), nil)
			if ready {
				require.NoError(t, err)
				break
			}
			runtime.Gosched()
		}
		require.False(t, c.publishing.Load(), "consumed publication cannot admit an overlapping request")
		_, ready := c.take(t.Context(), nil)
		require.False(t, ready)
	}
}
