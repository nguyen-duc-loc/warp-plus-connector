// Package validator defines isolated entitlement checking and normalized results.
package validator

import (
	"context"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// Result contains normalized entitlement facts, never raw provider output.
type Result struct {
	ProviderResponsive bool
	Outcome            state.Outcome
	ReasonCode         string
	Evidence           *state.Evidence
	FailureClass       state.FailureClass
	ServerDelay        time.Duration
	CleanupProven      bool
	Removed            bool
}

// BatchBackend acquires shared recovery ownership before any run is saved.
type BatchBackend interface {
	Acquire(context.Context) (BatchOwner, error)
}

// BatchOwner retains ownership across concurrent attempts and retry waits.
type BatchOwner interface {
	Run(context.Context, Input, Hooks) (Result, error)
	Close() error
}

// Hooks route journal writes, mutation admission, and evidence through the coordinator.
type Hooks struct {
	Session        func(string) error
	Commit         func(func() error) error
	Admit          func(context.Context, string) error
	MutationDone   func(Result, error) error
	Observe        func(Result) error
	CleanupContext func() (context.Context, context.CancelFunc)
}

// Backend is the external boundary for deliberate candidate checks.
type Backend interface {
	Identity() (id, version string)
	Check(context.Context, Input) (Result, error)
}

// Input associates remote work with the application attempt that requested it.
type Input struct {
	Key         string
	StateDir    string
	CandidateID int64
	RunID       int64
	AttemptID   int64
}

// Unsupported records lack of capability without external operations.
type Unsupported struct{ Version string }

// Identity ties the unsupported result to this executable version.
func (u Unsupported) Identity() (string, string) {
	v := u.Version
	if v == "" {
		v = "dev"
	}
	return "unsupported", v
}

// Check returns inconclusive and performs no network or subprocess work.
func (u Unsupported) Check(ctx context.Context, _ Input) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return Result{
		Outcome: state.Inconclusive, ReasonCode: "validation_unsupported",
		FailureClass: state.Terminal, CleanupProven: true, Removed: true,
	}, nil
}
