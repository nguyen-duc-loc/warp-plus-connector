package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

func (f workSource) FetchPage(ctx context.Context, channel string, before *int64) (telegram.Page, error) {
	return f(ctx, channel, before)
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
