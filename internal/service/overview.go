package service

import (
	"context"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

func overviewComponent[T any](data *T, err error) state.OverviewComponent[T] {
	c := component(nil, err)
	policy := "replace"
	if c.Availability == "missing" {
		policy = "discard"
	}
	if c.Availability == "unavailable" {
		policy = "retain"
	}
	if err != nil {
		data = nil
	}
	return state.OverviewComponent[T]{
		Availability: c.Availability, ObservedAt: c.ObservedAt,
		CachePolicy: policy, ErrorCode: c.ErrorCode, Data: data,
	}
}

// UnavailableOverview reports a failed service observation without hiding the collection.
func UnavailableOverview(err error) state.OverviewService {
	out := state.OverviewService{IdentityState: "unknown", IdentityCachePolicy: "discard", CollectionMatch: "unknown"}
	out.Installation = overviewComponent[state.OverviewInstallation](nil, err)
	out.Manager = overviewComponent[state.OverviewManager](nil, err)
	out.Runner = overviewComponent[state.OverviewRunner](nil, err)
	out.Installation.CachePolicy, out.Manager.CachePolicy, out.Runner.CachePolicy = "discard", "discard", "discard"
	return out
}

// OverviewStatus observes the service without opening its bound collection.
// The caller applies session cache identity and selected collection comparison.
func OverviewStatus(ctx context.Context, a Account) state.OverviewService {
	m, firstErr := a.manifest()
	out := UnavailableOverview(firstErr)
	if firstErr == nil {
		out.Installation = overviewComponent(
			&state.OverviewInstallation{
				InstallationID: m.InstallationID, Phase: m.Phase, StateDir: m.StateDir,
				AutostartEnabled: m.AutostartEnabled,
			}, nil,
		)
		native, err := inspect(ctx, a, &m)
		out.Manager = overviewComponent(
			&state.OverviewManager{
				Loaded: native.Loaded, Running: native.Running, PID: native.PID,
				LastExitCode: native.LastExitCode, LastSignal: native.LastSignal,
				ActiveState: native.ActiveState, PendingJob: native.PendingJob,
				AutostartObserved: native.AutostartObserved,
			}, err,
		)
		runner, observed, err := readRunnerStatus(a, &m)
		r := state.OverviewRunner{
			LockHeld: runner.LockHeld, OwnerPID: runner.OwnerPID,
			InstallationMatches: runner.InstallationMatches, Stale: runner.Stale, ClockAnomaly: runner.ClockAnomaly,
		}
		if o := runner.Observation; o != nil {
			r.Observation = &state.OverviewObservation{
				Phase: o.Phase, CrawlPhase: o.CrawlPhase,
				ValidationPhase: o.ValidationPhase, StartedAt: o.StartedAt, UpdatedAt: o.UpdatedAt,
				StoppedAt: o.StoppedAt, FailureCode: o.FailureCode,
			}
		}
		out.Runner = overviewComponent(&r, err)
		out.Runner.ObservedAt, out.Runner.ClockAnomaly = observed, r.ClockAnomaly
	}
	last, lastErr := a.manifest()
	if ctx.Err() != nil {
		return UnavailableOverview(ctx.Err())
	}
	switch {
	case firstErr == nil && lastErr == nil && m.InstallationID == last.InstallationID:
		out.IdentityState, out.InstallationID, out.BoundStateDir = "verified", &m.InstallationID, &m.StateDir
	case missing(firstErr) && missing(lastErr):
		out = UnavailableOverview(failure("service_not_installed"))
		out.IdentityState = "missing"
		out.Installation.Availability, out.Manager.Availability, out.Runner.Availability = "missing", "missing", "missing"
	case (firstErr == nil || missing(firstErr)) && (lastErr == nil || missing(lastErr)):
		out = UnavailableOverview(failure("service_changed"))
		out.IdentityState = "changed"
	default:
		if lastErr != nil {
			out = UnavailableOverview(lastErr)
		} else {
			out = UnavailableOverview(firstErr)
		}
	}
	return out
}

// CompareOverviewCollection compares canonical identities without creating paths.
func CompareOverviewCollection(selected string, out *state.OverviewService) {
	if out.IdentityState != "verified" || out.BoundStateDir == nil {
		return
	}
	a, err := canonical(selected)
	if err != nil {
		return
	}
	b, err := canonical(*out.BoundStateDir)
	if err != nil {
		return
	}
	out.CollectionMatch = "different"
	if a == b {
		out.CollectionMatch = "match"
	}
}
