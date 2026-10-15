package terminal_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/terminal"
	"github.com/stretchr/testify/require"
)

// Spec 0010 AC-3, AC-12: asset admission uses the real executable and rejects unsafe bundles.
// Cancellation after admission proves the bundle passed without executing the fixture runtime.
func TestRun_BundleAdmission(t *testing.T) {
	if want := os.Getenv("WPC_TEST_BUNDLE_RESULT"); want != "" {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := terminal.Run(ctx, nil, nil, terminal.Options{Release: "dev"})
		if want == "accepted" {
			require.ErrorIs(t, err, context.Canceled)
		} else {
			require.EqualError(t, err, want)
		}
		return
	}
	dir := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	input, err := os.Open(executable)
	require.NoError(t, err)
	binary := filepath.Join(dir, "wpc")
	output, err := os.OpenFile(binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	require.NoError(t, err)
	_, err = io.Copy(output, input)
	require.NoError(t, err)
	require.NoError(t, input.Close())
	require.NoError(t, output.Close())
	launcher := filepath.Join(dir, "launcher")
	require.NoError(t, os.Symlink(binary, launcher))
	root := filepath.Join(dir, "lib", "wpc-ui")
	for _, mode := range []string{
		"valid symlink launcher", "missing asset", "changed payload", "wrong release", "wrong architecture",
		"wrong lock", "unlisted module", "symlink asset", "path escape", "unknown field", "null commit",
	} {
		t.Run(mode, func(t *testing.T) {
			require.NoError(t, os.RemoveAll(root))
			files := map[string]string{}
			for _, name := range []string{"node/bin/node", "app/package.json", "app/dist/main.js", "app/package-lock.json"} {
				path := filepath.Join(root, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
				data := []byte("fixture " + name)
				require.NoError(t, os.WriteFile(path, data, 0600))
				hash := sha256.Sum256(data)
				files[name] = hex.EncodeToString(hash[:])
			}
			manifest := map[string]any{
				"schema_version": 1, "protocol_version": 1, "release": "dev", "commit": "",
				"os": runtime.GOOS, "arch": runtime.GOARCH, "node": "24.20.0",
				"lock_sha256": files["app/package-lock.json"], "files": files,
			}
			want := "ui_installation_invalid"
			entry := filepath.Join(root, "app", "dist", "main.js")
			switch mode {
			case "valid symlink launcher":
				want = "accepted"
			case "missing asset":
				require.NoError(t, os.Remove(entry))
			case "changed payload":
				require.NoError(t, os.WriteFile(entry, []byte("private tampered payload"), 0600))
			case "wrong release":
				manifest["release"], want = "other", "ui_version_mismatch"
			case "wrong architecture":
				manifest["arch"], want = "other", "ui_version_mismatch"
			case "wrong lock":
				manifest["lock_sha256"] = files["app/dist/main.js"]
			case "unlisted module":
				require.NoError(t, os.WriteFile(filepath.Join(root, "app", "injected.js"), []byte("private"), 0600))
			case "symlink asset":
				require.NoError(t, os.Rename(entry, entry+".target"))
				require.NoError(t, os.Symlink(entry+".target", entry))
			case "path escape":
				files["../outside"] = files["app/dist/main.js"]
			case "unknown field":
				manifest["private"], want = "not allowed", "ui_version_mismatch"
			case "null commit":
				manifest["commit"], want = nil, "ui_version_mismatch"
			}
			data, err := json.Marshal(manifest)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(root, "manifest.json"), data, 0600))
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, launcher, "-test.run=^TestRun_BundleAdmission$")
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "WPC_TEST_BUNDLE_RESULT="+want)
			result, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", result)
		})
	}
}
