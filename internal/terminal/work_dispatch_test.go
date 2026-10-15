package terminal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

type workSource func(context.Context, string, *int64) (telegram.Page, error)

type countedWorkBackend struct {
	validator.Unsupported
	acquired atomic.Int32
}

func (b *countedWorkBackend) Acquire(context.Context) (validator.BatchOwner, error) {
	b.acquired.Add(1)
	return nil, &validator.Error{Code: "validation_unsupported"}
}

// Spec 0012 AC-3 and AC-15: a changed candidate rejects the entire batch before
// validator ownership, even when earlier local history was recovered at admission.
func TestTerminalWork_ChangedCandidateRejectsBatchBeforeValidator(t *testing.T) {
	a, b := workFixture(t)
	backend := &countedWorkBackend{}
	a.Validator = backend
	seedWorkCandidates(t, a, 2)
	dispatchResultsJob(t, b, "results.open", record{})
	review := dispatchResultsJob(t, b, "work.review_check", record{
		"view_id": "1", "selection": "matching", "candidate_id": nil,
	})
	// The store has no public identity edit. This fixture simulates an outside
	// database edit after review without replacing the collection's file identity.
	db, err := sql.Open("sqlite", filepath.Join(a.Settings.StateDir, "state.db"))
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "UPDATE candidates SET key_text=? WHERE id=2", "private-replacement")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	start, err := dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
	require.NoError(t, err)
	outcome := awaitWork(t, b, start.(record)["operation_id"])
	require.Equal(t, "ui_candidate_unavailable", outcome["error_code"])
	require.Equal(t, "false", outcome["run_created"])
	require.Nil(t, outcome["run_id"])
	require.Equal(t, "known", outcome["persistence"])
	require.Zero(t, backend.acquired.Load())
	r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
	require.NoError(t, err)
	recovered, err := r.GetCrawlReport(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, state.Interrupted, recovered.Run.Status)
	_, err = r.GetValidationReport(t.Context(), 2)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.NoError(t, r.Close())
	raw, err := json.Marshal(outcome)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private-replacement")
}

// Spec 0012 AC-14: controls accept exact typed identities and reject ambiguous
// or private fields before they can reach a session or external boundary.
func TestTerminalWork_ClosedRequestParameters(t *testing.T) {
	for _, tt := range []struct {
		name, method, params string
		valid                bool
	}{
		{"exact ID", "work.poll", `{"operation_id":"9007199254740993"}`, true},
		{"maximum ID", "work.start", `{"review_id":"9223372036854775807"}`, true},
		{"numeric ID", "work.poll", `{"operation_id":1}`, false},
		{"overflow ID", "work.poll", `{"operation_id":"9223372036854775808"}`, false},
		{"zero ID", "work.cancel", `{"operation_id":"0"}`, false},
		{"duplicate ID", "work.start", `{"review_id":"1","review_id":"2"}`, false},
		{"alias ID", "work.start", `{"Review_id":"1"}`, false},
		{"private field", "work.start", `{"review_id":"1","key":"private"}`, false},
		{"focused without candidate", "work.review_check",
			`{"view_id":"1","selection":"focused","candidate_id":null}`, false},
		{"matching with candidate", "work.review_check",
			`{"view_id":"1","selection":"matching","candidate_id":"1"}`, false},
		{"null channel", "work.review_crawl", `{"channels":[null],"page_limit":null}`, false},
		{"report null ID", "report.open", `{"run_id":null}`, false},
		{"report private field", "report.open", `{"run_id":"1","request_token":"private"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := requestParams(request{Method: tt.method, Params: json.RawMessage(tt.params)})
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func (f workSource) FetchPage(ctx context.Context, channel string, before *int64) (telegram.Page, error) {
	return f(ctx, channel, before)
}

// Spec 0012 AC-1, AC-2 and AC-4: review preserves configured order and policy,
// applies an override only to this run, and does not initialize absent state.
func TestTerminalWork_CrawlReviewPreservesConfiguration(t *testing.T) {
	a, b := workFixture(t)
	a.Settings.Sources = []config.Source{
		{Channel: "first", Enabled: true, PageLimit: 2},
		{Channel: "disabled", Enabled: false, PageLimit: 9},
		{Channel: "last", Enabled: true, PageLimit: 3},
	}
	b.options.Settings = a.Settings
	before, err := dispatchResults(t, b, "work.options", record{})
	require.NoError(t, err)
	require.Equal(t, []any{
		record{"channel": "first", "enabled": true, "page_limit": "2"},
		record{"channel": "disabled", "enabled": false, "page_limit": "9"},
		record{"channel": "last", "enabled": true, "page_limit": "3"},
	}, before.(record)["sources"])
	for _, limit := range []any{nil, "1", "100"} {
		review := dispatchResultsJob(t, b, "work.review_crawl", record{
			"channels": []string{"last", "first"}, "page_limit": limit,
		})
		first, last, budget := any("2"), any("3"), "5"
		if limit != nil {
			first, last = limit, limit
			n, err := strconv.Atoi(limit.(string))
			require.NoError(t, err)
			budget = strconv.Itoa(2 * n)
		}
		require.Equal(t, []any{
			record{"channel": "first", "page_limit": first},
			record{"channel": "last", "page_limit": last},
		}, review["sources"])
		require.Equal(t, budget, review["page_budget"])
		require.Equal(t, "absent", review["collection_state"])
		_, err = dispatchResults(t, b, "work.review_release", record{"review_id": review["review_id"]})
		require.NoError(t, err)
		_, err = dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
		require.Equal(t, "ui_handle_stale", codeFor(err))
	}
	after, err := dispatchResults(t, b, "work.options", record{})
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = os.Stat(a.Settings.StateDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// Spec 0012 AC-2 and AC-14: invalid selections cannot prepare an executable review.
func TestTerminalWork_RejectsInvalidCrawlReviews(t *testing.T) {
	for _, tt := range []struct {
		name     string
		channels []string
		limit    any
	}{
		{"empty", []string{}, nil},
		{"disabled", []string{"disabled"}, nil},
		{"unknown", []string{"unknown"}, nil},
		{"duplicate", []string{"fixture", "fixture"}, nil},
		{"normalized duplicate", []string{"fixture", "@fixture"}, nil},
		{"zero limit", []string{"fixture"}, "0"},
		{"excessive limit", []string{"fixture"}, "101"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, b := workFixture(t)
			a.Settings.Sources = append(a.Settings.Sources,
				config.Source{Channel: "disabled", Enabled: false, PageLimit: 1})
			_, err := dispatchResults(t, b, "work.review_crawl", record{
				"channels": tt.channels, "page_limit": tt.limit,
			})
			if err == nil {
				b.work.Wait()
				value, pollErr := b.poll(b.nextJob, false)
				require.NoError(t, pollErr)
				require.Equal(t, "failed", value.(record)["state"])
				require.Equal(t, "state_invalid_input", value.(record)["error_code"])
			} else {
				require.Contains(t, []string{"ui_protocol_invalid", "state_invalid_input"}, codeFor(err))
			}
			require.False(t, b.options.Work.Busy())
			_, err = dispatchResults(t, b, "work.start", record{"review_id": "1"})
			require.Equal(t, "ui_handle_stale", codeFor(err))
			_, err = os.Stat(a.Settings.StateDir)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func seedWorkCandidates(t *testing.T, a *app.App, count int) {
	t.Helper()
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "seed", Kind: state.Crawl, StartedAt: a.Clock()})
	require.NoError(t, err)
	keys := make([]string, count)
	for i := range keys {
		keys[i] = fmt.Sprintf("private-marked-%d", i)
	}
	_, err = w.RecordMessage(t.Context(), state.MessageInput{
		RunID: run.ID, SourceID: "fixture", MessageID: "1", ObservedAt: a.Clock(), Keys: keys,
	})
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

// Spec 0012 AC-10: service advice requires positive matching ownership facts.
// Read failures, stale observations and foreground ownership keep generic advice.
func TestTerminalWork_BusyGuidanceRequiresVerifiedService(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*state.OverviewService)
	}{
		{"verified", func(*state.OverviewService) {}},
		{"unknown identity", func(s *state.OverviewService) { s.IdentityState = "unknown" }},
		{"different collection", func(s *state.OverviewService) { s.CollectionMatch = "different" }},
		{"manager unavailable", func(s *state.OverviewService) { s.Manager.Availability = "unavailable" }},
		{"manager stopped", func(s *state.OverviewService) { s.Manager.Data.Running = new(false) }},
		{"stale", func(s *state.OverviewService) { s.Runner.Data.Stale = new(true) }},
		{"staleness unknown", func(s *state.OverviewService) { s.Runner.Data.Stale = nil }},
		{"different installation", func(s *state.OverviewService) { s.Runner.Data.InstallationMatches = new(false) }},
		{"unknown owner", func(s *state.OverviewService) { s.Runner.Data.OwnerPID = nil }},
		{"clock anomaly", func(s *state.OverviewService) { s.Runner.ClockAnomaly = true }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, b := workFixture(t)
			w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, w.Close()) })
			observed := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
			service := state.OverviewService{
				IdentityState: "verified", InstallationID: new("fixture"), CollectionMatch: "match",
				Manager: state.OverviewComponent[state.OverviewManager]{
					Availability: "available", ObservedAt: observed,
					Data: &state.OverviewManager{Running: new(true)},
				},
				Runner: state.OverviewComponent[state.OverviewRunner]{
					Availability: "available", ObservedAt: observed,
					Data: &state.OverviewRunner{
						LockHeld: new(true), InstallationMatches: new(true), Stale: new(false), OwnerPID: new(42),
					},
				},
			}
			tt.change(&service)
			a.OverviewService = func(context.Context) state.OverviewService { return service }
			var sourceCalls atomic.Int32
			a.Source = workSource(func(context.Context, string, *int64) (telegram.Page, error) {
				sourceCalls.Add(1)
				return telegram.Page{}, nil
			})
			review := dispatchResultsJob(t, b, "work.review_crawl", record{
				"channels": []string{"fixture"}, "page_limit": nil,
			})
			start, err := dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
			require.NoError(t, err)
			outcome := awaitWork(t, b, start.(record)["operation_id"])
			require.Equal(t, "state_busy", outcome["error_code"])
			require.Equal(t, "false", outcome["run_created"])
			require.Zero(t, sourceCalls.Load())
			guidance := outcome["guidance"].(record)
			if tt.name == "verified" {
				require.Equal(t, "stop_service", guidance["action"])
				require.Equal(t, stamp(observed), guidance["service_observed_at"])
			} else {
				require.Equal(t, "inspect_owner", guidance["action"])
				require.Nil(t, guidance["service_observed_at"])
			}
		})
	}
}

// Spec 0012 AC-3 and AC-5: selection edits retire reviewed intent before mutation.
func TestTerminalWork_ChangedSelectionInvalidatesReview(t *testing.T) {
	for _, change := range []string{"mark", "clear marks", "filter", "refresh"} {
		t.Run(change, func(t *testing.T) {
			a, b := workFixture(t)
			seedWorkCandidates(t, a, 2)
			dispatchResultsJob(t, b, "results.open", record{})
			_, err := dispatchResults(t, b, "results.mark", record{
				"view_id": "1", "candidate_id": "1", "marked": true,
			})
			require.NoError(t, err)
			review := dispatchResultsJob(t, b, "work.review_check", record{
				"view_id": "1", "selection": "marked", "candidate_id": nil,
			})
			switch change {
			case "mark":
				_, err = dispatchResults(t, b, "results.mark", record{
					"view_id": "1", "candidate_id": "2", "marked": true,
				})
				require.NoError(t, err)
			case "clear marks":
				dispatchResultsJob(t, b, "results.clear_marks", record{"view_id": "1"})
			case "filter":
				dispatchResultsJob(t, b, "results.filter", record{"view_id": "1", "filters": record{
					"status": []string{"unchecked"}, "definitive": []string{}, "freshness": []string{},
				}})
			case "refresh":
				dispatchResultsJob(t, b, "results.refresh", record{"view_id": "1", "focus_id": "1"})
			}
			_, err = dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
			require.Equal(t, "ui_handle_stale", codeFor(err))
			require.False(t, b.options.Work.Busy())
			r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
			require.NoError(t, err)
			_, err = r.GetValidationReport(t.Context(), 2)
			require.ErrorIs(t, err, store.ErrNotFound)
			require.NoError(t, r.Close())
		})
	}
}

// Spec 0012 AC-3, AC-4 and AC-11: sparse marks preserve view order across pages,
// and attempts remain reachable for every saved selected item.
func TestTerminalWork_MarkedSelectionUsesViewOrder(t *testing.T) {
	a, b := workFixture(t)
	seedWorkCandidates(t, a, 102)
	dispatchResultsJob(t, b, "results.open", record{})
	for _, id := range []string{"102", "1", "51"} {
		_, err := dispatchResults(t, b, "results.mark", record{
			"view_id": "1", "candidate_id": id, "marked": true,
		})
		require.NoError(t, err)
	}
	review := dispatchResultsJob(t, b, "work.review_check", record{
		"view_id": "1", "selection": "marked", "candidate_id": nil,
	})
	require.Equal(t, "3", review["selected_count"])
	require.Equal(t, policyRecord(a.Settings.CheckPolicy), review["policy"])
	start, err := dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
	require.NoError(t, err)
	outcome := awaitWork(t, b, start.(record)["operation_id"])
	report := dispatchResultsJob(t, b, "report.open", record{"run_id": outcome["run_id"]})
	page, err := dispatchResults(t, b, "report.page", record{
		"report_id": report["report_id"], "section": "items",
		"source_id": nil, "candidate_id": nil, "offset": "0",
	})
	require.NoError(t, err)
	items := page.(record)["items"].([]any)
	require.Len(t, items, 3)
	for i, id := range []string{"1", "51", "102"} {
		require.Equal(t, id, items[i].(record)["candidate_id"])
		attempts, err := dispatchResults(t, b, "report.page", record{
			"report_id": report["report_id"], "section": "attempts",
			"source_id": nil, "candidate_id": id, "offset": "0",
		})
		require.NoError(t, err)
		require.Equal(t, "1", attempts.(record)["total"])
		raw, err := json.Marshal(attempts)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "private-marked")
	}
}

// Spec 0012 AC-5, AC-7 through AC-9: cancellation remains immediate during
// preparation, but the operation retains ownership until the source returns.
func TestTerminalWork_CancelRetainsOwnershipThroughSourceReturn(t *testing.T) {
	a, b := workFixture(t)
	entered, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var calls atomic.Int32
	a.Source = workSource(func(ctx context.Context, _ string, _ *int64) (telegram.Page, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		close(stopped)
		<-release
		return telegram.Page{}, ctx.Err()
	})
	review := dispatchResultsJob(t, b, "work.review_crawl", record{
		"channels": []string{"fixture"}, "page_limit": nil,
	})
	start, err := dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
	require.NoError(t, err)
	id := start.(record)["operation_id"]
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("source did not start")
	}
	preparing := make(chan struct{})
	job := b.start(t.Context(), "report_open", "report", nil,
		func(ctx context.Context, _ app.ResultsProgress, _ app.ResultsCommit) (any, error) {
			close(preparing)
			<-ctx.Done()
			return nil, ctx.Err()
		}).(record)
	<-preparing
	first, err := dispatchResults(t, b, "work.cancel", record{"operation_id": id})
	require.NoError(t, err)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("work cancellation waited for report preparation")
	}
	deadline := b.options.Work.Guard.Deadline()
	second, err := dispatchResults(t, b, "work.cancel", record{"operation_id": id})
	require.NoError(t, err)
	require.Equal(t, first.(record)["revision"], second.(record)["revision"])
	require.Equal(t, deadline, b.options.Work.Guard.Deadline())
	require.Equal(t, "cancelling", second.(record)["state"])
	_, err = dispatchResults(t, b, "work.acknowledge", record{"operation_id": id})
	require.Equal(t, "ui_busy", codeFor(err))
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	if w != nil {
		require.NoError(t, w.Close())
	}
	require.ErrorIs(t, err, store.ErrBusy)
	pending, err := dispatchResults(t, b, "job.poll", record{"job_id": job["job_id"]})
	require.NoError(t, err)
	require.Equal(t, "running", pending.(record)["state"])
	_, err = dispatchResults(t, b, "job.cancel", record{"job_id": job["job_id"]})
	require.NoError(t, err)
	b.work.Wait()
	unblock()
	outcome := awaitWork(t, b, id)
	require.Equal(t, "cancelled", outcome["state"])
	require.Equal(t, "true", outcome["run_created"])
	require.EqualValues(t, 1, calls.Load())
	_, err = dispatchResults(t, b, "work.review_crawl", record{
		"channels": []string{"fixture"}, "page_limit": nil,
	})
	require.Equal(t, "ui_busy", codeFor(err), "completed outcomes still require acknowledgement")
	_, err = dispatchResults(t, b, "work.acknowledge", record{"operation_id": id})
	require.NoError(t, err)
	_, err = dispatchResults(t, b, "work.poll", record{"operation_id": id})
	require.Equal(t, "ui_handle_stale", codeFor(err))
}

func workFixture(t *testing.T) (*app.App, *resultsBridge) {
	t.Helper()
	a := &app.App{
		Clock: time.Now, Settings: config.Settings{
			StateDir: filepath.Join(t.TempDir(), "collection"), RecheckInterval: time.Hour,
			Sources:     []config.Source{{Channel: "fixture", Enabled: true, PageLimit: 2}},
			CheckPolicy: state.DefaultCheckPolicy(), Validator: "docker", ValidatorImage: "sha256:fixture",
		}, Validator: validator.Unsupported{Version: "fixture"},
	}
	b := newResultsBridge(
		Options{
			Settings: a.Settings, StateDir: a.Settings.StateDir,
			Work: a.NewWorkSession(), Reports: a.NewReportSession(), Results: a.NewResultsSession(),
		},
	)
	t.Cleanup(func() { _ = b.close() })
	return a, b
}

func awaitWork(t *testing.T, b *resultsBridge, id any) record {
	t.Helper()
	var result record
	require.Eventually(
		t, func() bool {
			v, err := dispatchResults(t, b, "work.poll", record{"operation_id": id})
			require.NoError(t, err)
			result = v.(record)
			require.Equal(t, result["run_created"] == "true", result["run_id"] != nil)
			if check, ok := result["check"].(record); ok {
				totals := check["totals"].(record)
				sum := func(names ...string) int {
					n := 0
					for _, name := range names {
						value, err := strconv.Atoi(totals[name].(string))
						require.NoError(t, err)
						require.GreaterOrEqual(t, value, 0)
						n += value
					}
					return n
				}
				require.Equal(t, sum("selected"), sum("pending", "active", "completed", "skipped", "interrupted"))
				require.Equal(t, sum("selected"), sum("confirmed", "invalid", "inconclusive", "unresolved", "unstarted"))
			}
			return result["phase"] == "complete"
		}, 90*time.Second, 10*time.Millisecond,
	)
	return result
}

// Spec 0012 AC-1, AC-5 through AC-9, AC-11 and AC-14: real store commits,
// independent reports, consumed reviews and cancellation retain partial evidence.
func TestTerminalWork_CrawlCancellationAndFixedReport(t *testing.T) {
	a, b := workFixture(t)
	entered := make(chan struct{})
	a.Source = workSource(
		func(ctx context.Context, channel string, before *int64) (telegram.Page, error) {
			if before != nil {
				close(entered)
				<-ctx.Done()
				return telegram.Page{}, ctx.Err()
			}
			at, id := time.Now(), int64(2)
			return telegram.Page{
				Facts: state.CrawlPage{
					FetchedAt: at, ParsedPostCount: 1,
					MinMessageID: &id, MaxMessageID: &id,
				}, NextBeforeID: &id,
				Messages: []state.MessageInput{
					{
						SourceID: "telegram:" + channel, MessageID: "2",
						ObservedAt: at, Keys: []string{"private-work-fixture"},
					},
				},
			}, nil
		},
	)
	review := dispatchResultsJob(t, b, "work.review_crawl", record{"channels": []string{"fixture"}, "page_limit": nil})
	_, err := os.Stat(a.Settings.StateDir)
	require.True(t, os.IsNotExist(err), "review must not create collection state")
	start, err := dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
	require.NoError(t, err)
	id := start.(record)["operation_id"]
	_, err = dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
	require.Equal(t, "ui_handle_stale", codeFor(err))
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("second source page was not entered")
	}
	progress, err := dispatchResults(t, b, "work.poll", record{"operation_id": id})
	require.NoError(t, err)
	run := progress.(record)["run_id"]
	require.Equal(t, "1", progress.(record)["crawl"].(record)["saved_candidates"])
	snapshot := dispatchResultsJob(t, b, "report.open", record{"run_id": run})
	require.Equal(t, state.Running, snapshot["run"].(record)["status"])
	_, err = dispatchResults(t, b, "work.cancel", record{"operation_id": id})
	require.NoError(t, err)
	outcome := awaitWork(t, b, id)
	require.Equal(t, "cancelled", outcome["state"])
	require.Equal(t, "cancelled", outcome["run_status"])
	require.Equal(t, "known", outcome["persistence"])
	fixed, err := dispatchResults(t, b, "report.state", record{"report_id": snapshot["report_id"]})
	require.NoError(t, err)
	require.Equal(t, state.Running, fixed.(record)["run"].(record)["status"])
	pages, err := dispatchResults(
		t, b, "report.page", record{
			"report_id": snapshot["report_id"],
			"section":   "pages", "source_id": "telegram:fixture", "candidate_id": nil, "offset": "0",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "2", pages.(record)["total"])
	for _, v := range []any{review, progress, snapshot, outcome, pages} {
		raw, err := json.Marshal(v)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "private-work-fixture")
	}
	_, err = dispatchResults(t, b, "work.acknowledge", record{"operation_id": id})
	require.NoError(t, err)
	require.False(t, b.options.Work.Busy())
}

// Spec 0012 AC-3 and AC-11: matching review and report traversal include rows
// beyond the UI page and preserve the complete saved batch order.
func TestTerminalWork_CompleteCheckSelectionAndReport(t *testing.T) {
	a, b := workFixture(t)
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "fixture", Kind: state.Crawl, StartedAt: a.Clock()})
	require.NoError(t, err)
	keys := make([]string, 338)
	for i := range keys {
		keys[i] = fmt.Sprintf("private-selection-%d", i)
	}
	_, err = w.RecordMessage(
		t.Context(), state.MessageInput{
			RunID: run.ID, SourceID: "fixture",
			MessageID: "1", ObservedAt: a.Clock(), Keys: keys,
		},
	)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	dispatchResultsJob(t, b, "results.open", record{})
	review := dispatchResultsJob(
		t, b, "work.review_check", record{
			"view_id": "1", "selection": "matching", "candidate_id": nil,
		},
	)
	require.Equal(t, "338", review["selected_count"])
	start, err := dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
	require.NoError(t, err)
	outcome := awaitWork(t, b, start.(record)["operation_id"])
	require.Equal(t, "failed", outcome["state"])
	require.Equal(t, "completed", outcome["run_status"])
	report := dispatchResultsJob(t, b, "report.open", record{"run_id": outcome["run_id"]})
	require.Equal(t, "338", report["item_count"])
	var total int
	offset := any("0")
	for offset != nil {
		page, err := dispatchResults(
			t, b, "report.page", record{
				"report_id": report["report_id"],
				"section":   "items", "source_id": nil, "candidate_id": nil, "offset": offset,
			},
		)
		require.NoError(t, err)
		p := page.(record)
		total += len(p["items"].([]any))
		offset = p["next_offset"]
	}
	require.Equal(t, 338, total)
}

// A stale absent review cannot recover a collection that appeared before Start.
func TestTerminalWork_RejectsReplacementBeforeRecovery(t *testing.T) {
	a, b := workFixture(t)
	a.Source = workSource(
		func(context.Context, string, *int64) (telegram.Page, error) {
			t.Error("stale review reached a source")
			return telegram.Page{}, nil
		},
	)
	review := dispatchResultsJob(t, b, "work.review_crawl", record{"channels": []string{"fixture"}, "page_limit": nil})
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	run, err := w.StartRun(
		t.Context(),
		state.RunInput{RequestID: "replacement", Kind: state.Crawl, StartedAt: a.Clock()},
	)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	start, err := dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
	require.NoError(t, err)
	outcome := awaitWork(t, b, start.(record)["operation_id"])
	require.Equal(t, "ui_collection_changed", outcome["error_code"])
	require.Equal(t, "false", outcome["run_created"])
	r, err := store.OpenReader(t.Context(), a.Settings.StateDir)
	require.NoError(t, err)
	saved, err := r.GetCrawlReport(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, state.Running, saved.Run.Status, "replacement history must not be recovered")
	require.NoError(t, r.Close())
}

type unresolvedBackend struct{ validator.Unsupported }

func (unresolvedBackend) Acquire(context.Context) (validator.BatchOwner, error) {
	return unresolvedOwner{}, nil
}

type unresolvedOwner struct{}

func (unresolvedOwner) Close() error { return nil }
func (unresolvedOwner) Run(context.Context, validator.Input, validator.Hooks) (validator.Result, error) {
	return validator.Result{
		Outcome: state.Inconclusive, ReasonCode: "validation_cleanup_pending",
		FailureClass: state.Unsafe,
	}, nil
}

// Spec 0012 AC-15: a later crawl cannot replace the originating cleanup receipt.
func TestTerminalWork_RetainsCleanupAcrossAcknowledgementAndCrawl(t *testing.T) {
	a, b := workFixture(t)
	a.Validator = unresolvedBackend{}
	a.Source = workSource(
		func(_ context.Context, channel string, _ *int64) (telegram.Page, error) {
			at, id := a.Clock(), int64(1)
			return telegram.Page{
				Facts: state.CrawlPage{
					FetchedAt: at, ParsedPostCount: 1,
					MinMessageID: &id, MaxMessageID: &id,
				}, Messages: []state.MessageInput{
					{
						SourceID:  "telegram:" + channel,
						MessageID: "1", ObservedAt: at, Keys: []string{"private-retained-fixture"},
					},
				},
			}, nil
		},
	)
	_, err := a.Crawl(t.Context())
	require.NoError(t, err)
	dispatchResultsJob(t, b, "results.open", record{})
	review := dispatchResultsJob(
		t, b, "work.review_check", record{
			"view_id": "1", "selection": "focused", "candidate_id": "1",
		},
	)
	start, err := dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
	require.NoError(t, err)
	checkID := start.(record)["operation_id"]
	outcome := awaitWork(t, b, checkID)
	require.Equal(t, "unresolved", outcome["cleanup"])
	_, err = dispatchResults(t, b, "work.acknowledge", record{"operation_id": checkID})
	require.NoError(t, err)
	review = dispatchResultsJob(t, b, "work.review_crawl", record{"channels": []string{"fixture"}, "page_limit": "1"})
	start, err = dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
	require.NoError(t, err)
	crawl := awaitWork(t, b, start.(record)["operation_id"])
	require.Equal(t, "succeeded", crawl["state"])
	receipt := crawl["unresolved_cleanup"].(record)
	require.Equal(t, checkID, receipt["operation_id"])
	require.Equal(t, outcome["run_id"], receipt["run_id"])
	require.NotEqual(t, crawl["run_id"], receipt["run_id"])
	_, err = dispatchResults(t, b, "work.acknowledge", record{"operation_id": start.(record)["operation_id"]})
	require.NoError(t, err)
	review = dispatchResultsJob(
		t, b, "work.review_check", record{
			"view_id": "1", "selection": "focused", "candidate_id": "1",
		},
	)
	_, err = dispatchResults(t, b, "work.start", record{"review_id": review["review_id"]})
	require.Equal(t, "ui_work_recovery_required", codeFor(err))
}
