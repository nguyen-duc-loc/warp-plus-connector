package store

import "context"

// SQLite's native busy handler can ignore sqlite3_interrupt until its bounded
// wait ends. Keep entered work and its resources alive until cleanup finishes,
// while allowing its caller to observe cancellation immediately. No transaction
// is retried. Close still waits for all entered work before releasing the lock.
// Inputs captured by work must be owned copies, never caller mutable pointers.
func awaitContext[T any](ctx context.Context, work func() (T, error), discard func(T)) (T, error) {
	if ctx.Done() == nil {
		return work()
	}
	type result struct {
		value T
		err   error
	}
	ready := make(chan result)
	go func() {
		value, err := work()
		select {
		case ready <- result{value, err}:
		case <-ctx.Done():
			if discard != nil {
				discard(value)
			}
		}
	}()
	select {
	case out := <-ready:
		return out.value, out.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
