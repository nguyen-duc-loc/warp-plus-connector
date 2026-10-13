package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func doctorContractRead(ctx context.Context, a Account, native ManagerState, nativeErr error) DoctorReport {
	return doctor(ctx, func(context.Context) (Account, error) { return a, nil },
		func(context.Context, Account, *Manifest) (ManagerState, error) { return native, nativeErr })
}

func doctorContractFinding(t *testing.T, report DoctorReport, name, status, code, action string) {
	t.Helper()
	for _, finding := range report.Checks {
		if finding.Name == name && finding.Code == code {
			require.Equal(t, status, finding.Status)
			require.Equal(t, action, finding.SuggestedAction)
			return
		}
	}
	t.Fatalf("missing %s %s: %+v", name, code, report.Checks)
}

// The adapter is a subprocess that permits only the two local inspection commands.
// It cannot create containers or contact WARP, and its output is never report text.
func doctorDockerFixture(t *testing.T, daemon, image, delay string) (Account, Manifest, string) {
	t.Helper()
	a, m, _ := doctorFilesFixture(t)
	path := filepath.Join(m.HomeDir, "docker")
	trace := filepath.Join(m.HomeDir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + trace + "'\n" +
		"case \"$3 $4\" in\n'info --format') " + delay + "\nprintf '%s\\n' '" + daemon + "';;\n" +
		"'image inspect') printf '%s\\n' '" + image + "';;\n*) exit 99;;\nesac\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0700))
	socket := filepath.Join(m.HomeDir, "docker.sock")
	m.DockerExecutable, m.DockerSocket = &path, &socket
	require.NoError(t, m.save())
	settings, err := json.Marshal(map[string]any{
		"state_dir": m.StateDir, "validator": "docker", "validation_enabled": true,
		"validator_socket": socket, "validator_image": "sha256:" + strings.Repeat("a", 64),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(m.ConfigPath, settings, 0600))
	return a, m, trace
}

func doctorImageFixture(arch string) string {
	return `[{"Id":"sha256:` + strings.Repeat("a", 64) + `","Architecture":"` + arch +
		`","Os":"linux","Config":{"Labels":{"io.wpc.validator.protocol":"2",` +
		`"io.wpc.validator.warp":"2026.8.2100.0"}}}]`
}

// Spec 0008 AC-13, AC-19: effective platform and immutable worker checks are
// exercised through the complete doctor report and real subprocess boundary.
func TestDoctorDockerContract(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("local validator inspection requires Linux")
	}
	arch, alias, other := runtime.GOARCH, "aarch64", "amd64"
	if arch == "amd64" {
		alias, other = "x86_64", "arm64"
	}
	daemon := `{"ID":"fixture","OSType":"linux","Architecture":"` + arch + `"}`
	image := doctorImageFixture(arch)
	for _, tc := range []struct{ name, daemon, image, code string }{
		{"matching", daemon, image, "ok"},
		{"daemon alias", strings.ReplaceAll(daemon, arch, alias), image, "ok"},
		{"other daemon architecture", strings.ReplaceAll(daemon, arch, other), image, "validator_platform_unsupported"},
		{"unknown daemon architecture", strings.ReplaceAll(daemon, arch, "unknown"), image, "validator_platform_unsupported"},
		{"wrong daemon OS", strings.ReplaceAll(daemon, "linux", "windows"), image, "validator_platform_unsupported"},
		{"malformed daemon", "SYNTHETIC_SECRET", image, "validator_platform_unsupported"},
		{"missing daemon identity", strings.ReplaceAll(daemon, "fixture", ""), image, "validator_platform_unsupported"},
		{"missing image", daemon, "[]", "validator_image_unavailable"},
		{"malformed image", daemon, "SYNTHETIC_SECRET", "validator_image_unavailable"},
		{"other image architecture", daemon, doctorImageFixture(other), "validator_version_mismatch"},
		{"image alias rejected", daemon, doctorImageFixture(alias), "validator_version_mismatch"},
		{"wrong image OS", daemon, strings.ReplaceAll(image, "linux", "windows"), "validator_version_mismatch"},
		{"wrong image identity", daemon, strings.ReplaceAll(image, strings.Repeat("a", 64),
			strings.Repeat("b", 64)), "validator_version_mismatch"},
		{"old worker protocol", daemon, strings.ReplaceAll(image, `protocol":"2`, `protocol":"1`),
			"validator_version_mismatch"},
		{"wrong WARP version", daemon, strings.ReplaceAll(image, "2026.8.2100.0", "other"),
			"validator_version_mismatch"},
		{"missing labels", daemon, strings.ReplaceAll(image, "io.wpc.validator", "unrelated"),
			"validator_version_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, m, trace := doctorDockerFixture(t, tc.daemon, tc.image, "")
			report := doctorContractRead(t.Context(), a, ManagerState{}, nil)
			status, action := "fail", "check_local_docker"
			if tc.code == "ok" {
				status, action = "pass", "none"
			}
			doctorContractFinding(t, report, "docker", status, tc.code, action)
			require.Equal(t, tc.code == "ok", report.OK)
			data, err := json.Marshal(report)
			require.NoError(t, err)
			require.NotContains(t, string(data), "SYNTHETIC_SECRET")
			calls, err := os.ReadFile(trace)
			require.NoError(t, err)
			for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
				require.True(t, strings.HasSuffix(line, "info --format {{json .}}") ||
					strings.HasSuffix(line, "image inspect sha256:"+strings.Repeat("a", 64)), line)
			}
			_, err = os.Stat(m.StateDir)
			require.ErrorIs(t, err, os.ErrNotExist)
			_, err = os.Stat(m.ValidatorStateDir)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

// Expiry during the final probe still has the shared timeout action, rather
// than being mistaken for a Docker configuration failure.
// AC-13: shared deadline expiry retains retry guidance during the final probe.
func TestDoctorSharedBudgetDuringDocker(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("local validator inspection requires Linux")
	}
	a, _, _ := doctorDockerFixture(t, "{}", "[]", "exec sleep 30")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	report := doctorContractRead(ctx, a, ManagerState{}, nil)
	require.Less(t, time.Since(start), 2*time.Second)
	doctorContractFinding(t, report, "docker", "fail", "service_diagnostic_timeout", "retry_doctor")
}

// AC-13: an individual probe deadline keeps its local Docker guidance.
func TestDoctorIndividualDockerBudget(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("local validator inspection requires Linux")
	}
	a, _, _ := doctorDockerFixture(t, "{}", "[]", "exec sleep 30")
	start := time.Now()
	report := doctorContractRead(t.Context(), a, ManagerState{}, nil)
	require.GreaterOrEqual(t, time.Since(start), 5*time.Second)
	require.Less(t, time.Since(start), 7*time.Second)
	doctorContractFinding(t, report, "docker", "fail", "service_diagnostic_timeout", "check_local_docker")
}

// AC-13: shared expiry leaves a complete report with no further probes.
func TestDoctorSharedBudgetAfterNative(t *testing.T) {
	a, _, _ := doctorFilesFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	report := doctor(ctx, func(context.Context) (Account, error) { return a, nil },
		func(ctx context.Context, _ Account, _ *Manifest) (ManagerState, error) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.InDelta(t, 20, time.Until(deadline).Seconds(), 0.5)
			cancel()
			return ManagerState{}, ctx.Err()
		})
	require.Len(t, report.Checks, 9)
	for _, finding := range report.Checks[1:] {
		require.Equal(t, "service_diagnostic_timeout", finding.Code)
		require.Equal(t, "retry_doctor", finding.SuggestedAction)
	}
}

// AC-8, AC-10 and AC-13: unknown ownership cannot hide validated saved failures.
func TestDoctorUnknownOwnerRetainsSavedFailure(t *testing.T) {
	a, m, read := doctorFilesFixture(t)
	o := observationFixture()
	o.InstallationID = m.InstallationID
	o.Phase = "failed"
	o.UpdatedAt = timestamp()
	o.StartedAt = o.UpdatedAt.Add(-time.Minute)
	o.StoppedAt = &o.UpdatedAt
	code, exit := "service_config_invalid", 1
	o.FailureCode, o.AppExitCode = &code, &exit
	require.NoError(t, writeRecord(filepath.Join(a.Root, "observation.json"), o))
	require.NoError(t, os.WriteFile(filepath.Join(a.Root, "runner.lock"), nil, 0644))
	report := read()
	doctorContractFinding(t, report, "runner", "warn", "service_owner_unknown", "open_supported_user_session")
	doctorContractFinding(t, report, "runner", "fail", "service_runner_failed", "inspect_service_logs")
	require.False(t, report.OK)
	// Status still exposes unavailable ownership as unavailable with null data.
	component := runnerStatus(a, &m)
	require.Equal(t, "unavailable", component.Availability)
	require.Nil(t, component.Data)
	// An older installation cannot make the current one fail.
	o.InstallationID = strings.Repeat("c", 32)
	require.NoError(t, writeRecord(filepath.Join(a.Root, "observation.json"), o))
	report = read()
	doctorContractFinding(t, report, "runner", "warn", "service_historical_observation", "none")
	require.True(t, report.OK)
	// Invalid records are rejected before any of their claims become evidence.
	o.SchemaVersion = 2
	require.NoError(t, writeRecord(filepath.Join(a.Root, "observation.json"), o))
	report = read()
	doctorContractFinding(t, report, "runner", "fail", "service_record_invalid", "inspect_private_files")
	for _, finding := range report.Checks {
		require.NotEqual(t, "service_runner_failed", finding.Code)
	}
}

// These report level cases complement live native lifecycle evidence without
// accessing the account's actual fixed native service registration.
// AC-11, AC-13 and AC-14: prerequisite failures retain precise safe actions.
func TestDoctorPrerequisiteContract(t *testing.T) {
	for _, tc := range []struct{ name, check, status, code, action string }{
		{"no manifest", "installation", "warn", "service_not_installed", "install_service"},
		{"native conflict", "definition", "fail", "service_identity_unverified", "inspect_native_registration"},
		{"incomplete manifest", "installation", "fail", "service_operation_incomplete", "repeat_pending_operation"},
		{"missing config", "configuration", "fail", "service_config_invalid", "fix_configuration"},
		{"invalid config", "configuration", "fail", "service_config_invalid", "fix_configuration"},
		{"state binding", "configuration", "fail", "service_binding_changed", "reinstall_service"},
		{"unbound Docker", "configuration", "fail", "service_binding_changed", "reinstall_service"},
		{"manager unavailable", "manager", "fail", "service_manager_unavailable", "open_supported_user_session"},
		{"enablement unknown", "manager", "warn", "service_enablement_unknown", "inspect_native_registration"},
		{"enablement drift", "manager", "fail", "service_enablement_changed", "repeat_startup_intent"},
		{"native identity", "definition", "fail", "service_definition_changed", "inspect_native_definition"},
		{"missing state", "state", "warn", "state_not_initialized", "start_service"},
		{"disabled Docker", "docker", "skip", "validation_disabled", "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, m, _ := doctorFilesFixture(t)
			yes, no := true, false
			native := ManagerState{Loaded: true, Identity: "verified", AutostartObserved: &yes, PendingJob: &no}
			var nativeErr error
			switch tc.name {
			case "no manifest", "native conflict":
				require.NoError(t, os.Remove(filepath.Join(a.Root, "installation.json")))
				native.Loaded = tc.name == "native conflict"
			case "incomplete manifest":
				m.Operation, m.Phase = "disable", "prepared"
				require.NoError(t, m.save())
			case "missing config":
				require.NoError(t, os.Remove(m.ConfigPath))
			case "invalid config":
				require.NoError(t, os.WriteFile(m.ConfigPath, []byte("SYNTHETIC_SECRET"), 0600))
			case "state binding", "unbound Docker":
				settings := map[string]any{"state_dir": m.StateDir, "validation_enabled": false}
				if tc.name == "state binding" {
					settings["state_dir"] = filepath.Join(m.HomeDir, "other-state")
				} else {
					settings["validation_enabled"], settings["validator"] = true, "docker"
					settings["validator_image"] = "sha256:" + strings.Repeat("a", 64)
				}
				data, err := json.Marshal(settings)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(m.ConfigPath, data, 0600))
			case "manager unavailable":
				nativeErr = failure("service_manager_unavailable")
			case "enablement unknown":
				native.AutostartObserved = nil
			case "enablement drift":
				native.AutostartObserved = &no
			case "native identity":
				native.Identity = "unverified"
			}
			report := doctorContractRead(t.Context(), a, native, nativeErr)
			doctorContractFinding(t, report, tc.check, tc.status, tc.code, tc.action)
			require.Equal(t, tc.status != "fail", report.OK)
			if tc.check == "configuration" || tc.name == "no manifest" || tc.name == "native conflict" {
				doctorContractFinding(t, report, "docker", "skip", "prerequisite_unavailable", "resolve_prerequisite")
			}
			data, err := json.Marshal(report)
			require.NoError(t, err)
			require.NotContains(t, string(data), "SYNTHETIC_SECRET")
			_, err = os.Stat(m.StateDir)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

// AC-13: one native timeout does not suppress independent diagnostic checks.
func TestDoctorNativeCallBudget(t *testing.T) {
	a, _, _ := doctorFilesFixture(t)
	start := time.Now()
	report := doctor(t.Context(), func(context.Context) (Account, error) { return a, nil },
		func(ctx context.Context, _ Account, _ *Manifest) (ManagerState, error) {
			_, err := command(ctx, 5*time.Second, "/bin/sh", "-c", "exec sleep 30")
			return ManagerState{}, err
		})
	require.GreaterOrEqual(t, time.Since(start), 5*time.Second)
	require.Less(t, time.Since(start), 7*time.Second)
	doctorContractFinding(t, report, "manager", "fail", "service_manager_unavailable", "open_supported_user_session")
	doctorContractFinding(t, report, "logs", "pass", "ok", "none")
}

// AC-13: all remaining checks share the same twenty second report budget.
func TestDoctorSharedTwentySecondBudget(t *testing.T) {
	a, _, _ := doctorFilesFixture(t)
	start := time.Now()
	report := doctor(t.Context(), func(context.Context) (Account, error) { return a, nil },
		func(ctx context.Context, _ Account, _ *Manifest) (ManagerState, error) {
			<-ctx.Done()
			return ManagerState{}, ctx.Err()
		})
	require.GreaterOrEqual(t, time.Since(start), 20*time.Second)
	require.Less(t, time.Since(start), 22*time.Second)
	require.Len(t, report.Checks, 9)
	for _, finding := range report.Checks[1:] {
		require.Equal(t, "service_diagnostic_timeout", finding.Code)
		require.Equal(t, "retry_doctor", finding.SuggestedAction)
	}
}

// AC-10, AC-13 and AC-14: damaged state is diagnosed without repair or disclosure.
func TestDoctorDamagedStateContract(t *testing.T) {
	for _, tc := range []struct{ name, query, code string }{
		{"damaged", "", "state_error"},
		{"newer schema", "PRAGMA user_version=99", "state_schema_too_new"},
		{"old schema", "PRAGMA user_version=1", "state_migration_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, m, read := doctorFilesFixture(t)
			require.NoError(t, os.Mkdir(m.StateDir, 0700))
			path := filepath.Join(m.StateDir, "state.db")
			require.NoError(t, os.WriteFile(path, nil, 0600))
			if tc.query == "" {
				require.NoError(t, os.WriteFile(path, []byte("SYNTHETIC_SECRET"), 0600))
			} else {
				db, err := sql.Open("sqlite", path)
				require.NoError(t, err)
				_, err = db.ExecContext(t.Context(), tc.query)
				require.NoError(t, err)
				require.NoError(t, db.Close())
			}
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			report := read()
			doctorContractFinding(t, report, "state", "fail", tc.code, "inspect_saved_state")
			require.False(t, report.OK)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
			data, err := json.Marshal(report)
			require.NoError(t, err)
			require.NotContains(t, string(data), "SYNTHETIC_SECRET")
		})
	}
}
