package app

import (
	"sync"
	"time"
)

// OperationGuard serializes mutation admission with the process shutdown deadline.
type OperationGuard struct {
	mu              sync.Mutex
	active, closing bool
	stop, deadline  time.Time
}

// Arm reserves mutation admission unless session shutdown has already begun.
func (g *OperationGuard) Arm() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing || g.active {
		return false
	}
	g.active = true
	g.stop = time.Time{}
	return true
}

// Stop fixes the first cancellation time without extending it on repeated calls.
func (g *OperationGuard) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active && g.stop.IsZero() {
		g.stop = time.Now()
	}
}

// Disarm is permitted only after the operation's entered resource closure.
func (g *OperationGuard) Disarm() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active = false
	if !g.closing {
		g.stop = time.Time{}
	}
}

// Shutdown closes admission and fixes the process deadline once.
func (g *OperationGuard) Shutdown() time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closing {
		g.closing = true
		now := time.Now()
		g.deadline = now.Add(5 * time.Second)
		if g.active {
			if g.stop.IsZero() {
				g.stop = now
			}
			g.deadline = minTime(now.Add(75*time.Second), g.stop.Add(75*time.Second))
		}
	}
	return g.deadline
}

// Deadline includes cancellation while the terminal is still open.
func (g *OperationGuard) Deadline() time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return g.deadline
	}
	if g.active && !g.stop.IsZero() {
		return g.stop.Add(75 * time.Second)
	}
	return time.Time{}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
