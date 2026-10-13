package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func doctorFilesFixture(t *testing.T) (Account, Manifest, func() DoctorReport) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	a := managedAccountFixture(dir)
	require.NoError(t, os.Mkdir(a.Root, 0700))
	yes, no := true, false
	m := Manifest{
		SchemaVersion: 1, InstallationID: strings.Repeat("a", 32), UID: a.UID,
		Manager: a.Manager, ServiceName: a.Name, InstalledAt: time.Now().UTC().Truncate(time.Microsecond),
		HomeDir: dir, ServiceDir: a.Root, ExecutablePath: os.Args[0], ConfigPath: filepath.Join(dir, "config.json"),
		StateDir: filepath.Join(dir, "state"), ValidatorStateDir: filepath.Join(dir, "warp-plus-connector", "validator"),
		WorkingDir: dir, ConfigHome: dir, StateHome: dir,
		AutostartEnabled: &yes, Operation: "install", Phase: "ready",
	}
	m.DefinitionPath = definitionPath(a, m.ConfigHome)
	data, err := renderDefinition(m)
	require.NoError(t, err)
	m.DefinitionSHA256 = digest(data)
	require.NoError(t, os.MkdirAll(filepath.Dir(m.DefinitionPath), 0700))
	require.NoError(t, os.WriteFile(m.DefinitionPath, data, 0600))
	require.NoError(t, m.save())
	data, err = json.Marshal(map[string]any{"validation_enabled": false, "state_dir": m.StateDir})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(m.ConfigPath, data, 0600))
	archive, err := openArchive(a.Root, m.InstallationID, strings.Repeat("b", 32))
	require.NoError(t, err)
	require.NoError(t, archive.append(Event{Type: "runner_phase", Status: "stopped"}))
	require.NoError(t, archive.close())
	read := func() DoctorReport {
		return doctor(t.Context(), func(context.Context) (Account, error) { return a, nil },
			func(context.Context, Account, *Manifest) (ManagerState, error) {
				return ManagerState{
					Loaded: true, Identity: "verified", AutostartObserved: &yes, Running: &no, PendingJob: &no,
				}, nil
			})
	}
	require.True(t, read().OK, "healthy private files must pass before injecting the fault")
	return a, m, read
}

// Spec 0008 AC-13: permission and identity failures must keep private file guidance,
// while genuine definition drift and corrupt logs keep their specific guidance.
func TestDoctorFileFailureGuidance(t *testing.T) {
	for _, tc := range []struct {
		name, check, fault, code, action string
	}{
		{"definition permissions", "definition", "permissions", "service_access_error", "inspect_private_files"},
		{"definition symlink", "definition", "symlink", "service_access_error", "inspect_private_files"},
		{"definition directory", "definition", "directory", "service_access_error", "inspect_private_files"},
		{"definition too large", "definition", "large", "service_record_invalid", "inspect_private_files"},
		{"definition drift", "definition", "content", "service_definition_changed", "inspect_native_definition"},
		{"definition missing", "definition", "missing", "service_definition_changed", "inspect_native_definition"},
		{"current log permissions", "logs", "permissions", "service_access_error", "inspect_private_files"},
		{"archived log permissions", "logs", "archive", "service_access_error", "inspect_private_files"},
		{"log directory permissions", "logs", "parent", "service_access_error", "inspect_private_files"},
		{"log symlink", "logs", "symlink", "service_access_error", "inspect_private_files"},
		{"log directory entry", "logs", "directory", "service_access_error", "inspect_private_files"},
		{"corrupt log", "logs", "content", "service_log_invalid", "inspect_service_logs"},
		{"oversized log", "logs", "large", "service_log_invalid", "inspect_service_logs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, m, read := doctorFilesFixture(t)
			path := m.DefinitionPath
			if tc.check == "logs" {
				path = filepath.Join(a.Root, "logs", "current.jsonl")
			}
			switch tc.fault {
			case "archive":
				archived := filepath.Join(filepath.Dir(path), "1.jsonl")
				require.NoError(t, os.Rename(path, archived))
				path = archived
				require.NoError(t, os.Chmod(path, 0644))
			case "permissions":
				require.NoError(t, os.Chmod(path, 0644))
			case "parent":
				path = filepath.Dir(path)
				require.NoError(t, os.Chmod(path, 0755))
			case "symlink":
				target := path + ".target"
				require.NoError(t, os.Rename(path, target))
				require.NoError(t, os.Symlink(target, path))
			case "directory":
				require.NoError(t, os.Remove(path))
				require.NoError(t, os.Mkdir(path, 0700))
			case "content":
				require.NoError(t, os.WriteFile(path, []byte("synthetic_secret\n"), 0600))
			case "large":
				file, err := os.OpenFile(path, os.O_WRONLY, 0)
				require.NoError(t, err)
				require.NoError(t, file.Truncate(logLimit+1))
				require.NoError(t, file.Close())
			case "missing":
				require.NoError(t, os.Remove(path))
			}
			before, beforeErr := os.Lstat(path)
			report := read()
			require.False(t, report.OK)
			var matches []Finding
			for _, finding := range report.Checks {
				if finding.Name == tc.check {
					matches = append(matches, finding)
				}
			}
			require.Len(t, matches, 1)
			require.Equal(t, "fail", matches[0].Status)
			require.Equal(t, tc.code, matches[0].Code)
			require.Equal(t, tc.action, matches[0].SuggestedAction)
			data, err := json.Marshal(report)
			require.NoError(t, err)
			require.NotContains(t, string(data), "synthetic_secret")
			after, afterErr := os.Lstat(path)
			if beforeErr != nil {
				require.ErrorIs(t, afterErr, os.ErrNotExist)
			} else {
				require.NoError(t, afterErr)
				require.True(t, os.SameFile(before, after))
				require.Equal(t, before.Mode(), after.Mode())
				require.Equal(t, before.Size(), after.Size())
			}
			if tc.check == "logs" {
				view, err := ReadLogs(t.Context(), a, 100)
				require.EqualError(t, err, tc.code)
				require.Empty(t, view.Entries)
			}
		})
	}
}
