package terminal

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
)

// Spec 0010 AC-5, AC-12: the encoded reply omits request tokens and preserves exact IDs and null facts.
func TestOverviewReply_ExactValuesAndPrivateFields(t *testing.T) {
	at := time.Date(2026, 10, 14, 0, 0, 0, 123456000, time.UTC)
	collection := state.OverviewCollection{
		AsOf: at, MaxAge: time.Hour, Outcomes: state.OutcomeCounts{Total: 9007199254740993,
			Unchecked: 9007199254740993},
		RecentRuns: []state.OperationalRun{{Run: state.Run{
			ID: 9007199254740997, RequestID: "synthetic_private_request_token", Kind: state.Validation,
			Status: state.Running, StartedAt: at,
		}}},
	}
	var raw bytes.Buffer
	require.NoError(t, writeReply(&raw, "9007199254740999", overviewResult(state.Overview{
		HeaderAt: at, Collection: state.OverviewComponent[state.OverviewCollection]{
			Availability: "available", ObservedAt: at, CachePolicy: "replace", Data: &collection,
		},
	})))
	require.NotContains(t, raw.String(), "synthetic_private_request_token")
	require.NotContains(t, raw.String(), "RequestID")
	var reply map[string]any
	require.NoError(t, json.Unmarshal(raw.Bytes(), &reply))
	require.Equal(t, "9007199254740999", reply["id"])
	data := reply["result"].(map[string]any)["collection"].(map[string]any)["data"].(map[string]any)
	require.Equal(t, "9007199254740993", data["total"])
	require.Equal(t, "3600000000000", data["max_age_ns"])
	run := data["recent_runs"].([]any)[0].(map[string]any)
	require.Equal(t, "9007199254740997", run["id"])
	require.Equal(t, "unknown", run["origin"])
	for _, name := range []string{"finished_at", "recovered_at", "reason_code", "validation_counts", "crawl_counts"} {
		require.Contains(t, run, name)
		require.Nil(t, run[name])
	}
	require.Equal(t, "2026-10-14T00:00:00.123456Z", data["as_of"].(map[string]any)["utc"])
}

// Spec 0010 AC-6, AC-9: each section keeps its own observation age and clock qualification.
func TestOverviewReply_IndependentObservationAges(t *testing.T) {
	at := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		offset  time.Duration
		anomaly bool
		age     any
	}{
		{"past", -time.Second, false, "1000000000"},
		{"same instant", 0, false, "0"},
		{"future", time.Microsecond, false, nil},
		{"saved anomaly", -time.Second, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := overviewResult(state.Overview{
				HeaderAt:   at,
				Collection: state.OverviewComponent[state.OverviewCollection]{ObservedAt: at.Add(-time.Minute)},
				Service: state.OverviewService{Manager: state.OverviewComponent[state.OverviewManager]{
					ObservedAt: at.Add(tc.offset), ClockAnomaly: tc.anomaly,
				}},
			}).(record)
			manager := out["service"].(record)["manager"].(record)
			require.Equal(t, tc.age, manager["age_ns"])
			require.Equal(t, tc.age == nil, manager["clock_anomaly"])
			require.Equal(t, "60000000000", out["collection"].(record)["age_ns"])
		})
	}
}
