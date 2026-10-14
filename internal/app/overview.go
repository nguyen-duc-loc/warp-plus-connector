package app

import (
	"context"
	"errors"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

// ReadOverview waits for independent bounded reads, including reader closure.
// The terminal supervisor owns the cancellation resistant session watchdog.
func (a *App) ReadOverview(ctx context.Context) state.Overview {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	collection := make(chan state.OverviewComponent[state.OverviewCollection], 1)
	service := make(chan state.OverviewService, 1)
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		collection <- a.readOverviewCollection(ctx)
	}()
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if a.OverviewService != nil {
			service <- a.OverviewService(ctx)
			return
		}
		code := "service_account_unavailable"
		at := time.Now().UTC().Truncate(time.Microsecond)
		service <- state.OverviewService{
			IdentityState: "unknown", IdentityCachePolicy: "discard",
			CollectionMatch: "unknown",
			Installation: state.OverviewComponent[state.OverviewInstallation]{
				Availability: "unavailable",
				ObservedAt:   at, CachePolicy: "discard", ErrorCode: &code,
			},
			Manager: state.OverviewComponent[state.OverviewManager]{
				Availability: "unavailable",
				ObservedAt:   at, CachePolicy: "discard", ErrorCode: &code,
			},
			Runner: state.OverviewComponent[state.OverviewRunner]{
				Availability: "unavailable",
				ObservedAt:   at, CachePolicy: "discard", ErrorCode: &code,
			},
		}
	}()
	out := state.Overview{Collection: <-collection, Service: <-service}
	out.HeaderAt = a.Clock().UTC().Truncate(time.Microsecond)
	return out
}

func (a *App) readOverviewCollection(ctx context.Context) state.OverviewComponent[state.OverviewCollection] {
	out := state.OverviewComponent[state.OverviewCollection]{Availability: "available", CachePolicy: "replace"}
	r, err := store.OpenReader(ctx, a.Settings.StateDir)
	if err == nil {
		var data state.OverviewCollection
		data, err = r.GetOverview(ctx, a.Clock, a.Settings.RecheckInterval)
		err = errors.Join(err, r.Close(), ctx.Err())
		if err == nil {
			out.Data, out.ObservedAt, out.ClockAnomaly = &data, data.AsOf, data.ClockAnomaly
			return out
		}
	}
	out.ObservedAt = a.Clock().UTC().Truncate(time.Microsecond)
	out.Availability, out.CachePolicy = "unavailable", "retain"
	if errors.Is(err, store.ErrNotInitialized) {
		out.Availability, out.CachePolicy = "missing", "discard"
	}
	var safe *Error
	_ = errors.As(classified(err), &safe)
	code := "state_error"
	if safe != nil {
		code = safe.Code
	}
	out.ErrorCode = &code
	return out
}
