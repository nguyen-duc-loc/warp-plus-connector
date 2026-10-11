package app

import (
	"context"
	"sync"
	"time"
)

// shutdownWindow gives all lanes one remote deadline and one finalization deadline.
type shutdownWindow struct {
	parent        context.Context
	mu            sync.Mutex
	stopAt        time.Time
	localDeadline time.Time
	remote        int
	ready         chan struct{}
	once          sync.Once
	cleanup       context.Context
	cancelCleanup context.CancelFunc
}

func newShutdownWindow(parent context.Context) *shutdownWindow {
	ctx, cancel := context.WithCancel(context.Background())
	return &shutdownWindow{parent: parent, ready: make(chan struct{}), cleanup: ctx, cancelCleanup: cancel}
}
func (s *shutdownWindow) startRemote() { s.mu.Lock(); s.remote++; s.mu.Unlock() }
func (s *shutdownWindow) finishRemote() {
	if s.parent.Err() != nil {
		s.stop()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remote--
	if s.remote == 0 && !s.stopAt.IsZero() {
		s.finalizeLocked()
	}
}
func (s *shutdownWindow) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopAt.IsZero() {
		return
	}
	s.stopAt = time.Now()
	time.AfterFunc(60*time.Second, func() { s.cancelCleanup(); s.mu.Lock(); defer s.mu.Unlock(); s.finalizeLocked() })
	if s.remote == 0 {
		s.finalizeLocked()
	}
}
func (s *shutdownWindow) finalizeLocked() {
	s.once.Do(
		func() {
			s.localDeadline = time.Now().Add(5 * time.Second)
			if cap := s.stopAt.Add(65 * time.Second); cap.Before(s.localDeadline) {
				s.localDeadline = cap
			}
			close(s.ready)
		},
	)
}
func (s *shutdownWindow) localContext() (context.Context, context.CancelFunc) {
	if s.parent.Err() != nil {
		s.stop()
	}
	s.mu.Lock()
	stopping := !s.stopAt.IsZero()
	s.mu.Unlock()
	if !stopping {
		return context.WithTimeout(context.Background(), 5*time.Second)
	}
	<-s.ready
	s.mu.Lock()
	deadline := s.localDeadline
	s.mu.Unlock()
	return context.WithDeadline(context.Background(), deadline)
}
