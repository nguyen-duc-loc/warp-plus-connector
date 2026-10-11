package service

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func observationFixture() Observation {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return Observation{
		SchemaVersion: 1, InstallationID: strings.Repeat("a", 32), InvocationID: strings.Repeat("b", 32),
		PID: os.Getpid(), StartedAt: now, UpdatedAt: now, Phase: "starting", CrawlPhase: "unknown",
		ValidationPhase: "unknown",
	}
}

// Spec 0008 AC-8 and AC-14: ambiguous or unsafe records never authorize an operation.
func TestObservationRejectsMissingDuplicateAndUnsafeFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	path := filepath.Join(dir, "observation.json")
	o := observationFixture()
	require.NoError(t, writeRecord(path, o))
	_, err := readObservation(dir)
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	for name, invalid := range map[string][]byte{
		"missing": bytes.Replace(data, []byte(`"crawl_enabled":null,`), nil, 1),
		"duplicate": bytes.Replace(
			data,
			[]byte(`"schema_version":1`),
			[]byte(`"schema_version":1,"schema_version":1`),
			1,
		),
		"unknown":    bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":1,"extra":0`), 1),
		"case_alias": bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":1,"SCHEMA_VERSION":1`), 1),
		"null":       bytes.Replace(data, []byte(`"pid":`), []byte(`"pid":null,"ignored_pid":`), 1),
	} {
		t.Run(
			name, func(t *testing.T) {
				require.NoError(t, os.WriteFile(path, invalid, 0600))
				_, err := readObservation(dir)
				require.Error(t, err)
			},
		)
	}
	require.NoError(t, os.WriteFile(path, data, 0600))
	require.NoError(t, os.Chmod(path, 0644))
	_, err = readObservation(dir)
	require.Error(t, err)
	require.NoError(t, os.Remove(path))
	other := filepath.Join(dir, "other.json")
	require.NoError(t, os.WriteFile(other, data, 0600))
	require.NoError(t, os.Symlink(other, path))
	_, err = readObservation(dir)
	require.Error(t, err)
}

// Spec 0008 AC-6: an independent process sees the kernel record lock owner.
func TestRunnerLockReportsOwnerAcrossProcess(t *testing.T) {
	if path := os.Getenv("WPC_SERVICE_LOCK_TEST"); path != "" {
		pid, err := ownerPID(path)
		if err != nil || pid == nil || *pid == 0 {
			os.Exit(2)
		}
		f, err := runnerLock(path)
		if err == nil {
			_ = f.Close()
			os.Exit(3)
		}
		data, _ := json.Marshal(*pid)
		_, _ = os.Stdout.Write(data)
		os.Exit(0)
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	path := filepath.Join(dir, "runner.lock")
	f, err := runnerLock(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerLockReportsOwnerAcrossProcess$")
	cmd.Env = append(os.Environ(), "WPC_SERVICE_LOCK_TEST="+path)
	data, err := cmd.Output()
	require.NoError(t, err)
	var pid int
	require.NoError(t, json.Unmarshal(data, &pid))
	require.Equal(t, os.Getpid(), pid)
}
