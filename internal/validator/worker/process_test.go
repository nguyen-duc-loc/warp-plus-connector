package worker_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/worker"
	"github.com/stretchr/testify/require"
)

// covers: spec 0003 AC-6, AC-7. The actual process adapter requires an explicit
// mutation acknowledgement and never exposes arbitrary stderr as evidence.
func TestProcessRunner_ClassifiesVendorProcessResponses(t *testing.T) {
	for _, tc := range []struct {
		name, script, code string
		data               string
	}{
		{"success", `printf '%s' '{"status":"Success"}'`, "", `{"status":"Success"}`},
		{"unknown acknowledgement", `printf '%s' '{"status":"Unknown"}'`, "validation_provider_error", ""},
		{"empty object", `printf '%s' '{}'`, "validation_provider_error", ""},
		{"malformed JSON", `printf '%s' 'private-secret'`, "validation_provider_error", ""},
		{"structured failure", `printf '%s' '{"code":"ApiErrorTooManyDevices"}' >&2; exit 1`, "ApiErrorTooManyDevices", ""},
		{"stdout failure", `printf '%s' '{"code":"MissingRegistration"}'; exit 1`, "MissingRegistration", ""},
		{"raw failure", `printf '%s' 'private-secret' >&2; exit 1`, "validation_provider_error", ""},
		{"oversized stdout", `/usr/bin/head -c 262145 /dev/zero`, "validation_provider_error", ""},
		{"oversized stderr", `/usr/bin/head -c 262145 /dev/zero >&2`, "validation_provider_error", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// PATH contains only the fixture, so the installed host WARP is unreachable.
			t.Setenv("PATH", dir)
			script := "#!/bin/sh\n[ \"$1\" = --accept-tos ] && [ \"$2\" = --json ] && [ \"$3\" = registration ] && [ \"$4\" = delete ] || exit 7\n" + tc.script + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, "warp-cli"), []byte(script), 0700))
			reply := (worker.ProcessRunner{}).Run(t.Context(), "registration", "delete")
			require.Equal(t, tc.code, reply.Code)
			require.Equal(t, tc.data, string(reply.Data))
		})
	}
}

// covers: spec 0003 AC-6. Cancellation never starts the vendor process.
func TestProcessRunner_CancellationPreventsExecution(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	marker := filepath.Join(dir, "called")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "warp-cli"), []byte("#!/bin/sh\n: > '"+marker+"'\n"), 0700))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reply := (worker.ProcessRunner{}).Run(ctx, "registration", "new")
	require.Equal(t, "validation_timeout", reply.Code)
	_, err := os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist)
}
