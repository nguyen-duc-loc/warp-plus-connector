package store_test

import (
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

func crawlInput() state.CrawlInput {
	return state.CrawlInput{
		RequestID: "page", StartedAt: fixtureTime, SourceID: "telegram:warpplus",
		RequestedURL: "https://t.me/s/warpplus", CoverageMode: "latest_preview", PageLimit: 1,
	}
}

// covers: spec 0003 AC-2, AC-9. Coverage is durable, immutable and replayable.
func TestCrawlCoverage_ReplaysAndSurvivesRecovery(t *testing.T) {
	w, dir := openWriter(t)
	in := crawlInput()
	report, err := w.StartCrawl(t.Context(), in)
	require.NoError(t, err)
	require.Nil(t, report.Coverage.FetchedAt)
	require.Nil(t, report.Coverage.ParsedPostCount)
	retry, err := w.StartCrawl(t.Context(), in)
	require.NoError(t, err)
	require.Equal(t, report, retry)
	minID, maxID := int64(2), int64(20)
	page := state.CrawlPage{
		RunID: report.Run.ID, FetchedAt: fixtureTime, ParsedPostCount: 3, MinMessageID: &minID, MaxMessageID: &maxID,
	}
	coverage, err := w.RecordCrawlPage(t.Context(), page)
	require.NoError(t, err)
	again, err := w.RecordCrawlPage(t.Context(), page)
	require.NoError(t, err)
	require.Equal(t, coverage, again)
	changed := page
	changed.ParsedPostCount++
	_, err = w.RecordCrawlPage(t.Context(), changed)
	require.ErrorIs(t, err, store.ErrConflict)
	require.NoError(t, w.Close())
	recovery := fixtureTime.Add(time.Hour)
	w, err = store.OpenWriter(t.Context(), dir, func() time.Time { return recovery })
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	report, err = w.GetCrawlReport(t.Context(), report.Run.ID)
	require.NoError(t, err)
	require.Equal(t, state.Interrupted, report.Run.Status)
	require.Equal(t, coverage, *report.Coverage)
	require.Nil(t, report.Run.FinishedAt)
	require.Equal(t, recovery, *report.Run.RecoveredAt)
	again, err = w.RecordCrawlPage(t.Context(), page)
	require.NoError(t, err)
	require.Equal(t, coverage, again)
}

// covers: spec 0003 AC-2, AC-6. Invalid coverage cannot partially initialize a run.
func TestStartCrawl_RejectsInvalidCoverageAtomically(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*state.CrawlInput)
	}{
		{"request", func(v *state.CrawlInput) { v.RequestID = "" }},
		{"time", func(v *state.CrawlInput) { v.StartedAt = time.Time{} }},
		{"source", func(v *state.CrawlInput) { v.SourceID = "other" }},
		{"url", func(v *state.CrawlInput) { v.RequestedURL = "https://example.invalid" }},
		{"mode", func(v *state.CrawlInput) { v.CoverageMode = "history" }},
		{"pages", func(v *state.CrawlInput) { v.PageLimit = 2 }},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				w, _ := openWriter(t)
				in := crawlInput()
				tc.change(&in)
				_, err := w.StartCrawl(t.Context(), in)
				require.ErrorIs(t, err, store.ErrInvalidInput)
				runs, err := w.ListRuns(t.Context(), "", state.PageRequest{})
				require.NoError(t, err)
				require.Empty(t, runs.Items)
			},
		)
	}
}

// covers: spec 0003 AC-2. Absent facts never become an invented empty result.
func TestRecordCrawlPage_RejectsInvalidAndClosedInputs(t *testing.T) {
	w, _ := openWriter(t)
	report, err := w.StartCrawl(t.Context(), crawlInput())
	require.NoError(t, err)
	zero, one, two := int64(0), int64(1), int64(2)
	for _, tc := range []struct {
		name     string
		count    int
		min, max *int64
	}{
		{"negative count", -1, nil, nil}, {"empty with bounds", 0, &one, &one},
		{"missing bounds", 1, nil, nil}, {"zero bound", 1, &zero, &one}, {"reversed bounds", 1, &two, &one},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				_, err := w.RecordCrawlPage(
					t.Context(),
					state.CrawlPage{
						RunID: report.Run.ID, FetchedAt: fixtureTime, ParsedPostCount: tc.count, MinMessageID: tc.min,
						MaxMessageID: tc.max,
					},
				)
				require.ErrorIs(t, err, store.ErrInvalidInput)
				saved, err := w.GetCrawlReport(t.Context(), report.Run.ID)
				require.NoError(t, err)
				require.Nil(t, saved.Coverage.FetchedAt)
			},
		)
	}
	_, err = w.FinishRun(
		t.Context(),
		state.RunCompletion{
			RunID: report.Run.ID, Status: state.Failed, FinishedAt: fixtureTime, ReasonCode: "source_http_error",
		},
	)
	require.NoError(t, err)
	_, err = w.RecordCrawlPage(t.Context(), state.CrawlPage{RunID: report.Run.ID, FetchedAt: fixtureTime})
	require.ErrorIs(t, err, store.ErrRunClosed)
	validation := startRun(t, w, state.Validation)
	_, err = w.GetCrawlReport(t.Context(), validation.ID)
	require.ErrorIs(t, err, store.ErrInvalidInput)
}
