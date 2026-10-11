package store_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/source/telegram"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/stretchr/testify/require"
)

type scheduleAcknowledgementDriver struct{ acknowledgementDriver }

func (d scheduleAcknowledgementDriver) Open(name string) (driver.Conn, error) {
	c, err := d.acknowledgementDriver.Open(name)
	if err != nil {
		return nil, err
	}
	return &scheduleAcknowledgementConn{c.(*acknowledgementConn)}, nil
}

type scheduleAcknowledgementConn struct{ *acknowledgementConn }

func (c *scheduleAcknowledgementConn) ExecContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Result, error) {
	result, err := c.acknowledgementConn.ExecContext(ctx, query, args)
	if strings.Contains(query, "UPDATE crawl_pages SET status='parsed'") {
		c.operation = "crawl_parse"
	}
	return result, err
}

type scheduleAcknowledgementBackend struct{ acknowledgementBackend }

func (b *scheduleAcknowledgementBackend) AcquireScheduled(
	context.Context, func() time.Time, time.Duration,
) (validator.Acquisition, error) {
	return validator.Acquisition{Owner: b, CleanupInspected: true}, nil
}

type scheduleAcknowledgementSource struct {
	fetch func(context.Context) (telegram.Page, error)
}

func (s scheduleAcknowledgementSource) FetchPage(
	ctx context.Context, _ string, _ *int64,
) (telegram.Page, error) {
	return s.fetch(ctx)
}

// Spec 0007 AC-10, AC-12, AC-14: either lane's lost commit acknowledgement keeps
// its state diagnostic, poisons both SQL lanes, and permits independent cleanup.
func TestRunLostCommitAcknowledgements(t *testing.T) {
	for _, scenario := range []string{"crawl_parse", "completion"} {
		t.Run(scenario, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			seedAcknowledgementCandidates(t, dir)
			fault := &commitFault{target: scenario, occurrence: 1}
			name := fmt.Sprintf("schedule_acknowledgement_%d", acknowledgementDriverID.Add(1))
			sql.Register(name, scheduleAcknowledgementDriver{acknowledgementDriver{fault: fault}})
			restore := store.SwapDatabaseOpenerForTest(func(_ string, dsn string) (*sql.DB, error) {
				return sql.Open(name, dsn)
			})
			defer restore()

			var started, cleaned, remoteCommits, sourceCalls, sourceCancelled atomic.Int32
			workerStarted, sourceStarted := make(chan struct{}), make(chan struct{})
			var workerOnce, sourceOnce sync.Once
			backend := &scheduleAcknowledgementBackend{}
			backend.run = func(ctx context.Context, _ validator.Input, h validator.Hooks) (validator.Result, error) {
				started.Add(1)
				workerOnce.Do(func() { close(workerStarted) })
				if scenario == "crawl_parse" {
					<-ctx.Done()
				} else {
					select {
					case <-sourceStarted:
					case <-ctx.Done():
					}
				}
				if err := h.Commit(func() error { remoteCommits.Add(1); return nil }); err != nil {
					return validator.Result{}, err
				}
				cleaned.Add(1)
				return validator.Result{
					Outcome: state.Inconclusive, ReasonCode: "controlled_terminal", FailureClass: state.Terminal,
					CleanupProven: true, Removed: true,
				}, nil
			}
			source := scheduleAcknowledgementSource{fetch: func(ctx context.Context) (telegram.Page, error) {
				sourceCalls.Add(1)
				sourceOnce.Do(func() { close(sourceStarted) })
				if scenario == "completion" {
					<-ctx.Done()
					sourceCancelled.Add(1)
					return telegram.Page{}, ctx.Err()
				}
				select {
				case <-workerStarted:
				case <-ctx.Done():
					return telegram.Page{}, ctx.Err()
				}
				return telegram.Page{Facts: state.CrawlPage{FetchedAt: time.Now()}}, nil
			}}
			a := &app.App{
				Clock: time.Now, Source: source, Validator: backend,
				Settings: config.Settings{
					StateDir: dir, Sources: []config.Source{{Channel: "warpplus", Enabled: true, PageLimit: 1}},
					CrawlEnabled: true, ValidationEnabled: true,
					CrawlInterval: time.Hour, RecheckInterval: 6 * time.Hour, FailureInterval: time.Hour,
					CheckPolicy: state.DefaultCheckPolicy(), Validator: "docker",
					ValidatorImage: "sha256:" + strings.Repeat("a", 64),
				},
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := a.Run(ctx, nil)
			require.NoError(t, ctx.Err(), "the fault must stop work while the parent remains live")
			fault.mu.Lock()
			failed, after := fault.failed, fault.writesAfterLoss
			commits := append([]string(nil), fault.commits...)
			fault.mu.Unlock()
			require.True(t, failed, "target commit was not reached: %v", commits)
			require.Zero(t, after, "writes continued after lost acknowledgement: %v", commits)
			require.Positive(t, started.Load(), "validation must overlap the crawl")
			require.Equal(t, started.Load(), cleaned.Load())
			require.Equal(t, cleaned.Load(), remoteCommits.Load())
			require.Equal(t, int32(1), sourceCalls.Load())
			if scenario == "completion" {
				require.Equal(t, int32(1), sourceCancelled.Load())
			}
			require.EqualError(t, err, "state_error")
		})
	}
}
