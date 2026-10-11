package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
)

func planWriter(t *testing.T) (*Writer, time.Time) {
	t.Helper()
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	w, err := OpenWriter(t.Context(), filepath.Join(t.TempDir(), "state"), func() time.Time { return at })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	return w, at
}

func startPlanPage(t *testing.T, w *Writer, at time.Time, limit int) state.CrawlPageID {
	t.Helper()
	r, err := w.StartCrawlPlan(
		t.Context(),
		state.CrawlPlanInput{
			RequestID: "plan", StartedAt: at,
			Sources: []state.SourcePlan{{Channel: "first", PageLimit: limit}, {Channel: "later", PageLimit: 1}},
		},
	)
	require.NoError(t, err)
	id := state.CrawlPageID{RunID: r.Run.ID, SourceID: "telegram:first", PageNumber: 1}
	_, err = w.StartCrawlRequest(t.Context(), state.CrawlRequest{CrawlPageID: id, RequestedAt: at})
	require.NoError(t, err)
	return id
}

// covers: spec 0004 AC-2, AC-4. Rejected plans leave no partial parent or sources.
func TestStartCrawlPlan_InvalidInputsAreAtomic(t *testing.T) {
	w, at := planWriter(t)
	for _, tc := range []struct {
		name    string
		sources []state.SourcePlan
	}{
		{"empty", nil},
		{
			"duplicate normalized channel",
			[]state.SourcePlan{{Channel: "FIRST", PageLimit: 1}, {Channel: "first", PageLimit: 1}},
		},
		{
			"invalid later channel",
			[]state.SourcePlan{{Channel: "first", PageLimit: 1}, {Channel: "../secret", PageLimit: 1}},
		},
		{"zero budget", []state.SourcePlan{{Channel: "first", PageLimit: 0}}},
		{"excessive budget", []state.SourcePlan{{Channel: "first", PageLimit: 101}}},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				_, err := w.StartCrawlPlan(
					t.Context(),
					state.CrawlPlanInput{RequestID: tc.name, StartedAt: at, Sources: tc.sources},
				)
				require.ErrorIs(t, err, ErrInvalidInput)
				runs, err := w.ListRuns(t.Context(), state.Crawl, state.PageRequest{})
				require.NoError(t, err)
				require.Empty(t, runs.Items)
			},
		)
	}
}

// covers: spec 0004 AC-4, AC-5, AC-9. No later request or completion can bypass parsing.
func TestCrawlPlan_RejectsInvalidLifecycleWithoutChangingReport(t *testing.T) {
	w, at := planWriter(t)
	id := startPlanPage(t, w, at, 2)
	before, err := w.GetCrawlReport(t.Context(), id.RunID)
	require.NoError(t, err)
	next := id
	next.PageNumber = 2
	for _, tc := range []struct {
		name string
		run  func() error
		want error
	}{
		{
			"request before predecessor completes", func() error {
				_, e := w.StartCrawlRequest(t.Context(), state.CrawlRequest{CrawlPageID: next, RequestedAt: at})
				return e
			}, ErrConflict,
		},
		{
			"completion before parse", func() error {
				_, e := w.FinishCrawlPage(
					t.Context(),
					state.CrawlPageCompletion{CrawlPageID: id, Status: "completed", FinishedAt: at},
				)
				return e
			}, ErrConflict,
		},
		{
			"source before page completes", func() error {
				_, e := w.FinishCrawlSource(
					t.Context(),
					state.CrawlSourceCompletion{
						RunID: id.RunID, SourceID: id.SourceID, Status: "completed", StopReason: "preview_end",
						FinishedAt: at,
					},
				)
				return e
			}, ErrConflict,
		},
		{
			"parent with unfinished sources", func() error {
				_, e := w.FinishCrawlPlan(
					t.Context(),
					state.RunCompletion{RunID: id.RunID, Status: state.Completed, FinishedAt: at},
				)
				return e
			}, ErrConflict,
		},
		{
			"legacy finish", func() error {
				_, e := w.FinishRun(
					t.Context(),
					state.RunCompletion{RunID: id.RunID, Status: state.Completed, FinishedAt: at},
				)
				return e
			}, ErrInvalidInput,
		},
		{
			"discovery before parse", func() error {
				_, e := w.RecordCrawlMessage(
					t.Context(),
					id,
					state.MessageInput{
						RunID: id.RunID, SourceID: id.SourceID, MessageID: "1", ObservedAt: at,
						Keys: []string{"secret"},
					},
				)
				return e
			}, ErrRunClosed,
		},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				require.ErrorIs(t, tc.run(), tc.want)
				after, err := w.GetCrawlReport(t.Context(), id.RunID)
				require.NoError(t, err)
				require.Equal(t, before, after)
			},
		)
	}
	_, err = w.RecordCrawlParse(t.Context(), state.CrawlParse{CrawlPageID: id, FetchedAt: at})
	require.NoError(t, err)
	_, err = w.FinishCrawlPage(
		t.Context(),
		state.CrawlPageCompletion{CrawlPageID: id, Status: "completed", FinishedAt: at},
	)
	require.NoError(t, err)
	_, err = w.StartCrawlRequest(t.Context(), state.CrawlRequest{CrawlPageID: next, RequestedAt: at})
	require.ErrorIs(t, err, ErrConflict, "no next cursor means no additional request")
}

// covers: spec 0004 AC-4, AC-5, AC-9. Invalid facts never become saved coverage.
func TestRecordCrawlParse_RejectsInvalidFactsAtomically(t *testing.T) {
	w, at := planWriter(t)
	id := startPlanPage(t, w, at, 2)
	one, two, zero := int64(1), int64(2), int64(0)
	for _, tc := range []struct {
		name           string
		count          int
		min, max, next *int64
	}{
		{"negative count", -1, nil, nil, nil},
		{"missing bounds", 1, nil, nil, nil},
		{"zero lower bound", 1, &zero, &one, nil},
		{"reversed bounds", 1, &two, &one, nil},
		{"empty with bounds", 0, &one, &one, nil},
		{"empty with cursor", 0, nil, nil, &one},
		{"zero cursor", 1, &one, &one, &zero},
		{"cursor past lower bound", 1, &one, &two, &two},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				_, err := w.RecordCrawlParse(
					t.Context(),
					state.CrawlParse{
						CrawlPageID: id, FetchedAt: at, ParsedPostCount: tc.count, MinMessageID: tc.min,
						MaxMessageID: tc.max, NextBeforeID: tc.next,
					},
				)
				require.ErrorIs(t, err, ErrInvalidInput)
				report, err := w.GetCrawlReport(t.Context(), id.RunID)
				require.NoError(t, err)
				require.Equal(t, "requested", report.Sources[0].Pages[0].Status)
				require.Nil(t, report.Sources[0].Pages[0].FetchedAt)
			},
		)
	}
}

// covers: spec 0004 AC-4, AC-8. Terminal replays are safe but conflicting inputs fail.
func TestCrawlPlan_TerminalReplayPreservesSavedInputs(t *testing.T) {
	w, at := planWriter(t)
	id := startPlanPage(t, w, at, 1)
	facts := state.CrawlParse{CrawlPageID: id, FetchedAt: at}
	_, err := w.RecordCrawlParse(t.Context(), facts)
	require.NoError(t, err)
	pageEnd := state.CrawlPageCompletion{CrawlPageID: id, Status: "completed", FinishedAt: at}
	_, err = w.FinishCrawlPage(t.Context(), pageEnd)
	require.NoError(t, err)
	sourceEnd := state.CrawlSourceCompletion{
		RunID: id.RunID, SourceID: id.SourceID, Status: "completed", StopReason: "preview_end", FinishedAt: at,
	}
	_, err = w.FinishCrawlSource(t.Context(), sourceEnd)
	require.NoError(t, err)
	end := state.RunCompletion{RunID: id.RunID, Status: state.Cancelled, ReasonCode: "cancelled", FinishedAt: at}
	want, err := w.FinishCrawlPlan(t.Context(), end)
	require.NoError(t, err)
	_, err = w.StartCrawlRequest(t.Context(), state.CrawlRequest{CrawlPageID: id, RequestedAt: at})
	require.NoError(t, err)
	_, err = w.RecordCrawlParse(t.Context(), facts)
	require.NoError(t, err)
	_, err = w.FinishCrawlPage(t.Context(), pageEnd)
	require.NoError(t, err)
	_, err = w.FinishCrawlSource(t.Context(), sourceEnd)
	require.NoError(t, err)
	got, err := w.FinishCrawlPlan(t.Context(), end)
	require.NoError(t, err)
	require.Equal(t, want, got)
	_, err = w.StartCrawlRequest(t.Context(), state.CrawlRequest{CrawlPageID: id, RequestedAt: at.Add(time.Second)})
	require.ErrorIs(t, err, ErrConflict)
	facts.FetchedAt = at.Add(time.Second)
	_, err = w.RecordCrawlParse(t.Context(), facts)
	require.ErrorIs(t, err, ErrConflict)
	pageEnd.FinishedAt = at.Add(time.Second)
	_, err = w.FinishCrawlPage(t.Context(), pageEnd)
	require.ErrorIs(t, err, ErrConflict)
	sourceEnd.StopReason = "page_limit"
	_, err = w.FinishCrawlSource(t.Context(), sourceEnd)
	require.ErrorIs(t, err, ErrConflict)
	end.FinishedAt = at.Add(time.Second)
	_, err = w.FinishCrawlPlan(t.Context(), end)
	require.ErrorIs(t, err, ErrConflict)
	got, err = w.GetCrawlReport(t.Context(), id.RunID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// covers: spec 0004 AC-4, AC-6, AC-8. A failed page link rolls back discovery too.
func TestRecordCrawlMessage_PageLinkFailureRollsBackWholeMessage(t *testing.T) {
	w, at := planWriter(t)
	id := startPlanPage(t, w, at, 1)
	one := int64(1)
	_, err := w.RecordCrawlParse(
		t.Context(),
		state.CrawlParse{CrawlPageID: id, FetchedAt: at, ParsedPostCount: 1, MinMessageID: &one, MaxMessageID: &one},
	)
	require.NoError(t, err)
	_, err = w.h.writeDB.ExecContext(
		t.Context(),
		`CREATE TRIGGER reject_page_link BEFORE INSERT ON `+
			`crawl_page_observations BEGIN SELECT RAISE(ABORT,'private upstream `+
			`secret'); END`,
	)
	require.NoError(t, err)
	_, err = w.RecordCrawlMessage(
		t.Context(),
		id,
		state.MessageInput{
			RunID: id.RunID, SourceID: id.SourceID, MessageID: "1", ObservedAt: at, Keys: []string{"synthetic"},
		},
	)
	require.ErrorIs(t, err, ErrStateAccess)
	require.NotContains(t, err.Error(), "private upstream secret")
	for _, table := range []string{"candidates", "sightings", "crawl_observations", "crawl_page_observations"} {
		var count int
		require.NoError(t, w.h.writeDB.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count, table)
	}
	r, err := w.GetCrawlReport(t.Context(), id.RunID)
	require.NoError(t, err)
	require.Equal(t, "parsed", r.Sources[0].Pages[0].Status)
}

// covers: spec 0004 AC-10. A failed version 3 migration preserves version 2.
func TestPlanMigration_RollsBackAllNewTables(t *testing.T) {
	w, at := planWriter(t)
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "legacy", Kind: state.Crawl, StartedAt: at})
	require.NoError(t, err)
	_, err = w.h.writeDB.ExecContext(
		t.Context(),
		"DROP TABLE candidate_schedule; DROP TABLE scheduler_state; ALTER TABLE runs DROP COLUMN origin; "+
			"ALTER TABLE runs DROP COLUMN schedule_completion_json; "+
			"ALTER TABLE validation_attempts DROP COLUMN schedule_completion_json; "+
			"DROP TABLE validation_attempt_details; DROP TABLE validation_items; DROP TABLE validation_batches; "+
			"DROP TABLE crawl_page_observations; DROP TABLE crawl_pages; DROP TABLE crawl_sources; PRAGMA user_version=2",
	)
	require.NoError(t, err)
	err = migrate(
		t.Context(),
		w.h.writeDB,
		[]string{initialSchema, coverageSchema, planSchema + "; INSERT INTO missing_table VALUES (1)"},
	)
	require.Error(t, err)
	var version, count int
	require.NoError(t, w.h.writeDB.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version))
	require.Equal(t, 2, version)
	require.NoError(
		t,
		w.h.writeDB.QueryRowContext(
			t.Context(),
			"SELECT count(*) FROM sqlite_schema WHERE name IN ('crawl_sources',"+
				"'crawl_pages','crawl_page_observations','crawl_page_links')",
		).Scan(&count),
	)
	require.Zero(t, count)
	require.NoError(
		t,
		w.h.writeDB.QueryRowContext(t.Context(), "SELECT count(*) FROM runs WHERE id=?", run.ID).Scan(&count),
	)
	require.Equal(t, 1, count)
	require.NoError(t, migrate(t.Context(), w.h.writeDB, []string{initialSchema, coverageSchema, planSchema}))
	err = migrate(t.Context(), w.h.writeDB, []string{initialSchema, coverageSchema})
	require.ErrorIs(t, err, ErrSchemaTooNew)
}

// covers: spec 0004 AC-4, AC-10. Report reads retain every configured source.
func TestCrawlPlan_MaximumSourcesAndImmutableReplay(t *testing.T) {
	w, at := planWriter(t)
	input := state.CrawlPlanInput{RequestID: "maximum", StartedAt: at}
	for i := 0; i < 32; i++ {
		input.Sources = append(input.Sources, state.SourcePlan{Channel: fmt.Sprintf("source%d", i), PageLimit: 100})
	}
	want, err := w.StartCrawlPlan(t.Context(), input)
	require.NoError(t, err)
	require.Len(t, want.Sources, 32)
	input.Sources[0].PageLimit = 99
	_, err = w.StartCrawlPlan(t.Context(), input)
	require.ErrorIs(t, err, ErrConflict)
	got, err := w.GetCrawlReport(t.Context(), want.Run.ID)
	require.NoError(t, err)
	require.Equal(t, want, got)
	input.RequestID = "too-many"
	input.Sources = append(input.Sources, state.SourcePlan{Channel: "extra", PageLimit: 1})
	_, err = w.StartCrawlPlan(t.Context(), input)
	require.ErrorIs(t, err, ErrInvalidInput)
}

// covers: spec 0004 AC-6, AC-10. Rediscovery changes visibility, never entitlement.
func TestCrawlPlan_RediscoveryPreservesEvidenceAndProvenance(t *testing.T) {
	w, at := planWriter(t)
	var candidateID int64
	var evidence *state.Attempt
	for iteration := 0; iteration < 2; iteration++ {
		observed := at.Add(time.Duration(iteration) * time.Hour)
		plan, err := w.StartCrawlPlan(
			t.Context(),
			state.CrawlPlanInput{
				RequestID: fmt.Sprint("crawl", iteration), StartedAt: observed,
				Sources: []state.SourcePlan{{Channel: "first", PageLimit: 1}, {Channel: "second", PageLimit: 1}},
			},
		)
		require.NoError(t, err)
		for _, channel := range []string{"first", "second"} {
			id := state.CrawlPageID{RunID: plan.Run.ID, SourceID: "telegram:" + channel, PageNumber: 1}
			_, err = w.StartCrawlRequest(t.Context(), state.CrawlRequest{CrawlPageID: id, RequestedAt: observed})
			require.NoError(t, err)
			n := int64(7)
			_, err = w.RecordCrawlParse(
				t.Context(),
				state.CrawlParse{
					CrawlPageID: id, FetchedAt: observed, ParsedPostCount: 1, MinMessageID: &n, MaxMessageID: &n,
				},
			)
			require.NoError(t, err)
			url := "https://t.me/" + channel + "/7"
			if iteration == 1 {
				url += "/edited"
			}
			message := state.MessageInput{
				RunID: id.RunID, SourceID: id.SourceID, MessageID: "7", ObservedAt: observed, PublishedAt: &observed,
				MessageURL: &url, Keys: []string{"Key", "key", "Key"},
			}
			result, err := w.RecordCrawlMessage(t.Context(), id, message)
			require.NoError(t, err)
			require.Len(t, result.Discoveries, 2)
			candidateID = result.Discoveries[0].CandidateID
			if iteration == 1 {
				require.Zero(t, result.NewCandidates)
				require.Zero(t, result.NewSightings)
			}
			require.Equal(t, 2, result.NewObservations)
			replay, err := w.RecordCrawlMessage(t.Context(), id, message)
			require.NoError(t, err)
			require.Zero(t, replay.NewObservations)
			_, err = w.FinishCrawlPage(
				t.Context(),
				state.CrawlPageCompletion{CrawlPageID: id, Status: "completed", FinishedAt: observed},
			)
			require.NoError(t, err)
			_, err = w.FinishCrawlSource(
				t.Context(),
				state.CrawlSourceCompletion{
					RunID: id.RunID, SourceID: id.SourceID, Status: "completed", StopReason: "preview_end",
					FinishedAt: observed,
				},
			)
			require.NoError(t, err)
		}
		report, err := w.FinishCrawlPlan(
			t.Context(),
			state.RunCompletion{RunID: plan.Run.ID, Status: state.Completed, FinishedAt: observed},
		)
		require.NoError(t, err)
		require.Equal(t, int64(2), report.SavedCandidates)
		require.Equal(t, int64(4), report.SavedSightings)
		if iteration == 0 {
			run, err := w.StartRun(
				t.Context(),
				state.RunInput{RequestID: "validation", Kind: state.Validation, StartedAt: at},
			)
			require.NoError(t, err)
			attempt, err := w.BeginAttempt(
				t.Context(),
				state.AttemptInput{
					RequestID: "attempt", RunID: run.ID, CandidateID: candidateID, StartedAt: at, BackendID: "fixture",
					BackendVersion: "1",
				},
			)
			require.NoError(t, err)
			completed, err := w.CompleteAttempt(
				t.Context(),
				state.AttemptCompletion{
					AttemptID: attempt.ID, FinishedAt: at, Outcome: state.Confirmed,
					Evidence: &state.Evidence{Kind: state.AccountType, ObservedAt: at, AccountType: "Unlimited"},
				},
			)
			require.NoError(t, err)
			evidence = &completed
			_, err = w.FinishRun(
				t.Context(),
				state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: at},
			)
			require.NoError(t, err)
		}
	}
	candidate, err := w.GetCandidate(
		t.Context(),
		candidateID,
		state.Evaluation{AsOf: at.Add(2 * time.Hour), MaxAge: time.Hour},
	)
	require.NoError(t, err)
	require.Equal(t, state.Confirmed, candidate.LatestOutcome)
	require.Equal(t, state.Stale, candidate.Freshness)
	require.Equal(t, evidence, candidate.LastDefinitiveAttempt)
	require.Equal(t, at, candidate.FirstSeenAt)
	require.Equal(t, at.Add(time.Hour), candidate.LastSeenAt)
	sightings, err := w.ListSightings(t.Context(), candidateID, state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, sightings.Items, 2)
	for _, sighting := range sightings.Items {
		require.Equal(t, at, *sighting.PublishedAt)
		require.NotContains(t, *sighting.MessageURL, "edited")
	}
}

// Spec 0004 AC-4, AC-6, AC-8, AC-10: page attribution, replay and recovery.
func TestCrawlPlanOverlapReplayAndRecovery(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "state")
	w, err := OpenWriter(ctx, dir, func() time.Time { return at })
	require.NoError(t, err)
	input := state.CrawlPlanInput{
		RequestID: "plan", StartedAt: at,
		Sources: []state.SourcePlan{{Channel: "WarpPlus", PageLimit: 2}, {Channel: "later", PageLimit: 1}},
	}
	report, err := w.StartCrawlPlan(ctx, input)
	require.NoError(t, err)
	require.Len(t, report.Sources, 2)
	require.Nil(t, report.Coverage)
	replay, err := w.StartCrawlPlan(ctx, input)
	require.NoError(t, err)
	require.Equal(t, report, replay)
	run := report.Run.ID
	source := "telegram:warpplus"
	for n := 1; n <= 2; n++ {
		id := state.CrawlPageID{RunID: run, SourceID: source, PageNumber: n}
		request := state.CrawlRequest{CrawlPageID: id, RequestedAt: at.Add(time.Duration(n) * time.Second)}
		page, err := w.StartCrawlRequest(ctx, request)
		require.NoError(t, err)
		if n == 2 {
			require.Equal(t, int64(10), *page.BeforeID)
		}
		minID, maxID, next := int64(10), int64(10), int64(10)
		if n == 2 {
			minID = 9
			next = 9
		}
		facts := state.CrawlParse{
			CrawlPageID: id, FetchedAt: at, ParsedPostCount: n, MinMessageID: &minID, MaxMessageID: &maxID,
			NextBeforeID: &next,
		}
		_, err = w.RecordCrawlParse(ctx, facts)
		require.NoError(t, err)
		keys := []string{"a", "shared"}
		if n == 2 {
			keys = append(keys, "edited")
		}
		message := state.MessageInput{RunID: run, SourceID: source, MessageID: "10", ObservedAt: at, Keys: keys}
		_, err = w.RecordMessage(ctx, message)
		require.ErrorIs(t, err, ErrInvalidInput)
		_, err = w.RecordCrawlMessage(ctx, id, message)
		require.NoError(t, err)
		if n == 2 {
			message.MessageID = "9"
			message.Keys = []string{"a"}
			_, err = w.RecordCrawlMessage(ctx, id, message)
			require.NoError(t, err)
		}
		completion := state.CrawlPageCompletion{CrawlPageID: id, Status: "completed", FinishedAt: at}
		_, err = w.FinishCrawlPage(ctx, completion)
		require.NoError(t, err)
		_, err = w.FinishCrawlPage(ctx, completion)
		require.NoError(t, err)
		_, err = w.StartCrawlRequest(ctx, request)
		require.NoError(t, err)
		_, err = w.RecordCrawlParse(ctx, facts)
		require.NoError(t, err)
		_, err = w.RecordCrawlMessage(ctx, id, message)
		require.ErrorIs(t, err, ErrRunClosed)
	}
	_, err = w.FinishCrawlSource(
		ctx,
		state.CrawlSourceCompletion{
			RunID: run, SourceID: source, Status: "completed", FinishedAt: at, StopReason: "preview_end",
		},
	)
	require.ErrorIs(t, err, ErrConflict)
	_, err = w.FinishCrawlSource(
		ctx,
		state.CrawlSourceCompletion{
			RunID: run, SourceID: source, Status: "completed", FinishedAt: at, StopReason: "page_limit",
		},
	)
	require.NoError(t, err)
	_, err = w.FinishCrawlPlan(ctx, state.RunCompletion{RunID: run, Status: state.Completed, FinishedAt: at})
	require.ErrorIs(t, err, ErrConflict)
	report, err = w.GetCrawlReport(ctx, run)
	require.NoError(t, err)
	require.Equal(t, int64(3), report.SavedCandidates)
	require.Equal(t, int64(4), report.SavedSightings)
	s := report.Sources[0]
	require.Equal(t, int64(3), s.SavedCandidates)
	require.Equal(t, int64(2), s.Pages[0].SavedSightings)
	require.Equal(t, int64(2), s.Pages[1].SavedSightings)
	require.Equal(t, at.Add(time.Second), *s.StartedAt)
	require.NoError(t, w.Close())
	recovery := at.Add(time.Hour)
	w, err = OpenWriter(ctx, dir, func() time.Time { return recovery })
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	report, err = w.GetCrawlReport(ctx, run)
	require.NoError(t, err)
	require.Equal(t, state.Interrupted, report.Run.Status)
	require.Equal(t, "completed", report.Sources[0].Status)
	require.Equal(t, "skipped", report.Sources[1].Status)
	require.Equal(t, "interrupted_before_start", *report.Sources[1].StopReason)
	require.Nil(t, report.Sources[1].FinishedAt)
	require.Equal(t, recovery, *report.Sources[1].RecoveredAt)
	require.Equal(t, int64(3), report.SavedCandidates)
}

// Spec 0004 AC-10: apply version 3 to real version 2 coverage without backfill.
func TestPlanMigrationPreservesCoveredReport(t *testing.T) {
	ctx := t.Context()
	at := time.Now()
	dir := filepath.Join(t.TempDir(), "state")
	w, err := OpenWriter(ctx, dir, func() time.Time { return at })
	require.NoError(t, err)
	report, err := w.StartCrawl(
		ctx,
		state.CrawlInput{
			RequestID: "legacy-covered", StartedAt: at, SourceID: "telegram:warpplus",
			RequestedURL: "https://t.me/s/warpplus", CoverageMode: "latest_preview", PageLimit: 1,
		},
	)
	require.NoError(t, err)
	_, err = w.RecordCrawlPage(ctx, state.CrawlPage{RunID: report.Run.ID, FetchedAt: at})
	require.NoError(t, err)
	_, err = w.FinishRun(ctx, state.RunCompletion{RunID: report.Run.ID, Status: state.Completed, FinishedAt: at})
	require.NoError(t, err)
	before, err := w.GetCrawlReport(ctx, report.Run.ID)
	require.NoError(t, err)
	_, err = w.h.writeDB.ExecContext(
		ctx,
		"DROP TABLE candidate_schedule; DROP TABLE scheduler_state; ALTER TABLE runs DROP COLUMN origin; "+
			"ALTER TABLE runs DROP COLUMN schedule_completion_json; "+
			"ALTER TABLE validation_attempts DROP COLUMN schedule_completion_json; "+
			"DROP TABLE validation_attempt_details; DROP TABLE validation_items; DROP TABLE validation_batches; "+
			"DROP TABLE crawl_page_observations; DROP TABLE crawl_pages; DROP TABLE crawl_sources; PRAGMA user_version=2",
	)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	w, err = OpenWriter(ctx, dir, func() time.Time { return at })
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	after, err := w.GetCrawlReport(ctx, report.Run.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	var version, tables int
	require.NoError(t, w.h.writeDB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version))
	require.Equal(t, schemaVersion, version)
	require.NoError(
		t,
		w.h.writeDB.QueryRowContext(
			ctx,
			"SELECT count(*) FROM sqlite_schema WHERE type='table' AND name IN "+
				"('crawl_sources','crawl_pages','crawl_page_observations')",
		).Scan(&tables),
	)
	require.Equal(t, 3, tables)
}
