package terminal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// Spec 0011 AC-10 and AC-15: polling cannot wait on an entered commit operation,
// and cancellation after admission cannot fabricate a rollback of its result.
func TestResultsJob_PollsWhileCommitIsEntered(t *testing.T) {
	b := newResultsBridge(Options{})
	entered, release := make(chan struct{}), make(chan struct{})
	b.start(t.Context(), "clear_marks", "view", nil,
		func(ctx context.Context, _ app.ResultsProgress, commit app.ResultsCommit) (any, error) {
			err := commit(func() error { close(entered); <-release; return nil })
			return record{"type": "view", "value": "committed fixture"}, err
		})
	<-entered
	polled := make(chan any, 1)
	go func() { r, _ := b.poll(1, true); polled <- r }()
	select {
	case r := <-polled:
		require.Equal(t, "cancelling", r.(record)["state"])
	case <-time.After(time.Second):
		close(release)
		t.Fatal("poll waited on entered commit")
	}
	b.mu.Lock()
	first := b.job.cancelAt
	b.mu.Unlock()
	_, err := b.poll(1, true)
	require.NoError(t, err)
	b.mu.Lock()
	require.Equal(t, first, b.job.cancelAt)
	b.mu.Unlock()
	close(release)
	b.work.Wait()
	r, err := b.poll(1, false)
	require.NoError(t, err)
	require.Equal(t, "succeeded", r.(record)["state"])
	require.Nil(t, r.(record)["error_code"])
	require.NoError(t, b.close())
}

// covers: spec 0011 AC-3, AC-7, AC-10, AC-13. Changing a reviewed selection cannot publish it later.
func TestResultsDispatch_InvalidatesReview(t *testing.T) {
	for _, change := range []string{"mark", "clear marks", "filter", "refresh", "discard"} {
		t.Run(change, func(t *testing.T) {
			b := resultsBridgeFixture(t)
			review := dispatchResultsJob(t, b, "export.review", record{"view_id": "1", "selection": "matching",
				"format": "json", "include_keys": false, "path": "result.json"})
			id := review["review_id"]
			switch change {
			case "mark":
				_, err := dispatchResults(t, b, "results.mark", record{
					"view_id": "1", "candidate_id": "1", "marked": true})
				require.NoError(t, err)
			case "clear marks":
				dispatchResultsJob(t, b, "results.clear_marks", record{"view_id": "1"})
			case "filter":
				dispatchResultsJob(t, b, "results.filter", record{"view_id": "1", "filters": record{
					"status": []string{"unchecked"}, "definitive": []string{}, "freshness": []string{}}})
			case "refresh":
				dispatchResultsJob(t, b, "results.refresh", record{"view_id": "1", "focus_id": "1"})
			case "discard":
				_, err := dispatchResults(t, b, "export.discard", record{"review_id": id})
				require.NoError(t, err)
			}
			_, err := dispatchResults(t, b, "export.start", record{"review_id": id})
			require.Equal(t, "ui_handle_stale", codeFor(err))
		})
	}
}

// covers: spec 0011 AC-7, AC-8, AC-10. A review is consumed once and exports only its marked set.
func TestResultsDispatch_ConsumesReviewedExportOnce(t *testing.T) {
	b := resultsBridgeFixture(t)
	params := record{"view_id": "1", "selection": "marked", "format": "json",
		"include_keys": false, "path": "result.json"}
	_, err := dispatchResults(t, b, "export.review", params)
	require.Equal(t, "ui_export_selection_empty", codeFor(err))
	_, err = dispatchResults(t, b, "results.mark", record{"view_id": "1", "candidate_id": "2", "marked": true})
	require.NoError(t, err)
	review := dispatchResultsJob(t, b, "export.review", params)
	require.Equal(t, "1", review["selected_count"])
	require.Equal(t, "2", review["matching_count"])
	require.Equal(t, false, review["include_keys"])
	calls := 0
	b.options.Export = func(ctx context.Context, r ExportReview, rows ExportRows,
		_ func(string, ExportReceipt)) ExportReceipt {
		calls++
		var ids []int64
		err := rows(ctx, func(c state.Candidate) error { ids = append(ids, c.ID); return nil })
		if err != nil || len(ids) != 1 || ids[0] != 2 {
			return ExportReceipt{ErrorCode: "export_prepare_failed"}
		}
		return ExportReceipt{Destination: r.Destination, Selection: r.Selection, Format: r.Format,
			Count: r.Count, Prepared: 1, Published: new(true), Durability: "confirmed"}
	}
	receipt := dispatchResultsJob(t, b, "export.start", record{"review_id": review["review_id"]})
	require.Equal(t, "1", receipt["prepared_count"])
	_, err = dispatchResults(t, b, "export.start", record{"review_id": review["review_id"]})
	require.Equal(t, "ui_handle_stale", codeFor(err))
	require.Equal(t, 1, calls)
}

// covers: spec 0011 AC-4, AC-5, AC-13. Only the active detail can reveal its one exact key.
func TestResultsDispatch_RevealRequiresCurrentDetail(t *testing.T) {
	b := resultsBridgeFixture(t)
	page, err := dispatchResults(t, b, "results.page", record{"view_id": "1", "offset": "0"})
	require.NoError(t, err)
	detail := dispatchResultsJob(t, b, "details.open", record{"view_id": "1", "candidate_id": "1"})
	history, err := dispatchResults(t, b, "details.page", record{
		"detail_id": detail["detail_id"], "section": "sources", "offset": "0"})
	require.NoError(t, err)
	for _, value := range []any{page, detail, history} {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, string(data), "private-fixture")
		require.NotContains(t, string(data), "private-request")
	}
	params := record{"view_id": "1", "detail_id": detail["detail_id"], "candidate_id": "2"}
	_, err = dispatchResults(t, b, "candidate.reveal", params)
	require.Equal(t, "ui_candidate_unavailable", codeFor(err))
	params["candidate_id"] = "1"
	revealed, err := dispatchResults(t, b, "candidate.reveal", params)
	require.NoError(t, err)
	require.Equal(t, "private-fixture-one", revealed.(record)["key"])
	_, err = dispatchResults(t, b, "details.close", record{"detail_id": detail["detail_id"]})
	require.NoError(t, err)
	_, err = dispatchResults(t, b, "candidate.reveal", params)
	require.Equal(t, "ui_handle_stale", codeFor(err))
}

func resultsBridgeFixture(t *testing.T) *resultsBridge {
	t.Helper()
	ctx := t.Context()
	dir := filepath.Join(t.TempDir(), "state")
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	w, err := store.OpenWriter(ctx, dir, clock)
	require.NoError(t, err)
	run, err := w.StartRun(ctx, state.RunInput{RequestID: "private-request", Kind: state.Crawl, StartedAt: now})
	require.NoError(t, err)
	_, err = w.RecordMessage(ctx, state.MessageInput{RunID: run.ID, SourceID: "fixture", MessageID: "1",
		ObservedAt: now, Keys: []string{"private-fixture-one", "private-fixture-two"}})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	a := &app.App{Settings: config.Settings{StateDir: dir, RecheckInterval: time.Hour}, Clock: clock}
	parent := t.TempDir()
	b := newResultsBridge(Options{Results: a.NewResultsSession(), StateDir: dir,
		Destination: func(_ context.Context, path string, _ []string) (string, os.FileInfo, error) {
			info, err := os.Stat(parent)
			return filepath.Join(parent, path), info, err
		}, Export: func(context.Context, ExportReview, ExportRows, func(string, ExportReceipt)) ExportReceipt {
			t.Error("an invalidated review reached the export boundary")
			return ExportReceipt{ErrorCode: "export_prepare_failed"}
		}})
	t.Cleanup(func() { require.NoError(t, b.close()) })
	dispatchResultsJob(t, b, "results.open", record{})
	return b
}

func dispatchResults(t *testing.T, b *resultsBridge, method string, params record) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	return b.dispatch(t.Context(), request{ID: "1", Method: method, Params: raw})
}

func dispatchResultsJob(t *testing.T, b *resultsBridge, method string, params record) record {
	t.Helper()
	_, err := dispatchResults(t, b, method, params)
	require.NoError(t, err)
	b.work.Wait()
	result, err := b.poll(b.nextJob, false)
	require.NoError(t, err)
	j := result.(record)
	require.Equal(t, "succeeded", j["state"], "%v", j["error_code"])
	return j["result"].(record)["value"].(record)
}

// Spec 0011 AC-13: closed method parameters admit only canonical typed inputs.
func TestResultsRequest_ClosedParameters(t *testing.T) {
	for _, tt := range []struct {
		method, params string
		valid          bool
	}{
		{"results.open", `{}`, true},
		{"results.refresh", `{"view_id":"1","focus_id":null}`, true},
		{"results.refresh", `{"view_id":null,"focus_id":null}`, false},
		{"results.page", `{"view_id":"1","offset":"0"}`, true},
		{"results.page", `{"view_id":"1","offset":"9223372036854775808"}`, false},
		{"results.page", `{"view_id":"1","offset":0}`, false},
		{"results.page", `{"view_id":"1","offset":"00"}`, false},
		{"results.mark", `{"view_id":"1","candidate_id":"2","marked":null}`, false},
		{"results.filter", `{"view_id":"1","filters":{"status":[],"definitive":[],"freshness":[]}}`, true},
		{"results.filter", `{"view_id":"1","filters":{"status":null,"definitive":[],"freshness":[]}}`, false},
		{"results.filter", `{"view_id":"1","filters":{"status":["invalid","unchecked"],` +
			`"definitive":[],"freshness":[]}}`, false},
		{"candidate.reveal", `{"view_id":"1","detail_id":"1","candidate_id":"1","key":"forbidden"}`, false},
		{"export.review", `{"view_id":"1","selection":"matching","format":"keys","include_keys":false,"path":"a"}`, false},
	} {
		t.Run(tt.method+tt.params, func(t *testing.T) {
			_, err := requestParams(request{Method: tt.method, Params: json.RawMessage(tt.params)})
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
