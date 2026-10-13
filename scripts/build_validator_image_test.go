package scripts_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// AC-18: preparation rejects incompatible targets before building, binds the
// endpoint, verifies final image contents, and delivers only an immutable ID.
func TestBuildValidatorImage(t *testing.T) {
	helper := filepath.Join(t.TempDir(), "prepare")
	compile := exec.CommandContext(t.Context(), "go", "build", "-o", helper, "./validator-image")
	output, err := compile.CombinedOutput()
	require.NoError(t, err, string(output))
	repo, err := filepath.Abs("..")
	require.NoError(t, err)
	for _, tc := range []struct {
		name, daemon, arch, stage, host, want string
		args                                  []string
	}{
		{name: "arm64 default", daemon: "aarch64", arch: "arm64"},
		{name: "amd64 default", daemon: "x86_64", arch: "amd64"},
		{name: "arm64 explicit", daemon: "arm64", arch: "arm64", args: []string{"arm64"}},
		{name: "amd64 explicit", daemon: "amd64", arch: "amd64", args: []string{"amd64"}},
		{name: "mismatch", daemon: "arm64", arch: "arm64", args: []string{"amd64"}, want: "platform_unsupported"},
		{name: "unknown daemon", daemon: "riscv64", arch: "arm64", want: "platform_unsupported"},
		{name: "wrong OS", daemon: "arm64", arch: "arm64", stage: "wrong OS", want: "platform_unsupported"},
		{name: "unknown argument", args: []string{"aarch64"}, want: "usage:"},
		{name: "extra argument", args: []string{"arm64", "amd64"}, want: "usage:"},
		{name: "remote endpoint", host: "tcp://remote:2375", want: "local_socket_required"},
		{name: "go failure", daemon: "arm64", arch: "arm64", stage: "go failure", want: "build worker"},
		{name: "docker failure", daemon: "amd64", arch: "amd64", stage: "docker failure", want: "build image"},
		{name: "wrong worker", daemon: "amd64", arch: "arm64", want: "worker architecture mismatch"},
		{name: "wrong image", daemon: "amd64", arch: "amd64", stage: "wrong image", want: "version_mismatch"},
		{name: "wrong package", daemon: "arm64", arch: "arm64", stage: "wrong package", want: "version_mismatch"},
		{name: "wrong bytes", daemon: "arm64", arch: "arm64", stage: "wrong bytes", want: "version_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			scratch := filepath.Join(dir, "scratch with spaces")
			require.NoError(t, os.Mkdir(bin, 0700))
			require.NoError(t, os.Mkdir(scratch, 0700))
			// A minimal ELF header gives the fixture an actual architecture to inspect.
			worker := make([]byte, 64)
			copy(worker, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
			machine := uint16(183)
			if tc.arch == "amd64" {
				machine = 62
			}
			binary.LittleEndian.PutUint16(worker[16:], 2)
			binary.LittleEndian.PutUint16(worker[18:], machine)
			binary.LittleEndian.PutUint32(worker[20:], 1)
			binary.LittleEndian.PutUint16(worker[52:], 64)
			workerPath := filepath.Join(dir, "worker")
			require.NoError(t, os.WriteFile(workerPath, worker, 0600))
			goScript := `#!/bin/sh
[ "$GOOS" = linux ] && [ "$CGO_ENABLED" = 0 ] || exit 8
[ "$1" = build ] && [ "$2" = -trimpath ] && [ "$3" = -o ] || exit 9
printf 'compile\n' >> "$WPC_CALLS"
[ "$WPC_STAGE" != 'go failure' ] || exit 21
cp "$WPC_WORKER" "$4"
`
			dockerScript := `#!/bin/sh
[ -z "${DOCKER_HOST:-}" ] && [ -z "${DOCKER_CONTEXT:-}" ] || exit 10
[ "$1" = --host ] && [ "$2" = unix:///tmp/docker.sock ] || exit 11
shift 2
printf '%s\n' "$1" >> "$WPC_CALLS"
case "$1" in
 info)
  os=linux
  [ "$WPC_STAGE" != 'wrong OS' ] || os=windows
  printf '{"OSType":"%s","Architecture":"%s"}\n' "$os" "$WPC_DAEMON";;
 build)
  [ "$2" = --platform ] && [ "$3" = "linux/$WPC_ARCH" ] || exit 12
  [ "$6" = -t ] && [ "$7" = "wpc-validator:2026.8.2110.0-$WPC_ARCH" ] || exit 13
  [ "$8" = --provenance=false ] && [ -f "$9/wpc-worker" ] && [ -f "$9/Dockerfile" ] || exit 14
  grep -qx 'ARG WARP_VERSION=2026.8.2110.0' "$9/Dockerfile" || exit 16
  grep -qx 'LABEL io.wpc.validator.protocol="2" io.wpc.validator.warp="2026.8.2110.0"' "$9/Dockerfile" || exit 17
  [ "$WPC_STAGE" != 'docker failure' ] || exit 22
  printf '%s\n' 'build diagnostic'
  printf '%s\n' "$WPC_IMAGE" > "$5";;
 image)
  arch="$WPC_ARCH"
  [ "$WPC_STAGE" != 'wrong image' ] || arch=wrong
  printf '[{"Id":"%s","Os":"linux","Architecture":"%s","Config":{"Labels":{
"io.wpc.validator.protocol":"2","io.wpc.validator.warp":"2026.8.2110.0"}}}]\n' "$WPC_IMAGE" "$arch";;
 run)
  version=2026.8.2110.0
  hash="$WPC_HASH"
  [ "$WPC_STAGE" != 'wrong package' ] || version=wrong
  [ "$WPC_STAGE" != 'wrong bytes' ] || hash=wrong
  printf '%s\n%s\n%s  /usr/local/bin/wpc-worker\n' "$version" "$WPC_ARCH" "$hash";;
 *) exit 15;;
esac
`
			require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(goScript), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(bin, "docker"), []byte(dockerScript), 0700))
			host := tc.host
			if host == "" {
				host = "unix:///tmp/docker.sock"
			}
			calls := filepath.Join(dir, "calls")
			cmd := exec.CommandContext(t.Context(), helper, tc.args...)
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "TMPDIR="+scratch,
				"DOCKER_HOST="+host, "DOCKER_CONTEXT=", "WPC_STAGE="+tc.stage, "WPC_DAEMON="+tc.daemon,
				"WPC_ARCH="+tc.arch, "WPC_WORKER="+workerPath, "WPC_HASH="+fmt.Sprintf("%x", sha256.Sum256(worker)),
				"WPC_IMAGE=sha256:"+strings.Repeat("a", 64), "WPC_CALLS="+calls)
			var out, diagnostic bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &diagnostic
			err = cmd.Run()
			if tc.want == "" {
				require.NoError(t, err, diagnostic.String())
				require.Equal(t, "sha256:"+strings.Repeat("a", 64)+"\n", out.String())
				require.Contains(t, diagnostic.String(), "build diagnostic")
			} else {
				require.Error(t, err)
				require.Contains(t, diagnostic.String(), tc.want)
				require.Empty(t, out.String())
			}
			recorded, _ := os.ReadFile(calls)
			if tc.want == "platform_unsupported" || tc.want == "usage:" || tc.want == "local_socket_required" {
				require.NotContains(t, string(recorded), "compile")
				require.NotContains(t, string(recorded), "build")
			}
			entries, err := os.ReadDir(scratch)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}
