package terminal

import (
	"context"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
)

type guardKey struct{}

// WithOperationGuard connects terminal admission to the process watchdog.
func WithOperationGuard(ctx context.Context, guard *app.OperationGuard) context.Context {
	return context.WithValue(ctx, guardKey{}, guard)
}

// OperationGuard returns the process owner, or a standalone guard for embedded callers.
func OperationGuard(ctx context.Context) *app.OperationGuard {
	if g, ok := ctx.Value(guardKey{}).(*app.OperationGuard); ok {
		return g
	}
	return &app.OperationGuard{}
}
