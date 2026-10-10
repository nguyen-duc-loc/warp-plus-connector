package validator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/protocol"
	"github.com/stretchr/testify/require"
)

const testImage = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testContainer = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const testOrphan = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

type fakeDocker struct {
	t             *testing.T
	ids           []string
	session       string
	recoveryError error
	creates       int
	checks        int
	removals      int
	input         protocol.Request
	check         func(context.Context) error
}

func (f *fakeDocker) run(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	f.t.Helper()
	require.Equal(f.t, []string{"--host", "unix:///tmp/docker.sock"}, args[:2])
	a := args[2:]
	for _, arg := range a {
		require.NotContains(f.t, arg, "ABCDEFGH-12345678-AbCdEf01")
	}
	encode := func(value any) ([]byte, error) { return json.Marshal(value) }
	switch a[0] {
	case "info":
		return encode(map[string]string{"ID": "daemon", "OSType": "linux", "Architecture": "aarch64"})
	case "image":
		return encode([]any{map[string]any{"Id": testImage, "Os": "linux", "Architecture": "arm64", "Config": map[string]any{"Labels": map[string]string{containerLabel + ".protocol": "1", containerLabel + ".warp": protocol.WARPVersion}}}})
	case "ps":
		return []byte(strings.Join(f.ids, "\n")), nil
	case "create":
		f.creates++
		f.ids = append(f.ids, testContainer)
		for _, arg := range a {
			if strings.HasPrefix(arg, containerLabel+".session=") {
				f.session = strings.TrimPrefix(arg, containerLabel+".session=")
			}
		}
		return []byte(testContainer), nil
	case "container":
		found := false
		for _, id := range f.ids {
			if id == testContainer {
				found = true
			}
		}
		if !found {
			return nil, &Error{Code: "validator_runtime_error"}
		}
		return encode([]any{map[string]any{"Id": testContainer, "Image": testImage, "Config": map[string]any{"Labels": map[string]string{containerLabel: "1", containerLabel + ".session": f.session}}, "State": map[string]bool{"Running": true}, "HostConfig": map[string]any{"NetworkMode": "bridge", "Privileged": false, "PidMode": "", "CapAdd": []string{}}, "Mounts": []any{}}})
	case "start":
		return []byte(testContainer), nil
	case "rm":
		f.removals++
		f.ids = nil
		return nil, nil
	case "exec":
		op := a[len(a)-1]
		receipt := protocol.Receipt{Version: 1, SessionID: f.session, Phase: "cleaned", RegistrationID: "reg", DeviceID: "device", Result: protocol.Result{Outcome: state.Confirmed, Evidence: &state.Evidence{Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: time.Now()}}}
		switch op {
		case "ready", "cancel":
			return []byte(`{}`), nil
		case "initialize":
			require.NoError(f.t, json.Unmarshal(input, &f.input))
			require.Equal(f.t, f.session, f.input.SessionID)
			receipt.Phase = "prepared"
			return encode(receipt)
		case "check":
			f.checks++
			require.Empty(f.t, input)
			if f.check != nil {
				if err := f.check(ctx); err != nil {
					return nil, err
				}
			}
			return encode(receipt)
		case "recover":
			if f.recoveryError != nil {
				return nil, f.recoveryError
			}
			return encode(receipt)
		}
	}
	f.t.Fatalf("unexpected Docker operation %v", a)
	return nil, errors.New("unexpected")
}

func dockerFixture(t *testing.T) (*Docker, *fakeDocker) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	f := &fakeDocker{t: t}
	d := &Docker{Options: DockerOptions{Image: testImage, Socket: "/tmp/docker.sock", StateDir: dir}, run: f.run}
	return d, f
}

func savedManifest(t *testing.T, d *Docker, f *fakeDocker, cleaned bool) {
	t.Helper()
	f.session = "0123456789abcdefghijklmnop"
	m := &manifest{Version: 1, SessionID: f.session, Name: "wpc-validator-" + f.session, ContainerID: testContainer, Endpoint: "unix:///tmp/docker.sock", DaemonID: "daemon", Image: testImage, Submitted: true, Cleaned: cleaned}
	require.NoError(t, d.save(m))
}

// AC-8: input is private, cleanup precedes result delivery, and resources are retired.
func TestDockerCheckCompletesAndRemovesSession(t *testing.T) {
	d, f := dockerFixture(t)
	r, err := d.Check(context.Background(), Input{Key: "ABCDEFGH-12345678-AbCdEf01", StateDir: "/profile", CandidateID: 1, RunID: 2, AttemptID: 3})
	require.NoError(t, err)
	require.Equal(t, state.Confirmed, r.Outcome)
	require.Equal(t, 1, f.creates)
	require.Equal(t, 1, f.checks)
	require.Equal(t, 1, f.removals)
	require.Empty(t, f.ids)
	_, err = os.Stat(d.path())
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Equal(t, "ABCDEFGH-12345678-AbCdEf01", f.input.Key)
}

// AC-7: even a saved cleanup receipt cannot bypass an unrelated unresolved session.
func TestCleanedSessionStillRejectsOrphans(t *testing.T) {
	for _, ownPresent := range []bool{false, true} {
		t.Run(map[bool]string{true: "own_present", false: "own_missing"}[ownPresent], func(t *testing.T) {
			d, f := dockerFixture(t)
			savedManifest(t, d, f, true)
			f.ids = []string{testOrphan}
			if ownPresent {
				f.ids = append(f.ids, testContainer)
			}
			_, err := d.Check(context.Background(), Input{})
			var diagnostic *Error
			require.ErrorAs(t, err, &diagnostic)
			require.Equal(t, "validator_orphaned_session", diagnostic.Code)
			require.Zero(t, f.creates)
			require.Zero(t, f.removals)
		})
	}
}

func TestRecoveryBlocksNewAttachmentWhenUncertain(t *testing.T) {
	d, f := dockerFixture(t)
	savedManifest(t, d, f, false)
	f.ids = []string{testContainer}
	f.recoveryError = &Error{Code: "validation_cleanup_pending"}
	_, err := d.Check(context.Background(), Input{})
	require.Error(t, err)
	require.Zero(t, f.creates)
	require.Zero(t, f.checks)
	require.Zero(t, f.removals)
	_, err = os.Stat(d.path())
	require.NoError(t, err)
}

func TestCancelledHostRetainsSessionForRecovery(t *testing.T) {
	d, f := dockerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	f.check = func(context.Context) error { cancel(); return context.Canceled }
	_, err := d.Check(ctx, Input{Key: "ABCDEFGH-12345678-AbCdEf01"})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, f.removals)
	var m manifest
	require.NoError(t, privatefs.Read(d.path(), &m))
	require.True(t, m.Submitted)
	require.False(t, m.Cleaned)
	f.check = nil
	require.NoError(t, d.Recover(context.Background()))
	require.Equal(t, 1, f.creates)
	require.Equal(t, 1, f.removals)
}

func TestSharedOwnershipAndUnsafeEndpoint(t *testing.T) {
	d, f := dockerFixture(t)
	lock, err := privatefs.Lock(filepath.Join(d.Options.StateDir, "owner.lock"))
	require.NoError(t, err)
	_, err = d.Check(context.Background(), Input{})
	require.Error(t, err)
	require.Zero(t, f.creates)
	require.NoError(t, lock.Close())
	d.Options.Socket = ""
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "tcp://remote:2375")
	_, err = d.Check(context.Background(), Input{})
	var diagnostic *Error
	require.ErrorAs(t, err, &diagnostic)
	require.Equal(t, "validator_local_socket_required", diagnostic.Code)
}

// covers: spec 0003 AC-5, AC-7. Unsafe execution contexts never reach a worker.
func TestDockerCheckRejectsUnsafeRuntimeImageAndContainer(t *testing.T) {
	for _, tc := range []struct {
		name, operation, reason string
		change                  func(map[string]any)
	}{
		{"daemon architecture", "info", "validator_platform_unsupported", func(v map[string]any) { v["Architecture"] = "amd64" }},
		{"image identity", "image", "validator_version_mismatch", func(v map[string]any) { v["Id"] = "sha256:" + testOrphan }},
		{"image platform", "image", "validator_version_mismatch", func(v map[string]any) { v["Os"] = "darwin" }},
		{"image version", "image", "validator_version_mismatch", func(v map[string]any) {
			v["Config"].(map[string]any)["Labels"].(map[string]any)[containerLabel+".warp"] = "unknown"
		}},
		{"foreign container", "container", "validator_identity_mismatch", func(v map[string]any) { v["Id"] = testOrphan }},
		{"foreign session", "container", "validator_identity_mismatch", func(v map[string]any) {
			v["Config"].(map[string]any)["Labels"].(map[string]any)[containerLabel+".session"] = "other"
		}},
		{"host network", "container", "validator_identity_mismatch", func(v map[string]any) { v["HostConfig"].(map[string]any)["NetworkMode"] = "host" }},
		{"privileged", "container", "validator_identity_mismatch", func(v map[string]any) { v["HostConfig"].(map[string]any)["Privileged"] = true }},
		{"host PID", "container", "validator_identity_mismatch", func(v map[string]any) { v["HostConfig"].(map[string]any)["PidMode"] = "host" }},
		{"capabilities", "container", "validator_identity_mismatch", func(v map[string]any) { v["HostConfig"].(map[string]any)["CapAdd"] = []string{"NET_ADMIN"} }},
		{"mounts", "container", "validator_identity_mismatch", func(v map[string]any) { v["Mounts"] = []any{map[string]string{"Source": "/host"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, f := dockerFixture(t)
			d.run = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
				data, err := f.run(ctx, input, args...)
				if err != nil || args[2] != tc.operation {
					return data, err
				}
				if tc.operation == "info" {
					var value map[string]any
					require.NoError(t, json.Unmarshal(data, &value))
					tc.change(value)
					return json.Marshal(value)
				}
				var values []map[string]any
				require.NoError(t, json.Unmarshal(data, &values))
				tc.change(values[0])
				return json.Marshal(values)
			}
			_, err := d.Check(t.Context(), Input{Key: "ABCDEFGH-12345678-AbCdEf01"})
			require.EqualError(t, err, tc.reason)
			require.Zero(t, f.checks)
			require.Empty(t, f.input.Key)
			require.Zero(t, f.removals)
		})
	}
}

// covers: spec 0003 AC-7. Malformed or incomplete receipts retain recovery ownership.
func TestDockerCheckRetainsSessionWhenReceiptDoesNotProveCleanup(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*protocol.Receipt)
		wantError bool
	}{
		{"wrong session", func(r *protocol.Receipt) { r.SessionID = "foreign" }, false},
		{"wrong protocol", func(r *protocol.Receipt) { r.Version++ }, false},
		{"pending cleanup", func(r *protocol.Receipt) { r.Phase = "cleanup_pending" }, false},
		{"missing evidence", func(r *protocol.Receipt) { r.Result.Evidence = nil }, true},
		{"wrong account", func(r *protocol.Receipt) { r.Result.Evidence.AccountType = "Free" }, true},
		{"missing registration", func(r *protocol.Receipt) { r.RegistrationID = "" }, true},
		{"invalid outcome", func(r *protocol.Receipt) { r.Result.Outcome = state.Invalid }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, f := dockerFixture(t)
			d.run = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
				data, err := f.run(ctx, input, args...)
				if err != nil || args[2] != "exec" || args[len(args)-1] != "check" {
					return data, err
				}
				var receipt protocol.Receipt
				require.NoError(t, json.Unmarshal(data, &receipt))
				tc.change(&receipt)
				return json.Marshal(receipt)
			}
			result, err := d.Check(t.Context(), Input{Key: "ABCDEFGH-12345678-AbCdEf01"})
			if tc.wantError {
				require.EqualError(t, err, "validator_protocol_error")
			} else {
				require.NoError(t, err)
				require.Equal(t, state.Inconclusive, result.Outcome)
				require.Equal(t, "validation_cleanup_pending", result.ReasonCode)
			}
			require.Nil(t, result.Evidence)
			require.Zero(t, f.removals)
			var saved manifest
			require.NoError(t, privatefs.Read(d.path(), &saved))
			require.True(t, saved.Submitted)
			require.False(t, saved.Cleaned)
		})
	}
}
