package service

import (
	"context"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
)

// Finding is a safe local diagnosis with one fixed suggested action.
type Finding struct {
	Name            string    `json:"name"`
	Status          string    `json:"status"`
	Code            string    `json:"code"`
	ObservedAt      time.Time `json:"observed_at"`
	SuggestedAction string    `json:"suggested_action"`
}

// DoctorReport retains every finding before the caller selects an exit outcome.
type DoctorReport struct {
	SchemaVersion int       `json:"schema_version"`
	View          string    `json:"view"`
	ObservedAt    time.Time `json:"observed_at"`
	OK            bool      `json:"ok"`
	Checks        []Finding `json:"checks"`
}

// Doctor performs only bounded local reads and never acquires validator ownership.
func Doctor(parent context.Context) DoctorReport {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	out := DoctorReport{SchemaVersion: 1, View: "service_doctor", OK: true, Checks: []Finding{}}
	add := func(name, status, code, action string) {
		out.Checks = append(out.Checks, Finding{name, status, code, timestamp(), action})
		if status == "fail" {
			out.OK = false
		}
	}
	a, err := ResolveAccount(ctx)
	if err != nil {
		add("installation", "fail", diagnosticCode(err), "repair_account_lookup")
		for _, name := range []string{
			"definition", "executable", "configuration", "manager", "runner", "state", "logs", "docker",
		} {
			add(name, "skip", "prerequisite_unavailable", "resolve_prerequisite")
		}
		out.ObservedAt = timestamp()
		return out
	}
	m, manifestErr := a.manifest()
	if manifestErr != nil {
		if missing(manifestErr) {
			add("installation", "warn", "service_not_installed", "install_service")
		} else {
			add("installation", "fail", diagnosticCode(manifestErr), "inspect_private_files")
		}
	} else if m.Phase != "ready" {
		add("installation", "fail", "service_operation_incomplete", "repeat_pending_operation")
	} else {
		add("installation", "pass", "ok", "none")
	}
	var settings config.Settings
	configOK := false
	var nativeState ManagerState
	var nativeErr error
	var binding *Manifest
	if manifestErr == nil {
		binding = &m
	}
	nativeState, nativeErr = inspect(ctx, a, binding)
	for _, name := range []string{
		"definition", "executable", "configuration", "manager", "runner", "state", "logs", "docker",
	} {
		if ctx.Err() != nil {
			add(name, "fail", "service_diagnostic_timeout", "retry_doctor")
			continue
		}
		if manifestErr != nil && name != "manager" && name != "logs" && name != "definition" {
			add(name, "skip", "prerequisite_unavailable", "resolve_prerequisite")
			continue
		}
		switch name {
		case "definition":
			if manifestErr != nil {
				if nativeErr == nil && nativeState.Loaded {
					add(name, "fail", "service_identity_unverified", "inspect_native_registration")
				} else {
					add(name, "skip", "prerequisite_unavailable", "resolve_prerequisite")
				}
			} else if err := verifyDefinition(m); err != nil {
				add(name, "fail", "service_definition_changed", "inspect_native_definition")
			} else if nativeErr == nil && nativeState.Identity == "unverified" {
				action := "inspect_native_registration"
				if m.Manager == "systemd" {
					action = "inspect_native_definition"
				}
				add(name, "fail", nativeIdentityCode(m.Manager), action)
			} else {
				add(name, "pass", "ok", "none")
			}
		case "executable":
			if err := executable(m.ExecutablePath); err != nil {
				add(name, "fail", "service_executable_invalid", "restore_executable")
			} else {
				add(name, "pass", "ok", "none")
			}
		case "configuration":
			settings, err = m.Settings()
			if err != nil {
				action := "fix_configuration"
				if diagnosticCode(err) == "service_binding_changed" {
					action = "reinstall_service"
				}
				add(name, "fail", diagnosticCode(err), action)
			} else {
				configOK = true
				add(name, "pass", "ok", "none")
			}
		case "manager":
			if nativeErr != nil {
				add(name, "fail", "service_manager_unavailable", "open_supported_user_session")
			} else if nativeState.AutostartObserved == nil {
				add(name, "warn", "service_enablement_unknown", "inspect_native_registration")
			} else if binding != nil && m.AutostartEnabled != nil && *m.AutostartEnabled != *nativeState.AutostartObserved {
				add(name, "fail", "service_enablement_changed", "repeat_startup_intent")
			} else {
				add(name, "pass", "ok", "none")
			}
		case "runner":
			c := runnerStatus(a, &m)
			if c.Availability != "available" {
				if codeValue(c.ErrorCode) == "service_owner_unknown" {
					add(name, "warn", "service_owner_unknown", "open_supported_user_session")
				} else {
					add(name, "fail", codeValue(c.ErrorCode), "inspect_private_files")
				}
				continue
			}
			diagnoseRunner(c.Data.(RunnerStatus), nativeState, nativeErr, add)
		case "state":
			_, err := readWork(ctx, m.StateDir)
			if err != nil {
				if diagnosticCode(err) == "state_not_initialized" {
					add(name, "warn", "state_not_initialized", "start_service")
				} else {
					add(name, "fail", diagnosticCode(err), "inspect_saved_state")
				}
			} else {
				add(name, "pass", "ok", "none")
			}
		case "logs":
			view, err := ReadLogs(ctx, a, 10000)
			if err != nil {
				add(name, "fail", diagnosticCode(err), "inspect_service_logs")
			} else if view.TruncatedTail || view.ContinuityUnknown {
				add(name, "warn", "service_log_gap", "none")
			} else if view.Returned == 0 {
				add(name, "warn", "service_logs_empty", "none")
			} else {
				add(name, "pass", "ok", "none")
			}
		case "docker":
			if !configOK {
				add(name, "skip", "prerequisite_unavailable", "resolve_prerequisite")
			} else if !settings.ValidationEnabled {
				add(name, "skip", "validation_disabled", "none")
			} else {
				d := validator.Docker{
					Options: validator.DockerOptions{
						Executable: *m.DockerExecutable,
						Socket:     *m.DockerSocket, Image: settings.ValidatorImage,
					},
				}
				probe, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := d.InspectLocal(probe)
				cancel()
				if err != nil {
					add(name, "fail", diagnosticCode(err), "check_local_docker")
				} else {
					add(name, "pass", "ok", "none")
				}
			}
		}
	}
	out.ObservedAt = timestamp()
	return out
}

// diagnoseRunner keeps unknown ownership separate from released ownership.
func diagnoseRunner(r RunnerStatus, native ManagerState, nativeErr error, add func(string, string, string, string)) {
	findings := 0
	emit := func(status, code, action string) {
		findings++
		add("runner", status, code, action)
	}
	held := r.LockHeld != nil && *r.LockHeld
	released := r.LockHeld != nil && !*r.LockHeld
	pending := nativeErr == nil && native.PendingJob != nil && *native.PendingJob
	pendingKnown := nativeErr == nil && (native.PendingJob != nil || !native.Loaded)
	o := r.Observation
	historical := o != nil && r.InstallationMatches != nil && !*r.InstallationMatches
	mismatch := held && (historical ||
		(nativeErr == nil && native.PID != nil && r.OwnerPID != nil && *r.OwnerPID != *native.PID) ||
		(o != nil && r.OwnerPID != nil && o.PID != *r.OwnerPID))
	if mismatch {
		emit("fail", "service_owner_mismatch", "inspect_native_registration")
	}
	if held && (o == nil || o.terminal()) {
		emit("fail", "service_observation_unavailable", "inspect_service_logs")
	}
	if o != nil && !historical {
		if !o.terminal() && r.Stale != nil && *r.Stale {
			emit("fail", "service_observation_stale", "inspect_service_logs")
		}
		if r.ClockAnomaly {
			emit("fail", "service_clock_anomaly", "check_system_clock")
		}
		if o.Phase == "failed" {
			emit("fail", "service_runner_failed", "inspect_service_logs")
		}
		if released && !o.terminal() && pendingKnown && !pending {
			emit("fail", "service_interrupted", "inspect_service_logs")
		}
	}
	if !held && pending {
		emit("warn", "service_starting", "check_status_later")
	}
	if historical {
		emit("warn", "service_historical_observation", "none")
	}
	if nativeErr != nil || (held && native.PID == nil) ||
		(r.LockHeld == nil && o != nil && !pending) ||
		(released && o != nil && !o.terminal() && !pendingKnown) {
		emit("warn", "service_owner_unknown", "open_supported_user_session")
	}
	if findings == 0 {
		code := "ok"
		if !held {
			code = "service_stopped"
		}
		emit("pass", code, "none")
	}
}
