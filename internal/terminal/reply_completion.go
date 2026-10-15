package terminal

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"time"
)

var errReplyDeadline = errors.New("reply publication deadline")

// replyCompletion keeps visible replies and their admission signals in order.
type replyCompletion struct {
	publishing atomic.Bool
	done       chan error
}

func (c *replyCompletion) write(w io.Writer, id string, value any) {
	c.done <- writeReply(publishingWriter{w, &c.publishing}, id, value)
}

type publishingWriter struct {
	io.Writer
	publishing *atomic.Bool
}

func (w publishingWriter) Write(p []byte) (int, error) {
	w.publishing.Store(true)
	return w.Writer.Write(p)
}

func (c *replyCompletion) take(ctx context.Context, deadline <-chan time.Time) (error, bool) {
	// The peer can read a reply before Write returns. A request that follows
	// those bytes must wait for publication to finish before testing admission.
	if c.publishing.Load() {
		select {
		case err := <-c.done:
			c.publishing.Store(false)
			return err, true
		case <-ctx.Done():
			return ctx.Err(), true
		case <-deadline:
			return errReplyDeadline, true
		}
	}
	select {
	case err := <-c.done:
		c.publishing.Store(false)
		return err, true
	default:
		return nil, false
	}
}
