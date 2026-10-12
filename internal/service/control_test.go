package service

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func definitionFixture(t *testing.T, dir, manager string) (Manifest, []byte) {
	t.Helper()
	m := Manifest{
		Manager: manager, ServiceName: "wpc-test", InstallationID: strings.Repeat("a", 32),
		ExecutablePath: "/bin/wpc", DefinitionPath: filepath.Join(dir, "definition"),
		HomeDir: dir, ConfigHome: dir, StateHome: dir, WorkingDir: dir,
	}
	data, err := renderDefinition(m)
	require.NoError(t, err)
	m.DefinitionSHA256 = digest(data)
	return m, data
}

// AC-14: a real short write must leave installation retryable on both targets.
func TestDefinitionShortWriteCanRetry(t *testing.T) {
	if dir := os.Getenv("WPC_DEFINITION_WRITE_TEST"); dir != "" {
		m, _ := definitionFixture(t, dir, os.Getenv("WPC_DEFINITION_MANAGER"))
		// Limit this child only. Ignoring SIGXFSZ makes the kernel return the
		// write error instead of terminating before the publisher handles it.
		signal.Ignore(syscall.SIGXFSZ)
		require.NoError(t, syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 128, Max: 128}))
		require.Error(t, publishDefinition(m))
		return
	}
	for _, manager := range []string{"launchd", "systemd"} {
		t.Run(manager, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.Chmod(dir, 0700))
			m, expected := definitionFixture(t, dir, manager)
			cmd := exec.Command(os.Args[0], "-test.run=^TestDefinitionShortWriteCanRetry$")
			cmd.Env = append(os.Environ(), "WPC_DEFINITION_WRITE_TEST="+dir, "WPC_DEFINITION_MANAGER="+manager)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))
			_, err = os.Lstat(m.DefinitionPath)
			require.ErrorIs(t, err, os.ErrNotExist)
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Empty(t, entries)
			require.NoError(t, publishDefinition(m))
			actual, err := os.ReadFile(m.DefinitionPath)
			require.NoError(t, err)
			require.Equal(t, expected, actual)
		})
	}
}

// AC-4 and AC-14: exact retries adopt a publication without replacing its inode.
func TestDefinitionRetryPreservesPublishedFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	m, expected := definitionFixture(t, dir, "launchd")
	require.NoError(t, publishDefinition(m))
	before, err := os.Stat(m.DefinitionPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), before.Mode().Perm())
	require.NoError(t, publishDefinition(m))
	after, err := os.Stat(m.DefinitionPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	actual, err := os.ReadFile(m.DefinitionPath)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

// AC-14: a retry cannot overwrite a foreign, permissive, or linked native file.
func TestDefinitionRetryRefusesUnsafeDestination(t *testing.T) {
	for _, kind := range []string{"foreign", "partial", "permissive", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.Chmod(dir, 0700))
			m, expected := definitionFixture(t, dir, "systemd")
			original := expected
			switch kind {
			case "foreign":
				original = []byte("foreign definition")
			case "partial":
				original = expected[:128]
			}
			destination := m.DefinitionPath
			if kind == "symlink" {
				destination = filepath.Join(dir, "target")
			}
			require.NoError(t, os.WriteFile(destination, original, 0600))
			if kind == "permissive" {
				require.NoError(t, os.Chmod(destination, 0644))
			}
			if kind == "symlink" {
				require.NoError(t, os.Symlink(destination, m.DefinitionPath))
			}
			before, err := os.Lstat(m.DefinitionPath)
			require.NoError(t, err)
			require.EqualError(t, publishDefinition(m), "service_conflict")
			after, err := os.Lstat(m.DefinitionPath)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after))
			actual, err := os.ReadFile(destination)
			require.NoError(t, err)
			require.Equal(t, original, actual)
		})
	}
}
