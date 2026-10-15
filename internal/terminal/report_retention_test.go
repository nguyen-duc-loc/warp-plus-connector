package terminal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// Spec 0012 AC-12: failed refresh preserves the report until its collection changes.
func TestReportRefresh_RetainsPrivateAccessFailure(t *testing.T) {
	a, b := workFixture(t)
	w, err := store.OpenWriter(t.Context(), a.Settings.StateDir, a.Clock)
	require.NoError(t, err)
	run, err := w.StartRun(t.Context(), state.RunInput{RequestID: "retention", Kind: state.Crawl, StartedAt: a.Clock()})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	initial := dispatchResultsJob(t, b, "report.open", record{"run_id": decimal(run.ID)})
	database := filepath.Join(a.Settings.StateDir, "state.db")
	require.NoError(t, os.Chmod(database, 0644))
	t.Cleanup(func() { _ = os.Chmod(database, 0600) })
	_, err = dispatchResults(t, b, "report.refresh", record{
		"report_id": initial["report_id"], "focus": initial["focus"],
	})
	require.NoError(t, err)
	b.work.Wait()
	result, err := b.poll(b.nextJob, false)
	require.NoError(t, err)
	require.Equal(t, "failed", result.(record)["state"])
	require.Equal(t, "state_error", result.(record)["error_code"])
	retained, err := dispatchResults(t, b, "report.state", record{"report_id": initial["report_id"]})
	require.NoError(t, err)
	want := record{}
	for key, value := range initial {
		want[key] = value
	}
	want["retained"], want["error_code"] = true, "state_error"
	require.Equal(t, want, retained)
	_, err = dispatchResults(t, b, "report.page", record{
		"report_id": initial["report_id"], "section": "sources",
		"source_id": nil, "candidate_id": nil, "offset": "0",
	})
	require.NoError(t, err)
	require.NoError(t, os.Chmod(database, 0600))
	refreshed := dispatchResultsJob(t, b, "report.refresh", record{
		"report_id": initial["report_id"], "focus": initial["focus"],
	})
	require.False(t, refreshed["retained"].(bool))
	require.Nil(t, refreshed["error_code"])
	require.NotEqual(t, initial["report_id"], refreshed["report_id"])

	// A replacement with broad permissions is still a different collection.
	require.NoError(t, os.Rename(database, database+".original"))
	require.NoError(t, os.WriteFile(database, []byte("replacement"), 0644))
	_, err = dispatchResults(t, b, "report.state", record{"report_id": refreshed["report_id"]})
	require.Equal(t, "ui_collection_changed", codeFor(err))
	_, err = dispatchResults(t, b, "report.state", record{"report_id": refreshed["report_id"]})
	require.Equal(t, "ui_handle_stale", codeFor(err))
	_, err = dispatchResults(t, b, "report.close", record{"report_id": refreshed["report_id"]})
	require.NoError(t, err)
}
