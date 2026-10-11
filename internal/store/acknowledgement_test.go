package store_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/stretchr/testify/require"
	"modernc.org/sqlite"
)

type commitFault struct {
	mu              sync.Mutex
	target          string
	occurrence      int
	seen            int
	failed          bool
	commits         []string
	writesAfterLoss int
}

func (f *commitFault) acknowledge(operation string) error {
	if operation == "" {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits = append(f.commits, operation)
	if operation == f.target {
		f.seen++
		if f.seen == f.occurrence {
			f.failed = true
			return errors.New("synthetic lost commit acknowledgement")
		}
	}
	return nil
}

type acknowledgementDriver struct{ fault *commitFault }

func (d acknowledgementDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return &acknowledgementConn{Conn: c, fault: d.fault}, nil
}

type acknowledgementConn struct {
	driver.Conn
	fault     *commitFault
	operation string
}

func (c *acknowledgementConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.operation = ""
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return acknowledgementTx{Tx: tx, conn: c}, nil
}

func (c *acknowledgementConn) ExecContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Result, error) {
	if strings.HasPrefix(query, "INSERT ") || strings.HasPrefix(query, "UPDATE ") ||
		strings.HasPrefix(query, "DELETE ") {
		c.fault.mu.Lock()
		if c.fault.failed {
			c.fault.writesAfterLoss++
		}
		c.fault.mu.Unlock()
	}
	switch {
	case strings.Contains(query, "INSERT INTO validation_batches"):
		c.operation = "batch"
	case strings.Contains(query, "INSERT INTO validation_attempt_details"):
		c.operation = "attempt"
	case strings.Contains(query, "UPDATE validation_attempts SET outcome=") && strings.Contains(query, "finished_at"):
		c.operation = "completion"
	case strings.Contains(query, "UPDATE runs SET status=") && strings.Contains(query, "finished_at"):
		c.operation = "finalization"
	case c.operation == "" && (strings.HasPrefix(query, "INSERT ") || strings.HasPrefix(query, "UPDATE ")):
		c.operation = "other"
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func (c *acknowledgementConn) QueryContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

type acknowledgementTx struct {
	driver.Tx
	conn *acknowledgementConn
}

func (tx acknowledgementTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	// SQLite has committed. Only the acknowledgement returned to the store is lost.
	return tx.conn.fault.acknowledge(tx.conn.operation)
}

type acknowledgementBackend struct {
	run func(context.Context, validator.Input, validator.Hooks) (validator.Result, error)
}

func (*acknowledgementBackend) Identity() (string, string) { return "acknowledgement_fixture", "1" }
func (*acknowledgementBackend) Check(context.Context, validator.Input) (validator.Result, error) {
	panic("batch ownership bypassed")
}
func (b *acknowledgementBackend) Acquire(context.Context) (validator.BatchOwner, error) {
	return b, nil
}
func (*acknowledgementBackend) Close() error { return nil }
func (b *acknowledgementBackend) Run(
	ctx context.Context, in validator.Input, hooks validator.Hooks,
) (validator.Result, error) {
	return b.run(ctx, in, hooks)
}

var acknowledgementDriverID atomic.Int64

// Spec 0005 AC-6 and AC-8: lost acknowledgements do not authorize later writes or replacement evidence.
func TestBatchLostCommitAcknowledgements(t *testing.T) {
	for _, scenario := range []string{"batch", "attempt", "retry_completion", "confirmed_completion", "finalization"} {
		t.Run(scenario, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			seedAcknowledgementCandidates(t, dir)
			fault := &commitFault{target: scenario, occurrence: 1}
			if strings.HasSuffix(scenario, "completion") {
				fault.target = "completion"
			}
			if scenario == "attempt" {
				fault.occurrence = 2
			}
			name := fmt.Sprintf("acknowledgement_%d", acknowledgementDriverID.Add(1))
			sql.Register(name, acknowledgementDriver{fault: fault})
			restore := store.SwapDatabaseOpenerForTest(func(_ string, dsn string) (*sql.DB, error) {
				return sql.Open(name, dsn)
			})
			defer restore()

			var started, cleaned, remoteCommits atomic.Int32
			secondStarted := make(chan struct{})
			backend := &acknowledgementBackend{}
			backend.run = func(ctx context.Context, in validator.Input, h validator.Hooks) (validator.Result, error) {
				started.Add(1)
				if in.CandidateID == 2 {
					close(secondStarted)
				}
				if scenario == "attempt" || (strings.HasSuffix(scenario, "completion") && in.CandidateID == 2) {
					<-ctx.Done()
				} else if strings.HasSuffix(scenario, "completion") {
					<-secondStarted
				}
				// Independent remote cleanup and its journal must still run after a store failure.
				if err := h.Commit(func() error { remoteCommits.Add(1); return nil }); err != nil {
					return validator.Result{}, err
				}
				cleaned.Add(1)
				result := validator.Result{
					Outcome: state.Inconclusive, ReasonCode: "validation_timeout", FailureClass: state.Temporary,
					CleanupProven: true, Removed: true,
				}
				if scenario == "confirmed_completion" || scenario == "finalization" {
					result.Outcome, result.ReasonCode, result.FailureClass = state.Confirmed, "", ""
					result.Evidence = &state.Evidence{
						Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: time.Now(),
					}
					if err := h.Observe(result); err != nil {
						return result, err
					}
				}
				return result, nil
			}
			a := &app.App{
				Settings: config.Settings{
					StateDir: dir, RecheckInterval: time.Hour, CheckPolicy: state.DefaultCheckPolicy(),
				},
				Clock: time.Now, Validator: backend,
				RetryDraw: func(low, _ time.Duration) time.Duration { return low },
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err := a.CheckBatch(ctx, []int64{1, 2}, nil)
			require.EqualError(t, err, "state_error")
			require.NoError(t, ctx.Err(), "the fault must stop work without a test timeout")
			fault.mu.Lock()
			failed, after := fault.failed, fault.writesAfterLoss
			commits := append([]string(nil), fault.commits...)
			fault.mu.Unlock()
			require.True(t, failed, "target commit was not reached: %v", commits)
			require.Zero(t, after, "writes continued after lost acknowledgement: %v", commits)
			require.Equal(t, started.Load(), cleaned.Load())
			require.Equal(t, cleaned.Load(), remoteCommits.Load())
			switch scenario {
			case "batch":
				require.Zero(t, started.Load())
			case "attempt":
				require.Equal(t, int32(1), started.Load())
			default:
				require.Equal(t, int32(2), started.Load())
			}
			restore()
			verifyAcknowledgementHistory(t, dir, scenario)
		})
	}
}

func seedAcknowledgementCandidates(t *testing.T, dir string) {
	t.Helper()
	w, err := store.OpenWriter(t.Context(), dir, time.Now)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: time.Now()})
	require.NoError(t, err)
	_, err = w.RecordMessage(t.Context(), state.MessageInput{
		RunID: run.ID, SourceID: "fixture", MessageID: "1", ObservedAt: time.Now(),
		Keys: []string{"00000001-12345678-AbCdEf01", "00000002-12345678-AbCdEf01"},
	})
	require.NoError(t, err)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: time.Now()})
	require.NoError(t, err)
}

func verifyAcknowledgementHistory(t *testing.T, dir, scenario string) {
	t.Helper()
	r, err := store.OpenReader(t.Context(), dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	runs, err := r.ListRuns(t.Context(), state.Validation, state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, runs.Items, 1, "the failed acknowledgement must still have committed its run")
	id := runs.Items[0].ID
	before, err := r.GetValidationReport(t.Context(), id)
	require.NoError(t, err)
	switch scenario {
	case "batch":
		require.Equal(t, 2, before.Totals.Pending)
	case "attempt":
		require.Equal(t, 2, before.Totals.Unresolved)
	case "retry_completion":
		require.Equal(t, 1, before.Totals.Inconclusive)
		require.Equal(t, 1, before.Totals.Unresolved)
		attempts, err := r.ListBatchAttempts(t.Context(), id, 1, state.PageRequest{})
		require.NoError(t, err)
		require.Len(t, attempts.Items, 1)
		require.Equal(t, 5*time.Second, attempts.Items[0].RetryWait)
		require.NotNil(t, attempts.Items[0].RetryEligibleAt)
	case "confirmed_completion":
		require.Equal(t, 1, before.Totals.Confirmed)
		require.Equal(t, 1, before.Totals.Unresolved)
	case "finalization":
		require.Equal(t, state.Completed, before.Run.Status)
		require.Equal(t, 2, before.Totals.Confirmed)
	}
	require.NoError(t, r.Close())
	w, err := store.OpenWriter(t.Context(), dir, time.Now)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	after, err := w.GetValidationReport(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, before.Totals.Confirmed, after.Totals.Confirmed, "recovery replaced committed evidence")
	require.Zero(t, after.Totals.Unresolved)
	if scenario == "finalization" {
		require.Equal(t, before, after)
	} else {
		require.Equal(t, state.Interrupted, after.Run.Status)
	}
	for _, candidate := range []int64{1, 2} {
		attempts, err := w.ListBatchAttempts(t.Context(), id, candidate, state.PageRequest{})
		require.NoError(t, err)
		require.LessOrEqual(t, len(attempts.Items), 1, "recovery spent the interrupted batch's retry budget")
	}
}
