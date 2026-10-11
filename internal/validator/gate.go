package validator

import (
	"context"
	"errors"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// Gate retains an admission restriction under the shared recovery owner.
type Gate struct {
	Phase         string        `json:"phase"`
	BasisAt       time.Time     `json:"basis_at"`
	RetryInterval time.Duration `json:"retry_interval_ns"`
	NotBefore     *time.Time    `json:"not_before,omitempty"`
	Reason        string        `json:"reason"`
}

// Acquisition is a typed local observation; cleanup proof is independent of admission.
type Acquisition struct {
	Owner            BatchOwner
	CleanupInspected bool
	Wait             *state.ValidationWait
	Probe            bool
}

// ScheduledBackend supplies typed admission facts under shared ownership.
type ScheduledBackend interface {
	AcquireScheduled(context.Context, func() time.Time, time.Duration) (Acquisition, error)
}

// GateOwner keeps durable admission intent until local terminal persistence is acknowledged.
type GateOwner interface {
	BatchOwner
	Admission(time.Time) (bool, error)
	BeforeMutation(time.Time) error
	Begin(time.Time, time.Duration) error
	Throttle(time.Time, time.Duration) error
	Resolve(time.Time, bool, bool) error
}

// AcquireScheduled shares the ordinary acquisition budget and never creates an attempt.
func (d *Docker) AcquireScheduled(ctx context.Context, clock func() time.Time, interval time.Duration) (
	Acquisition,
	error,
) {
	var out Acquisition
	if clock == nil || interval <= 0 {
		return out, &Error{Code: "validator_state_error"}
	}
	owner, err := d.acquireAt(ctx, true, clock, interval)
	if owner != nil {
		out.CleanupInspected = owner.inspected
	}
	if err == nil {
		out.Owner = owner
		out.Probe, err = owner.Admission(clock())
		if err == nil {
			return out, nil
		}
	}
	var gate *gateError
	if errors.As(err, &gate) {
		out.Wait = &gate.wait
		out.CleanupInspected = gate.inspected
	}
	if out.Wait == nil {
		var e *Error
		if errors.As(err, &e) {
			reason := ""
			delay := interval
			switch e.Code {
			case "validator_busy":
				reason = "validator_busy"
				delay = 5 * time.Second
			case "validator_backend_unavailable":
				reason = "backend_unavailable"
			case "validation_cleanup_pending":
				reason = "cleanup_pending"
			}
			if reason != "" {
				at := clock().UTC().Truncate(time.Microsecond)
				due, e := state.ScheduleDue(at, delay)
				if e == nil {
					out.Wait = &state.ValidationWait{
						Reason: reason, BasisAt: at, RetryAt: due, ObservedAt: at, Interval: delay,
					}
				}
			}
		}
	}
	if out.Owner != nil {
		_ = out.Owner.Close()
		out.Owner = nil
	}
	return out, err
}

type gateError struct {
	code      string
	wait      state.ValidationWait
	inspected bool
}

func (e *gateError) Error() string { return e.code }
func (e *gateError) Unwrap() error { return &Error{Code: e.code} }

func (o *dockerOwner) Admission(at time.Time) (bool, error) {
	at = at.UTC().Truncate(time.Microsecond)
	if at.Before(o.registry.LastObservedAt) {
		return false, &gateError{
			code: "validator_clock_regressed", inspected: o.inspected, wait: state.ValidationWait{
				Reason: "validator_clock_regressed", BasisAt: at, RetryAt: o.registry.LastObservedAt, ObservedAt: at,
				Interval: o.registry.LastObservedAt.Sub(at),
			},
		}
	}
	if g := o.registry.Gate; g != nil {
		if g.Phase != "cooldown" {
			return false, &Error{Code: "validator_state_error"}
		}
		if at.Before(*g.NotBefore) {
			return false, &gateError{
				code: "validator_cooldown", inspected: o.inspected,
				wait: state.ValidationWait{
					Reason: g.Reason, BasisAt: g.BasisAt, RetryAt: *g.NotBefore, ObservedAt: at,
					Interval: g.RetryInterval,
				},
			}
		}
		return true, nil
	}
	return false, nil
}

func (o *dockerOwner) Begin(at time.Time, interval time.Duration) error {
	probe, err := o.Admission(at)
	if err != nil {
		return err
	}
	phase, reason := "active", "validation_active"
	if probe {
		phase, reason = "probe", "validation_probe"
		interval = o.registry.Gate.RetryInterval
	}
	if interval <= 0 {
		return &Error{Code: "validator_state_error"}
	}
	o.probe = probe
	o.interval = interval
	o.registry.Gate = &Gate{
		Phase: phase, Reason: reason, BasisAt: at.UTC().Truncate(time.Microsecond), RetryInterval: interval,
	}
	o.observeTime(at)
	return o.save(o.registry.Sessions)
}
func (o *dockerOwner) observeTime(at time.Time) {
	at = at.UTC().Truncate(time.Microsecond)
	if at.After(o.registry.LastObservedAt) {
		o.registry.LastObservedAt = at
	}
}

func gatePriority(reason string) int {
	switch reason {
	case "validation_rate_limited":
		return 3
	case "validation_probe_failed":
		return 2
	default:
		return 1
	}
}
func (o *dockerOwner) cooldown(at time.Time, interval, delay time.Duration, reason string) error {
	at = at.UTC().Truncate(time.Microsecond)
	wait := interval
	if delay > wait {
		wait = delay
	}
	due, err := state.ScheduleDue(at, wait)
	if err != nil {
		return &Error{Code: "validator_state_error"}
	}
	g := &Gate{Phase: "cooldown", BasisAt: at, RetryInterval: interval, NotBefore: &due, Reason: reason}
	if old := o.registry.Gate; old != nil && old.Phase == "cooldown" {
		if old.NotBefore.After(due) {
			g.NotBefore = old.NotBefore
			g.BasisAt = old.BasisAt
			g.RetryInterval = old.RetryInterval
		}
		if gatePriority(old.Reason) > gatePriority(g.Reason) {
			g.Reason = old.Reason
		}
	}
	o.registry.Gate = g
	o.observeTime(at)
	return o.save(o.registry.Sessions)
}
func (o *dockerOwner) Throttle(at time.Time, delay time.Duration) error {
	interval := o.interval
	if g := o.registry.Gate; g != nil {
		interval = g.RetryInterval
	}
	if interval <= 0 {
		return &Error{Code: "validator_state_error"}
	}
	// Repeated reporting of the same throttle must not move the deadline.
	if o.throttled {
		return nil
	}
	o.throttled = true
	return o.cooldown(at, interval, delay, "validation_rate_limited")
}
func (o *dockerOwner) Resolve(at time.Time, acknowledged, healthy bool) error {
	if !acknowledged {
		return nil
	}
	g := o.registry.Gate
	if g == nil {
		return nil
	}
	if g.Phase == "cooldown" {
		return nil
	}
	if len(o.registry.Sessions) > 0 || o.probe && !healthy {
		reason := "validation_interrupted"
		if o.probe {
			reason = "validation_probe_failed"
		}
		return o.cooldown(at, g.RetryInterval, 0, reason)
	}
	o.registry.Gate = nil
	o.observeTime(at)
	return o.save(o.registry.Sessions)
}

func (o *dockerOwner) recoverGate(at time.Time) error {
	g := o.registry.Gate
	if g == nil || g.Phase == "cooldown" {
		return nil
	}
	reason := "validation_interrupted"
	if g.Phase == "probe" {
		reason = "validation_probe_failed"
	}
	return o.cooldown(at, g.RetryInterval, 0, reason)
}

func (g *Gate) valid() bool {
	if g == nil {
		return true
	}
	if g.BasisAt.IsZero() || g.BasisAt.Nanosecond()%1000 != 0 || g.RetryInterval <= 0 {
		return false
	}
	switch g.Phase {
	case "active":
		return g.NotBefore == nil && g.Reason == "validation_active"
	case "probe":
		return g.NotBefore == nil && g.Reason == "validation_probe"
	case "cooldown":
		return g.NotBefore != nil && g.NotBefore.Nanosecond()%1000 == 0 && !g.NotBefore.Before(g.BasisAt) &&
			(g.Reason == "validation_rate_limited" || g.Reason == "validation_probe_failed" ||
				g.Reason == "validation_interrupted")
	}
	return false
}

func (o *dockerOwner) BeforeMutation(at time.Time) error {
	at = at.UTC().Truncate(time.Microsecond)
	if at.Before(o.registry.LastObservedAt) {
		return &gateError{
			code: "validator_clock_regressed", inspected: o.inspected, wait: state.ValidationWait{
				Reason: "validator_clock_regressed", BasisAt: at, ObservedAt: at, RetryAt: o.registry.LastObservedAt,
				Interval: o.registry.LastObservedAt.Sub(at),
			},
		}
	}
	if g := o.registry.Gate; g == nil || (g.Phase != "active" && g.Phase != "probe") {
		return &Error{Code: "validator_cooldown"}
	}
	o.observeTime(at)
	return o.save(o.registry.Sessions)
}

// AcquireCoordinated serializes only durable writes; acquisition I/O remains outside the executor.
func (d *Docker) AcquireCoordinated(
	ctx context.Context, clock func() time.Time, interval time.Duration,
	commit func(func() error) error,
) (Acquisition, error) {
	clone := *d
	clone.mutation = commit
	result, err := clone.AcquireScheduled(ctx, clock, interval)
	if owner, ok := result.Owner.(*dockerOwner); ok {
		owner.docker = d
	}
	return result, err
}

// AdmissionWait returns a copy of a typed shared gate observation, never parsed diagnostic text.
func AdmissionWait(err error) *state.ValidationWait {
	var gate *gateError
	if errors.As(err, &gate) {
		wait := gate.wait
		return &wait
	}
	return nil
}
