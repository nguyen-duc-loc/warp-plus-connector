package scripts_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// covers: spec 0003 AC-7. The build wrapper uses Linux arm64, returns only the
// immutable image ID, and cleans temporary artifacts on success and failure.
// External Go and Docker executables are fixtures; actual image compatibility
// remains covered by the recorded live verification.
func TestBuildValidatorImage_PropagatesFailuresAndCleansArtifacts(t *testing.T) {
	script, err := filepath.Abs("build-validator-image.sh")
	require.NoError(t, err)
	for _, stage := range []string{"success", "go failure", "docker failure"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			scratch := filepath.Join(dir, "scratch with spaces")
			require.NoError(t, os.Mkdir(bin, 0700))
			require.NoError(t, os.Mkdir(scratch, 0700))
			goScript := `#!/bin/sh
[ "$GOOS" = linux ] && [ "$GOARCH" = arm64 ] && [ "$CGO_ENABLED" = 0 ] || exit 8
[ "$1" = build ] && [ "$2" = -trimpath ] && [ "$3" = -o ] && [ "$5" = ./cmd/wpc-worker ] || exit 9
[ "$WPC_FIXTURE_STAGE" != 'go failure' ] || exit 21
printf '%s' 'fixture worker' > "$4"
`
			dockerScript := `#!/bin/sh
printf '%s' called > "$WPC_FIXTURE_CALLED"
[ "$1" = build ] && [ "$2" = --platform ] && [ "$3" = linux/arm64 ] && [ "$4" = --iidfile ] || exit 10
[ "$6" = -t ] && [ "$7" = wpc-validator:2026.8.2100.0 ] || exit 11
[ -f "$8/wpc-worker" ] && [ -f "$8/Dockerfile" ] || exit 12
[ "$WPC_FIXTURE_STAGE" != 'docker failure' ] || exit 22
printf '%s\n' 'build diagnostic'
printf '%s\n' 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' > "$5"
`
			require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(goScript), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(bin, "docker"), []byte(dockerScript), 0700))
			marker := filepath.Join(dir, "docker-called")
			cmd := exec.CommandContext(t.Context(), "/bin/sh", script)
			cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "TMPDIR="+scratch, "WPC_FIXTURE_STAGE="+stage, "WPC_FIXTURE_CALLED="+marker)
			var out, diagnostic bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &diagnostic
			err := cmd.Run()
			if stage == "success" {
				require.NoError(t, err, diagnostic.String())
				require.Equal(t, "sha256:"+strings.Repeat("a", 64)+"\n", out.String())
				require.Contains(t, diagnostic.String(), "build diagnostic")
			} else {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit)
				want := 21
				if stage == "docker failure" {
					want = 22
				}
				require.Equal(t, want, exit.ExitCode())
				require.Empty(t, out.String())
			}
			if stage == "go failure" {
				_, err = os.Stat(marker)
				require.ErrorIs(t, err, os.ErrNotExist)
			}
			entries, err := os.ReadDir(scratch)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}
