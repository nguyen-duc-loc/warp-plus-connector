package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
)

// covers: AC-11.
func TestMigrationRollbackAndSchemaGuards(t *testing.T) {
	ctx := t.Context()
	dir := filepath.Join(t.TempDir(), "state")
	w, err := OpenWriter(ctx, dir, time.Now)
	require.NoError(t, err)
	r, err := w.StartRun(ctx, state.RunInput{RequestID: "original", Kind: state.Crawl, StartedAt: time.Now()})
	require.NoError(t, err)
	err = migrate(
		ctx,
		w.h.writeDB,
		[]string{
			initialSchema, coverageSchema, planSchema, batchSchema,
			"CREATE TABLE rollback_probe(id INTEGER); INSERT INTO table_missing VALUES (1);",
		},
	)
	require.Error(t, err)
	var version, count int
	require.NoError(t, w.h.writeDB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version))
	require.Equal(t, schemaVersion, version)
	require.NoError(
		t,
		w.h.writeDB.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name='rollback_probe'").Scan(&count),
	)
	require.Zero(t, count)
	runs, err := w.ListRuns(ctx, "", state.PageRequest{})
	require.NoError(t, err)
	require.Equal(t, r.ID, runs.Items[0].ID)
	// Introspection verifies that the embedded migration actually ran.
	require.NoError(
		t,
		w.h.writeDB.QueryRowContext(
			ctx,
			"SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'",
		).Scan(&count),
	)
	require.Equal(t, 12, count)
	require.NoError(t, w.Close())
}

// covers: AC-7, AC-12.
func TestSQLiteBusyWaitHonorsContext(t *testing.T) {
	ctx := t.Context()
	dir := filepath.Join(t.TempDir(), "state")
	w, err := OpenWriter(ctx, dir, time.Now)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	db, err := openDB(ctx, dir, false)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	deadline, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = w.StartRun(deadline, state.RunInput{RequestID: "blocked", Kind: state.Crawl, StartedAt: time.Now()})
	elapsed := time.Since(start)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, elapsed, 500*time.Millisecond, "SQLite contention must honor earlier cancellation")
}

// covers: AC-7, AC-11.
func TestVersionZeroInitialization(t *testing.T) {
	for _, schema := range []string{
		"", "CREATE TABLE foreign_table(id INTEGER)", "CREATE VIEW foreign_view AS SELECT 1",
	} {
		t.Run(
			schema, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "state")
				require.NoError(t, os.Mkdir(dir, 0700))
				f, err := privateFile(filepath.Join(dir, "state.db"), true)
				require.NoError(t, err)
				require.NoError(t, f.Close())
				db, err := openDB(t.Context(), dir, false)
				require.NoError(t, err)
				_, err = db.ExecContext(t.Context(), "PRAGMA journal_mode=WAL")
				require.NoError(t, err)
				if schema != "" {
					_, err = db.ExecContext(t.Context(), schema)
					require.NoError(t, err)
				}
				require.NoError(t, db.Close())
				reader, err := OpenReader(t.Context(), dir)
				require.ErrorIs(t, err, ErrMigrationRequired)
				require.Nil(t, reader)
				w, err := OpenWriter(t.Context(), dir, time.Now)
				if schema == "" {
					require.NoError(t, err)
					require.NoError(t, w.Close())
				} else {
					require.ErrorIs(t, err, ErrStateAccess)
					require.Nil(t, w)
				}
			},
		)
	}
}

// covers: AC-10, AC-11.
func TestSchemaTooNewAndDamagedState(t *testing.T) {
	for _, damaged := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "newer", true: "damaged"}[damaged], func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "state")
				w, err := OpenWriter(t.Context(), dir, time.Now)
				require.NoError(t, err)
				if !damaged {
					_, err = w.h.writeDB.ExecContext(t.Context(), "PRAGMA user_version=99")
					require.NoError(t, err)
				}
				require.NoError(t, w.Close())
				expected := ErrSchemaTooNew
				if damaged {
					expected = ErrStateAccess
					require.NoError(
						t,
						os.WriteFile(filepath.Join(dir, "state.db"), []byte("not a database: synthetic secret"), 0600),
					)
				}
				before, err := os.ReadFile(filepath.Join(dir, "state.db"))
				require.NoError(t, err)
				reader, err := OpenReader(t.Context(), dir)
				require.ErrorIs(t, err, expected)
				require.Nil(t, reader)
				writer, err := OpenWriter(t.Context(), dir, time.Now)
				require.ErrorIs(t, err, expected)
				require.Nil(t, writer)
				after, err := os.ReadFile(filepath.Join(dir, "state.db"))
				require.NoError(t, err)
				require.Equal(t, before, after)
			},
		)
	}
}

// The SQL fault is deliberately below the public boundary to prove that a failure
// midway through one message rolls the whole mutation back.
// covers: AC-6, AC-10.
func TestMessageRollback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	w, err := OpenWriter(t.Context(), dir, time.Now)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "crawl", Kind: state.Crawl, StartedAt: time.Now()})
	require.NoError(t, err)
	input := state.MessageInput{
		RunID: run.ID, SourceID: "source", MessageID: "original", ObservedAt: time.Now(), Keys: []string{"saved"},
	}
	_, err = w.RecordMessage(t.Context(), input)
	require.NoError(t, err)
	_, err = w.h.writeDB.ExecContext(
		t.Context(), `CREATE TRIGGER fail_second BEFORE INSERT ON candidates WHEN NEW.key_text='secret_fail'
 BEGIN SELECT RAISE(ABORT,'secret_fail raw provider output'); END`,
	)
	require.NoError(t, err)
	input.MessageID = "failed"
	input.Keys = []string{"a_partial", "secret_fail"}
	_, err = w.RecordMessage(t.Context(), input)
	require.ErrorIs(t, err, ErrStateAccess)
	require.NotContains(t, err.Error(), "secret")
	var count int
	require.NoError(t, w.h.writeDB.QueryRowContext(t.Context(), "SELECT count(*) FROM candidates").Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, w.h.writeDB.QueryRowContext(t.Context(), "SELECT count(*) FROM crawl_observations").Scan(&count))
	require.Equal(t, 1, count)
}

// covers: spec 0003 AC-9. Migrating schema 1 preserves evidence and provenance,
// requires a writer, and leaves old coverage explicitly unavailable.
func TestCoverageMigrationPreservesLegacyHistory(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "state")
	w, err := OpenWriter(ctx, dir, func() time.Time { return at })
	require.NoError(t, err)
	crawl, err := w.StartRun(ctx, state.RunInput{RequestID: "legacy", Kind: state.Crawl, StartedAt: at})
	require.NoError(t, err)
	url := "https://t.me/warpplus/1"
	discovery, err := w.RecordMessage(
		ctx,
		state.MessageInput{
			RunID: crawl.ID, SourceID: "telegram:warpplus", MessageID: "1", MessageURL: &url, PublishedAt: &at,
			ObservedAt: at, Keys: []string{"synthetic"},
		},
	)
	require.NoError(t, err)
	id := discovery.Discoveries[0].CandidateID
	validation, err := w.StartRun(ctx, state.RunInput{RequestID: "confirmed", Kind: state.Validation, StartedAt: at})
	require.NoError(t, err)
	attempt, err := w.BeginAttempt(
		ctx,
		state.AttemptInput{
			RequestID: "attempt", RunID: validation.ID, CandidateID: id, StartedAt: at, BackendID: "fixture",
			BackendVersion: "1",
		},
	)
	require.NoError(t, err)
	_, err = w.CompleteAttempt(
		ctx,
		state.AttemptCompletion{
			AttemptID: attempt.ID, FinishedAt: at, Outcome: state.Confirmed,
			Evidence: &state.Evidence{Kind: state.AccountType, ObservedAt: at, AccountType: "Unlimited"},
		},
	)
	require.NoError(t, err)
	_, err = w.FinishRun(ctx, state.RunCompletion{RunID: validation.ID, Status: state.Completed, FinishedAt: at})
	require.NoError(t, err)
	evaluation := state.Evaluation{AsOf: at, MaxAge: time.Hour}
	before, err := w.GetCandidate(ctx, id, evaluation)
	require.NoError(t, err)
	sightings, err := w.ListSightings(ctx, id, state.PageRequest{})
	require.NoError(t, err)
	// Remove only the schema 2 addition to reconstruct a populated schema 1 file.
	_, err = w.h.writeDB.ExecContext(
		ctx,
		"DROP TABLE validation_attempt_details; DROP TABLE validation_items; DROP TABLE validation_batches; "+
			"DROP TABLE crawl_page_observations; DROP TABLE crawl_pages; DROP "+
			"TABLE crawl_sources; DROP TABLE crawl_coverage; PRAGMA "+
			"user_version=1",
	)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	r, err := OpenReader(ctx, dir)
	require.ErrorIs(t, err, ErrMigrationRequired)
	require.Nil(t, r)
	w, err = OpenWriter(ctx, dir, func() time.Time { return at.Add(time.Minute) })
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	after, err := w.GetCandidate(ctx, id, evaluation)
	require.NoError(t, err)
	require.Equal(t, before, after)
	afterSightings, err := w.ListSightings(ctx, id, state.PageRequest{})
	require.NoError(t, err)
	require.Equal(t, sightings, afterSightings)
	report, err := w.GetCrawlReport(ctx, crawl.ID)
	require.NoError(t, err)
	require.Nil(t, report.Coverage)
	require.Equal(t, state.Interrupted, report.Run.Status)
	require.Equal(t, int64(1), report.SavedCandidates)
	require.Equal(t, int64(1), report.SavedSightings)
	observations, err := w.ListObservations(ctx, crawl.ID, state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, observations.Items, 1)
	require.Equal(t, at, observations.Items[0].ObservedAt)
	second, err := OpenWriter(ctx, dir, func() time.Time { return at })
	require.ErrorIs(t, err, ErrBusy)
	require.Nil(t, second)
	r, err = OpenReader(ctx, dir)
	require.NoError(t, err)
	require.NoError(t, r.Close())
}
