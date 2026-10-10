// Package validator defines isolated entitlement checking and normalized results.
package validator

import (
	"context"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// Result contains normalized entitlement facts, never raw provider output.
type Result struct {
	Outcome    state.Outcome
	ReasonCode string
	Evidence   *state.Evidence
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
	return Result{Outcome: state.Inconclusive, ReasonCode: "validation_unsupported"}, nil
}
