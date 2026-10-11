package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// publisher serializes observations and retains entered operations after a deadline.
type publisher struct {
	token    chan struct{}
	entered  sync.WaitGroup
	root     string
	current  Observation
	failed   error
	poisoned atomic.Bool
	cancel   context.CancelCauseFunc
	write    func(string, any) error
}

func newPublisher(root string, o Observation, cancel context.CancelCauseFunc) *publisher {
	p := &publisher{token: make(chan struct{}, 1), root: root, current: o, cancel: cancel, write: writeRecord}
	p.token <- struct{}{}
	return p
}

func (p *publisher) publish(parent context.Context, change func(*Observation)) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		p.poisoned.Store(true)
		p.cancel(failure("service_observation_failed"))
		return failure("service_observation_failed")
	}
	select {
	case <-p.token:
	case <-ctx.Done():
		p.poisoned.Store(true)
		p.cancel(failure("service_observation_failed"))
		return failure("service_observation_failed")
	}
	defer func() { p.token <- struct{}{} }()
	if p.poisoned.Load() && p.failed == nil {
		p.failed = failure("service_observation_failed")
	}
	if p.failed != nil {
		return p.failed
	}
	if change != nil {
		change(&p.current)
	}
	p.current.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	value := p.current
	finished := make(chan error, 1)
	p.entered.Add(1)
	go func() {
		defer p.entered.Done()
		finished <- p.write(filepath.Join(p.root, "observation.json"), value)
	}()
	select {
	case err := <-finished:
		deadline, _ := ctx.Deadline()
		if err == nil && ctx.Err() == nil && time.Now().Before(deadline) && !p.poisoned.Load() {
			return nil
		}
	case <-ctx.Done():
	}
	p.failed = failure("service_observation_failed")
	p.cancel(p.failed)
	return p.failed
}

func (p *publisher) close() error {
	p.entered.Wait()
	<-p.token
	defer func() { p.token <- struct{}{} }()
	return p.failed
}

func managedCode(err error) string {
	if err == nil {
		return ""
	}
	var own *Error
	if errors.As(err, &own) {
		return own.Code
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "service_runner_failed"
}
