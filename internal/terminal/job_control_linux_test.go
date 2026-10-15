package terminal_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Spec 0011 AC-10: a real child must retain a cancellation pressed while the
// parent's start acknowledgement is withheld. Poll and cancel never overlap.
func driveEarlyCancellation(t *testing.T, master *os.File, output *terminalOutput,
	read func(string) string, reply func(string, any)) {
	t.Helper()
	stamp := map[string]any{"utc": "2026-10-15T00:00:00.000000Z", "local": "2026-10-15 00:00:00",
		"utc_offset_seconds": "0"}
	missing := func(code string) any {
		return map[string]any{
			"availability": "missing", "observed_at": stamp, "age_ns": "0", "clock_anomaly": false,
			"cache_policy": "discard", "error_code": code, "data": nil,
		}
	}
	reply(read("overview"), map[string]any{
		"header_at": stamp, "collection": missing("state_not_initialized"), "service": map[string]any{
			"identity":         map[string]any{"state": "missing", "installation_id": nil, "cache_policy": "discard"},
			"collection_match": "unknown", "bound_state_dir": nil, "installation": missing("service_not_installed"),
			"manager": missing("service_not_installed"), "runner": missing("service_not_installed"),
		},
	})
	d := resultsDriver{t, master, output}
	d.wait(0, "Overview  | Ready")
	_, err := master.WriteString("b")
	require.NoError(t, err)
	start := read("results.open")
	_, err = master.WriteString("c")
	require.NoError(t, err)
	// The local feedback proves the key was processed before admitting the job.
	require.Eventually(t, func() bool { return strings.Contains(output.text(), "Cancellation requested") },
		time.Second, 10*time.Millisecond)
	reply(start, map[string]any{"job_id": "1"})
	job := func(state string) any {
		var code any
		if state == "cancelled" {
			code = "cancelled"
		}
		return map[string]any{"job_id": "1", "kind": "open", "state": state, "phase": "collection",
			"completed_count": "0", "expected_count": nil, "error_code": code, "warning_code": nil,
			"remaining_path": nil, "result": nil}
	}
	reply(read("job.poll"), job("running"))
	reply(read("job.cancel"), job("cancelling"))
	after := len(output.text())
	reply(read("job.poll"), job("cancelled"))
	d.wait(after, "cancelled (collection)")
	_, err = master.WriteString("q")
	require.NoError(t, err)
}
