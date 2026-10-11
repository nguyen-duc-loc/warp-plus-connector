package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// Reader inspects committed state. Its zero value is closed.
type Reader struct{ h *handle }

// Writer owns the advisory lock. Read methods use a separate connection.
type Writer struct{ Reader }

// Keep database construction replaceable in package tests that inject driver failures.
var openDatabase = sql.Open

type handle struct {
	mu      sync.RWMutex
	closed  bool
	readDB  *sql.DB
	writeDB *sql.DB
	lock    *os.File
}

// OpenWriter initializes, migrates, and recovers private state while holding the
// exclusive writer lock. The clock supplies one recovery timestamp.
func OpenWriter(ctx context.Context, stateDir string, clock func() time.Time) (*Writer, error) {
	return awaitContext(
		ctx, func() (*Writer, error) { return openWriter(ctx, stateDir, clock) }, func(w *Writer) {
			if w != nil {
				_ = w.Close()
			}
		},
	)
}

func openWriter(ctx context.Context, stateDir string, clock func() time.Time) (*Writer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if clock == nil {
		return nil, ErrInvalidInput
	}
	dir, err := prepareDir(stateDir, true)
	if err != nil {
		return nil, err
	}
	h := &handle{}
	success := false
	defer func() {
		if !success {
			_ = h.close()
		}
	}()
	h.lock, err = privateFile(filepath.Join(dir, "writer.lock"), true)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(h.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrBusy
		}
		return nil, safeError(ctx, "lock state", err)
	}
	if err = checkFiles(dir); err != nil {
		return nil, err
	}
	f, err := privateFile(filepath.Join(dir, "state.db"), true)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, safeError(ctx, "close state file", err)
	}
	h.writeDB, err = openDB(ctx, dir, false)
	if err != nil {
		return nil, safeError(ctx, "open writer", err)
	}
	var version int
	if err = h.writeDB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return nil, safeError(ctx, "read schema", err)
	}
	if version > schemaVersion {
		return nil, ErrSchemaTooNew
	}
	var mode string
	if err = h.writeDB.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return nil, safeError(ctx, "prepare journal", err)
	}
	if mode != "wal" {
		return nil, ErrStateAccess
	}
	if err = migrate(ctx, h.writeDB, []string{initialSchema, coverageSchema, planSchema, batchSchema}); err != nil {
		return nil, safeError(ctx, "migrate state", err)
	}
	recovery, err := normalizeTime(clock())
	if err != nil {
		return nil, err
	}
	if err = recoverState(ctx, h.writeDB, recovery); err != nil {
		return nil, safeError(ctx, "recover state", err)
	}
	h.readDB, err = openDB(ctx, dir, true)
	if err != nil {
		return nil, safeError(ctx, "open writer reads", err)
	}
	if err = checkFiles(dir); err != nil {
		return nil, err
	}
	success = true
	return &Writer{Reader{h: h}}, nil
}

// OpenReader requires initialized state at the current schema version. It never
// initializes, migrates, recovers, or changes journal mode.
func OpenReader(ctx context.Context, stateDir string) (*Reader, error) {
	return awaitContext(
		ctx, func() (*Reader, error) { return openReader(ctx, stateDir) }, func(r *Reader) {
			if r != nil {
				_ = r.Close()
			}
		},
	)
}

func openReader(ctx context.Context, stateDir string) (*Reader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := prepareDir(stateDir, false)
	if err != nil {
		return nil, err
	}
	if err = checkFiles(dir); err != nil {
		return nil, err
	}
	if _, err = os.Lstat(filepath.Join(dir, "state.db")); errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotInitialized
	}
	h := &handle{}
	h.readDB, err = openDB(ctx, dir, true)
	if err != nil {
		return nil, safeError(ctx, "open reader", err)
	}
	success := false
	defer func() {
		if !success {
			_ = h.close()
		}
	}()
	var version int
	if err = h.readDB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return nil, safeError(ctx, "read schema", err)
	}
	if version > schemaVersion {
		return nil, ErrSchemaTooNew
	}
	if version < schemaVersion {
		return nil, ErrMigrationRequired
	}
	if err = checkFiles(dir); err != nil {
		return nil, err
	}
	success = true
	return &Reader{h: h}, nil
}

func openDB(ctx context.Context, dir string, readOnly bool) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: filepath.Join(dir, "state.db")}
	q := url.Values{}
	q.Set("mode", "rw")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	if readOnly {
		q.Set("mode", "ro")
	} else {
		q.Add("_pragma", "synchronous(FULL)")
		q.Set("_txlock", "immediate")
	}
	u.RawQuery = q.Encode()
	db, err := openDatabase("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func prepareDir(path string, create bool) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrInvalidInput
	}
	if create {
		if err := os.MkdirAll(path, 0700); err != nil {
			return "", safeError(context.Background(), "create state directory", err)
		}
	}
	dir, err := filepath.EvalSymlinks(path)
	if err != nil {
		if !create && errors.Is(err, os.ErrNotExist) {
			return "", ErrNotInitialized
		}
		return "", safeError(context.Background(), "resolve state directory", err)
	}
	if err = checkPrivate(dir, true); err != nil {
		return "", err
	}
	return dir, nil
}

func checkPrivate(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	validType := info.Mode().IsRegular()
	mode := "0600"
	if directory {
		validType = info.IsDir()
		mode = "0700"
	}
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0077 != 0 || !validType {
		return fmt.Errorf(
			"state path %q requires account ownership and private mode %s: %w",
			path,
			mode,
			ErrStateAccess,
		)
	}
	return nil
}

func checkFiles(dir string) error {
	for _, name := range []string{"state.db", "writer.lock", "state.db-wal", "state.db-shm", "state.db-journal"} {
		if err := checkPrivate(filepath.Join(dir, name), false); err != nil && !errors.Is(err, os.ErrNotExist) {
			if errors.Is(err, ErrStateAccess) {
				return err
			}
			return safeError(context.Background(), "inspect state file", err)
		}
	}
	return nil
}

func privateFile(path string, create bool) (*os.File, error) {
	if err := checkPrivate(path, false); err != nil && !errors.Is(err, os.ErrNotExist) {
		if errors.Is(err, ErrStateAccess) {
			return nil, err
		}
		return nil, safeError(context.Background(), "inspect state file", err)
	}
	flags := os.O_RDWR | syscall.O_NOFOLLOW
	if create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, safeError(context.Background(), "open private state file", err)
	}
	if err = checkPrivate(path, false); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// Close stops new calls, waits for entered calls, closes all database connections,
// and finally releases writer ownership. Calling it twice is safe.
func (r *Reader) Close() error {
	if r == nil || r.h == nil {
		return nil
	}
	return r.h.close()
}

func (h *handle) close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	var err error
	if h.readDB != nil {
		err = errors.Join(err, h.readDB.Close())
	}
	if h.writeDB != nil {
		err = errors.Join(err, h.writeDB.Close())
	}
	if h.lock != nil {
		err = errors.Join(err, h.lock.Close())
	}
	return safeError(context.Background(), "close state", err)
}

func transact[T any](ctx context.Context, r *Reader, write bool, op string, fn func(*sql.Tx) (T, error)) (T, error) {
	return transactOwned(ctx, r, write, op, false, fn)
}

// Synchronous operations retain caller owned callback resources until all work ends.
func transactOwned[T any](
	ctx context.Context, r *Reader, write bool, op string, synchronous bool, fn func(*sql.Tx) (T, error),
) (T, error) {
	var zero T
	if r == nil || r.h == nil {
		return zero, ErrClosed
	}
	h := r.h
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return zero, ErrClosed
	}
	db := h.readDB
	if write {
		db = h.writeDB
	}
	if db == nil {
		h.mu.RUnlock()
		return zero, ErrClosed
	}
	work := func() (T, error) {
		defer h.mu.RUnlock()
		conn, err := db.Conn(ctx)
		if err != nil {
			return zero, safeError(ctx, op, err)
		}
		defer func() { _ = conn.Close() }()
		// Bound SQLite's native wait by a known earlier deadline. Explicit
		// cancellation is handled by awaitContext without releasing ownership early.
		timeout := 5000
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return zero, safeError(ctx, op, context.DeadlineExceeded)
			}
			if remaining < 5*time.Second {
				timeout = int((remaining + time.Millisecond - 1) / time.Millisecond)
			}
		}
		if _, err = conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", timeout)); err != nil {
			return zero, safeError(ctx, op, err)
		}
		defer func() { _, _ = conn.ExecContext(context.Background(), "PRAGMA busy_timeout=5000") }()
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: !write})
		if err != nil {
			return zero, safeError(ctx, op, err)
		}
		defer func() { _ = tx.Rollback() }()
		if err = ctx.Err(); err != nil {
			return zero, safeError(ctx, op, err)
		}
		result, err := fn(tx)
		if err != nil {
			return zero, safeError(ctx, op, err)
		}
		if err = tx.Commit(); err != nil {
			return zero, safeError(ctx, op, err)
		}
		return result, nil
	}
	if synchronous {
		return work()
	}
	return awaitContext(ctx, work, nil)
}

func recoverState(ctx context.Context, db *sql.DB, at time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(
		ctx,
		"UPDATE validation_attempts SET outcome='inconclusive', "+
			"reason_code='interrupted', recovered_at=? WHERE outcome IS NULL",
		at.UnixMicro(),
	); err != nil {
		return err
	}
	for _, query := range []string{
		"UPDATE validation_items SET status='interrupted',stop_reason='interrupted'," +
			"recovered_at=? WHERE status IN ('pending','active')",
		"UPDATE crawl_pages SET status='interrupted'," +
			"reason_code='interrupted',recovered_at=? WHERE status IN " +
			"('requested','parsed') AND run_id IN (SELECT id FROM runs WHERE " +
			"status='running')",
		"UPDATE crawl_sources SET status='interrupted'," +
			"stop_reason='interrupted',recovered_at=? WHERE status='running' AND " +
			"run_id IN (SELECT id FROM runs WHERE status='running')",
		"UPDATE crawl_sources SET status='skipped'," +
			"stop_reason='interrupted_before_start',recovered_at=? WHERE " +
			"status='pending' AND run_id IN (SELECT id FROM runs WHERE " +
			"status='running')",
	} {
		if _, err = tx.ExecContext(ctx, query, at.UnixMicro()); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(
		ctx,
		"UPDATE runs SET status='interrupted', reason_code='interrupted', recovered_at=? WHERE status='running'",
		at.UnixMicro(),
	); err != nil {
		return err
	}
	return tx.Commit()
}
