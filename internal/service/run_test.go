package service

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func managedAccountFixture(dir string) Account {
	return Account{
		UID: os.Geteuid(), Home: dir, Root: filepath.Join(dir, "service"),
		Manager: "systemd", Name: "warp-plus-connector.service",
	}
}

// AC-5 and AC-14: failure before the first log acknowledgement still saves a
// terminal observation through the independent working sink, without work.
func TestManagedStartupLogFailureSavesObservation(t *testing.T) {
	if dir := os.Getenv("WPC_STARTUP_LOG_TEST"); dir != "" {
		a := managedAccountFixture(dir)
		m, err := a.manifest()
		require.NoError(t, err)
		signal.Ignore(syscall.SIGXFSZ)
		require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 1024, Max: 1024}))
		var stopping atomic.Bool
		ctx := WithLifecycle(t.Context(), nil, func() { stopping.Store(true) }, nil)
		err = runManaged(ctx, m.InstallationID, "test", func(context.Context) (Account, error) { return a, nil })
		require.Error(t, err)
		require.True(t, stopping.Load())
		return
	}
	for _, fault := range []string{"open", "first append"} {
		t.Run(fault, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			a := managedAccountFixture(dir)
			require.NoError(t, os.Mkdir(a.Root, 0700))
			enabled := true
			m := Manifest{
				SchemaVersion: 1, InstallationID: strings.Repeat("a", 32), UID: a.UID,
				Manager: a.Manager, ServiceName: a.Name, InstalledAt: time.Now().UTC().Truncate(time.Microsecond),
				HomeDir: dir, ServiceDir: a.Root, ExecutablePath: os.Args[0], ConfigPath: filepath.Join(dir, "config.json"),
				StateDir: filepath.Join(dir, "state"), ValidatorStateDir: filepath.Join(dir, "validator"),
				WorkingDir: dir, ConfigHome: dir, StateHome: dir, DefinitionSHA256: strings.Repeat("b", 64),
				AutostartEnabled: &enabled, Operation: "install", Phase: "ready",
			}
			m.DefinitionPath = definitionPath(a, m.ConfigHome)
			require.NoError(t, m.save())
			archive, err := openArchive(a.Root, m.InstallationID, strings.Repeat("c", 32))
			require.NoError(t, err)
			require.NoError(t, archive.append(Event{Type: "runner_phase", Status: "stopped"}))
			require.NoError(t, archive.close())
			path := filepath.Join(a.Root, "logs", "current.jsonl")
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			if fault == "open" {
				data = []byte("{malformed}\n")
			} else {
				// Fill the file limit with one complete record. Observations are
				// smaller and remain writable when the next log append fails.
				data = append(data[:len(data)-1], []byte(strings.Repeat(" ", 1024-len(data))+"\n")...)
			}
			require.NoError(t, os.WriteFile(path, data, 0600))
			cmd := exec.Command(os.Args[0], "-test.run=^TestManagedStartupLogFailureSavesObservation$")
			cmd.Env = append(os.Environ(), "WPC_STARTUP_LOG_TEST="+dir)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))
			observation, err := readObservation(a.Root)
			require.NoError(t, err)
			require.Equal(t, "failed", observation.Phase)
			require.Equal(t, "service_log_failed", *observation.FailureCode)
			require.Equal(t, 1, *observation.AppExitCode)
			require.NotNil(t, observation.StoppedAt)
			_, err = os.Stat(m.StateDir)
			require.ErrorIs(t, err, os.ErrNotExist)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, data, after, "failed log sink must not be retried")
		})
	}
}
