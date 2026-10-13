package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// AC-4, AC-6 and AC-8: PID reuse, native drift, and mixed observations cannot
// establish readiness, even when a previously checked owner was running.
func TestStartRequiresAgreeingInvocationAndNativeIdentity(t *testing.T) {
	for _, kind := range []string{
		"ready", "paused", "starting", "becoming_ready", "reused_pid", "changed_pid",
		"old_installation", "new_installation", "foreign_unit", "absent_unit", "unknown_native_pid",
		"other_native_pid", "unknown_owner", "released_owner", "other_owner", "stopping", "stopped", "failed",
	} {
		t.Run(kind, func(t *testing.T) {
			first := observationFixture()
			first.Phase = "running"
			second := first
			pid, owner := first.PID, first.PID
			ownerPtr := &owner
			s := ManagerState{Loaded: true, PID: &pid, Identity: "verified"}
			want := ""
			switch kind {
			case "ready":
				want = "running"
			case "paused":
				first.Phase, second.Phase = "clock_paused", "clock_paused"
				want = "running"
			case "starting":
				first.Phase, second.Phase = "starting", "starting"
				want = "starting"
			case "becoming_ready":
				first.Phase = "starting"
				want = "starting"
			case "reused_pid":
				second.InvocationID = strings.Repeat("c", 32)
			case "changed_pid":
				second.PID++
			case "old_installation":
				first.InstallationID = strings.Repeat("c", 32)
			case "new_installation":
				second.InstallationID = strings.Repeat("c", 32)
			case "foreign_unit":
				s.Identity = "unverified"
			case "absent_unit":
				s.Identity = "absent"
			case "unknown_native_pid":
				s.PID = nil
			case "other_native_pid":
				pid++
			case "unknown_owner":
				ownerPtr = nil
			case "released_owner":
				owner = 0
			case "other_owner":
				owner++
			case "stopping", "stopped", "failed":
				second.Phase = kind
			}
			require.Equal(t, want, startState(strings.Repeat("a", 32), first, second, s, ownerPtr))
		})
	}
}

// AC-5 and AC-8: a failed observation is attributable only after ownership and
// native work are gone. Unknown jobs and foreign units preserve uncertainty.
func TestStartFailureRequiresReleasedOwnerAndNoNativeWork(t *testing.T) {
	for _, kind := range []string{
		"stopped", "absent", "pending_job", "unknown_job", "running", "foreign_unit",
		"held_owner", "unknown_owner", "older_failure", "replaced_failure",
	} {
		t.Run(kind, func(t *testing.T) {
			first := observationFixture()
			first.Phase = "failed"
			second := first
			pid, owner, running, pending := 0, 0, false, false
			active := "inactive"
			ownerPtr := &owner
			s := ManagerState{
				Loaded: true, Identity: "verified", PID: &pid, Running: &running, PendingJob: &pending,
				ActiveState: &active,
			}
			want := ""
			switch kind {
			case "stopped":
				want = "failed"
			case "absent":
				s = ManagerState{Identity: "absent"}
				want = "failed"
			case "pending_job":
				pending = true
			case "unknown_job":
				s.PendingJob = nil
			case "running":
				running, pid = true, first.PID
			case "foreign_unit":
				s.Identity = "unverified"
			case "held_owner":
				owner = first.PID
			case "unknown_owner":
				ownerPtr = nil
			case "older_failure":
				first.Phase = "running"
			case "replaced_failure":
				second.InvocationID = strings.Repeat("c", 32)
			}
			require.Equal(t, want, startState(first.InstallationID, first, second, s, ownerPtr))
		})
	}
}
