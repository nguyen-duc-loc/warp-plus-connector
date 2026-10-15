package terminal

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
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
