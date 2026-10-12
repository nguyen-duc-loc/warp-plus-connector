package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// Spec 0008 AC-8, AC-11, AC-13: unknown process evidence cannot establish a
// stopped or interrupted runner. Findings retain independent saved failures.
func TestDoctorRunnerEvidence(t *testing.T) {
	yes, no, pid, otherPID := true, false, 123, 456
	active := observationFixture()
	active.PID, active.Phase = pid, "running"
	stopped := active
	stopped.Phase = "stopped"
	failed := active
	failed.Phase = "failed"
	manager := ManagerState{Loaded: true, PID: &pid, Running: &yes, PendingJob: &no}
	idle := ManagerState{Loaded: true, Running: &no, PendingJob: &no}
	for _, tc := range []struct {
		name       string
		runner     RunnerStatus
		manager    ManagerState
		managerErr error
		want       []string
	}{
		{
			name:   "pending first start without an observation",
			runner: RunnerStatus{LockHeld: &no}, manager: ManagerState{Loaded: true, PendingJob: &yes},
			want: []string{"warn service_starting check_status_later"},
		},
		{
			name:    "pending first start before lock file creation",
			manager: ManagerState{Loaded: true, PendingJob: &yes},
			want:    []string{"warn service_starting check_status_later"},
		},
		{
			name:   "missing lock does not prove interruption",
			runner: RunnerStatus{Observation: &active}, manager: idle,
			want: []string{"warn service_owner_unknown open_supported_user_session"},
		},
		{
			name:       "manager failure does not prove no pending restart",
			runner:     RunnerStatus{LockHeld: &no, Observation: &active},
			managerErr: errors.New("unavailable"),
			want:       []string{"warn service_owner_unknown open_supported_user_session"},
		},
		{
			name:   "unknown pending job does not prove interruption",
			runner: RunnerStatus{LockHeld: &no, Observation: &active}, manager: ManagerState{Loaded: true},
			want: []string{"warn service_owner_unknown open_supported_user_session"},
		},
		{
			name:   "released lock and absent native service prove interruption",
			runner: RunnerStatus{LockHeld: &no, Observation: &active},
			want:   []string{"fail service_interrupted inspect_service_logs"},
		},
		{
			name:   "matching live owner with fresh progress",
			runner: RunnerStatus{LockHeld: &yes, OwnerPID: &pid, Observation: &active}, manager: manager,
			want: []string{"pass ok none"},
		},
		{
			name:   "native PID disagrees with lock owner",
			runner: RunnerStatus{LockHeld: &yes, OwnerPID: &otherPID, Observation: &active}, manager: manager,
			want: []string{"fail service_owner_mismatch inspect_native_registration"},
		},
		{
			name:   "held owner without an observation",
			runner: RunnerStatus{LockHeld: &yes, OwnerPID: &pid}, manager: manager,
			want: []string{"fail service_observation_unavailable inspect_service_logs"},
		},
		{
			name:    "stale live progress",
			runner:  RunnerStatus{LockHeld: &yes, OwnerPID: &pid, Observation: &active, Stale: &yes},
			manager: manager, want: []string{"fail service_observation_stale inspect_service_logs"},
		},
		{
			name:   "old stopped observation does not fail on age",
			runner: RunnerStatus{LockHeld: &no, Observation: &stopped, Stale: &yes}, manager: idle,
			want: []string{"pass service_stopped none"},
		},
		{
			name:    "clock anomaly remains a failure",
			runner:  RunnerStatus{LockHeld: &yes, OwnerPID: &pid, Observation: &active, ClockAnomaly: true},
			manager: manager, want: []string{"fail service_clock_anomaly check_system_clock"},
		},
		{
			name:   "saved failure survives manager outage",
			runner: RunnerStatus{LockHeld: &no, Observation: &failed}, managerErr: errors.New("unavailable"),
			want: []string{
				"fail service_runner_failed inspect_service_logs",
				"warn service_owner_unknown open_supported_user_session",
			},
		},
		{
			name:    "historical failure and clock anomaly do not fail current installation",
			runner:  RunnerStatus{LockHeld: &no, Observation: &failed, InstallationMatches: &no, ClockAnomaly: true},
			manager: idle, want: []string{"warn service_historical_observation none"},
		},
		{
			name:    "missing observation before first start",
			manager: idle, want: []string{"pass service_stopped none"},
		},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				var got []string
				diagnoseRunner(
					tc.runner, tc.manager, tc.managerErr, func(name, status, code, action string) {
						require.Equal(t, "runner", name)
						got = append(got, status+" "+code+" "+action)
					},
				)
				require.Equal(t, tc.want, got)
			},
		)
	}
}
