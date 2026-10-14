package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/diagnostic"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/stretchr/testify/require"
)

// Spec 0009 AC-1 and AC-4: a typed failure without an outcome stays visible at info.
func TestAttemptDiagnosticRetainsTypedFailureAfterCleanup(t *testing.T) {
	a := batchApp(t, 1)
	var logs bytes.Buffer
	sink := diagnostic.NewSink(&logs, diagnostic.Options{Level: "info", Format: "json", Color: "never"},
		diagnostic.Capabilities{}, time.UTC, nil)
	a.Logger = diagnostic.NewLogger(sink, time.Now)
	a.Validator = &batchFixture{run: func(context.Context, validator.Input, validator.Hooks) (validator.Result, error) {
		return validator.Result{CleanupProven: true, Removed: true}, &validator.Error{Code: "validator_backend_unavailable"}
	}}
	report, err := a.CheckBatch(t.Context(), []int64{1}, nil)
	require.Error(t, err)
	require.NoError(t, sink.Err())
	var attempt struct {
		Event       string `json:"event"`
		Level       string `json:"level"`
		Code        string `json:"code"`
		RunID       int64  `json:"run_id"`
		CandidateID int64  `json:"candidate_id"`
		AttemptID   int64  `json:"attempt_id"`
	}
	found := false
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		require.NoError(t, json.Unmarshal(line, &attempt))
		if attempt.Event != "attempt_completed" {
			continue
		}
		found = true
		require.Equal(t, "warn", attempt.Level)
		require.Equal(t, "validator_backend_unavailable", attempt.Code)
		require.Equal(t, report.Run.ID, attempt.RunID)
		require.Equal(t, int64(1), attempt.CandidateID)
		require.Positive(t, attempt.AttemptID)
	}
	require.True(t, found, "the failed attempt must not be hidden by the info filter")
}
