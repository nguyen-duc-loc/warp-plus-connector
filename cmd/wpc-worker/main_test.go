package main_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: spec 0003 AC-6, AC-7. Unsupported worker invocations emit only a safe
// protocol error and cannot start the vendor daemon or touch registration state.
func TestWorkerCommand_RejectsUnsupportedInvocation(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "wpc-worker")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	for _, args := range [][]string{nil, {"private-secret"}, {"check", "private-secret"}} {
		cmd := exec.CommandContext(t.Context(), binary, args...)
		cmd.Env = []string{"PATH=" + t.TempDir()}
		var out, diagnostic bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &diagnostic
		err := cmd.Run()
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit)
		require.Equal(t, 1, exit.ExitCode())
		require.Empty(t, out.String())
		var value map[string]string
		require.NoError(t, json.Unmarshal(diagnostic.Bytes(), &value))
		require.Equal(t, map[string]string{"code": "validator_worker_unsupported"}, value)
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		cmd := exec.CommandContext(t.Context(), binary, "initialize")
		cmd.Stdin = strings.NewReader(`{"unknown":"private-secret"}`)
		output, err := cmd.CombinedOutput()
		require.Error(t, err)
		require.JSONEq(t, `{"code":"validation_invalid_input"}`, string(output))
	}
}
