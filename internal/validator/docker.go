package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/protocol"
)

const containerLabel = "io.wpc.validator"

// Error contains a stable diagnostic, never subprocess output or credentials.
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

// DockerOptions selects the immutable image and private global recovery directory.
type DockerOptions struct {
	Image    string
	Socket   string
	StateDir string
	// Executable is the optional absolute Docker path bound by a service install.
	Executable string
}

// Docker checks one candidate at a time with a retained isolated container.
type Docker struct {
	probe        bool
	mutation     func(func() error) error
	Options      DockerOptions
	run          func(context.Context, []byte, ...string) ([]byte, error)
	platform     func() (string, string)
	writeJournal func(string, any) error
}

type manifest struct {
	Version     int               `json:"version"`
	SessionID   string            `json:"session_id"`
	Name        string            `json:"name"`
	ContainerID string            `json:"container_id"`
	Endpoint    string            `json:"endpoint"`
	DaemonID    string            `json:"daemon_id"`
	Image       string            `json:"image"`
	Profile     string            `json:"profile"`
	CandidateID int64             `json:"candidate_id"`
	RunID       int64             `json:"run_id"`
	AttemptID   int64             `json:"attempt_id"`
	CreatedAt   time.Time         `json:"created_at"`
	Cleaned     bool              `json:"cleaned"`
	Submitted   bool              `json:"submitted"`
	Operation   string            `json:"operation,omitempty"`
	Receipt     *protocol.Receipt `json:"receipt,omitempty"`
}

type containerInfo struct {
	ID         string `json:"Id"`
	Image      string
	Config     struct{ Labels map[string]string }
	State      struct{ Running bool }
	HostConfig struct {
		NetworkMode string
		Privileged  bool
		PidMode     string
		CapAdd      []string
	}
	Mounts []json.RawMessage
}

var imagePattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var idPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9]{16,64}$`)

// ValidImage checks the immutable local image reference accepted by this backend.
func ValidImage(image string) bool { return imagePattern.MatchString(image) }

// Identity records both tested vendor version and immutable local image identity.
func (d *Docker) Identity() (string, string) {
	return "docker-warp", protocol.WARPVersion + "@" + d.Options.Image
}

func (d *Docker) command(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	if d.run != nil {
		return d.run(ctx, input, args...)
	}
	if d.Options.Executable != "" {
		return dockerCommandAtLimit(ctx, d.Options.Executable, input, d.probe, args...)
	}
	return dockerCommand(ctx, input, args...)
}

type limitedOutput struct {
	bytes.Buffer
	overflow bool
	shared   *atomic.Int64
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2*1024*1024 || (b.shared != nil && b.shared.Add(int64(len(p))) > 1024*1024) {
		b.overflow = true
		return 0, errors.New("output limit")
	}
	return b.Buffer.Write(p)
}

func dockerCommand(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	return dockerCommandAt(ctx, "docker", input, args...)
}

func dockerCommandAt(ctx context.Context, executable string, input []byte, args ...string) ([]byte, error) {
	return dockerCommandAtLimit(ctx, executable, input, false, args...)
}

func dockerCommandAtLimit(ctx context.Context, executable string, input []byte, probe bool, args ...string) (
	[]byte,
	error,
) {
	if probe {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		ctx = bounded
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.WaitDelay = 2 * time.Second
	// Endpoint selection is explicit after inspection; environment cannot redirect it.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "DOCKER_HOST=") && !strings.HasPrefix(entry, "DOCKER_CONTEXT=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Stdin = bytes.NewReader(input)
	var out, diagnostic limitedOutput
	if probe {
		shared := &atomic.Int64{}
		out.shared, diagnostic.shared = shared, shared
	}
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if out.overflow || diagnostic.overflow {
		return nil, &Error{Code: "validator_output_error"}
	}
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, &Error{Code: "validator_permission_error"}
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, &Error{Code: "validator_backend_unavailable"}
		}
		var code struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(diagnostic.Bytes(), &code) == nil {
			switch code.Code {
			case "validator_busy", "validation_cleanup_pending", "validator_state_error",
				"validator_not_ready", "validator_version_mismatch":
				return nil, &Error{Code: code.Code}
			}
		}
		return nil, &Error{Code: "validator_runtime_error"}
	}
	return out.Bytes(), nil
}

// LocalSocket resolves the local endpoint without contacting the daemon.
func (d *Docker) LocalSocket(ctx context.Context) (string, error) {
	endpoint, err := d.endpoint(ctx)
	return strings.TrimPrefix(endpoint, "unix://"), err
}

// InspectLocal checks the daemon and immutable image without acquiring ownership.
func (d *Docker) InspectLocal(ctx context.Context) error {
	copy := *d
	d = &copy
	d.probe = true
	endpoint, err := d.endpoint(ctx)
	if err != nil {
		return err
	}
	if _, err := d.runtime(ctx, endpoint); err != nil {
		return err
	}
	return d.image(ctx, endpoint)
}

func (d *Docker) endpoint(ctx context.Context) (string, error) {
	endpoint := ""
	if d.Options.Socket != "" {
		endpoint = "unix://" + d.Options.Socket
	} else if host := os.Getenv("DOCKER_HOST"); host != "" && os.Getenv("DOCKER_CONTEXT") == "" {
		endpoint = host
	} else {
		args := []string{"context", "inspect"}
		if name := os.Getenv("DOCKER_CONTEXT"); name != "" {
			args = append(args, name)
		}
		data, err := d.command(ctx, nil, args...)
		if err != nil {
			return "", err
		}
		var contexts []struct {
			Endpoints map[string]struct{ Host string }
		}
		if json.Unmarshal(data, &contexts) != nil || len(contexts) != 1 {
			return "", &Error{Code: "validator_runtime_error"}
		}
		endpoint = contexts[0].Endpoints["docker"].Host
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "unix" || u.Host != "" || !filepath.IsAbs(u.Path) ||
		u.RawQuery != "" || u.Fragment != "" {
		return "", &Error{Code: "validator_local_socket_required"}
	}
	return "unix://" + filepath.Clean(u.Path), nil
}

func (d *Docker) at(ctx context.Context, endpoint string, input []byte, args ...string) ([]byte, error) {
	return d.command(ctx, input, append([]string{"--host", endpoint}, args...)...)
}

func (d *Docker) runtime(ctx context.Context, endpoint string) (string, error) {
	osName, arch := d.executablePlatform()
	if osName != "linux" || (arch != "amd64" && arch != "arm64") {
		return "", &Error{Code: "validator_platform_unsupported"}
	}
	data, err := d.at(ctx, endpoint, nil, "info", "--format", `{{json .}}`)
	if err != nil {
		return "", err
	}
	var info struct{ ID, OSType, Architecture string }
	if json.Unmarshal(
		data,
		&info,
	) != nil || info.ID == "" || info.OSType != "linux" || daemonArchitecture(info.Architecture) != arch {
		return "", &Error{Code: "validator_platform_unsupported"}
	}
	return info.ID, nil
}

func (d *Docker) executablePlatform() (string, string) {
	if d.platform != nil {
		return d.platform()
	}
	return runtime.GOOS, runtime.GOARCH
}

func daemonArchitecture(value string) string {
	switch value {
	case "amd64", "x86_64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return ""
	}
}

func (d *Docker) image(ctx context.Context, endpoint string) error {
	if !ValidImage(d.Options.Image) {
		return &Error{Code: "validator_image_required"}
	}
	data, err := d.at(ctx, endpoint, nil, "image", "inspect", d.Options.Image)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return err
		}
		return &Error{Code: "validator_image_unavailable"}
	}
	var images []struct {
		ID               string `json:"Id"`
		Architecture, Os string
		Config           struct{ Labels map[string]string }
	}
	if json.Unmarshal(data, &images) != nil || len(images) != 1 {
		return &Error{Code: "validator_image_unavailable"}
	}
	i := images[0]
	osName, arch := d.executablePlatform()
	if osName != "linux" || (arch != "amd64" && arch != "arm64") ||
		i.ID != d.Options.Image || i.Architecture != arch || i.Os != "linux" ||
		i.Config.Labels[containerLabel+".protocol"] != "2" || i.Config.Labels[containerLabel+
		".warp"] != protocol.WARPVersion {
		return &Error{Code: "validator_version_mismatch"}
	}
	return nil
}

func (d *Docker) path() string { return filepath.Join(d.Options.StateDir, "session.json") }

func (d *Docker) lock() (*os.File, error) {
	if err := privatefs.Directory(d.Options.StateDir); err != nil {
		return nil, &Error{Code: "validator_state_error"}
	}
	f, err := privatefs.Lock(filepath.Join(d.Options.StateDir, "owner.lock"))
	if err != nil {
		return nil, &Error{Code: err.Error()}
	}
	return f, nil
}

func (d *Docker) save(m *manifest) error {
	return d.replaceJournal(m)
}

func (d *Docker) replaceJournal(value any) error {
	write := d.writeJournal
	if write == nil {
		write = privatefs.Write
	}
	persist := func() error { return write(d.path(), value) }
	var err error
	if d.mutation != nil {
		err = d.mutation(persist)
	} else {
		err = persist()
	}
	if err != nil {
		return &Error{Code: "validator_state_error"}
	}
	return nil
}

func (d *Docker) read() (*manifest, error) {
	var m manifest
	err := privatefs.Read(d.path(), &m)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, &Error{Code: "validator_state_error"}
	}
	if m.Version != 1 || !tokenPattern.MatchString(m.SessionID) || m.Name != "wpc-validator-"+
		strings.ToLower(m.SessionID) || !ValidImage(m.Image) || (m.ContainerID != "" &&
		!idPattern.MatchString(m.ContainerID)) || ((m.Submitted || m.Cleaned) &&
		m.ContainerID == "") || m.Endpoint == "" || m.DaemonID == "" {
		return nil, &Error{Code: "validator_state_error"}
	}
	return &m, nil
}

func (d *Docker) inspect(ctx context.Context, m *manifest) (containerInfo, error) {
	var zero containerInfo
	name := m.ContainerID
	if name == "" {
		name = m.Name
	}
	data, err := d.at(ctx, m.Endpoint, nil, "container", "inspect", name)
	if err != nil {
		return zero, err
	}
	var values []containerInfo
	if json.Unmarshal(data, &values) != nil || len(values) != 1 {
		return zero, &Error{Code: "validator_identity_mismatch"}
	}
	i := values[0]
	if !idPattern.MatchString(i.ID) || (m.ContainerID != "" && i.ID != m.ContainerID) ||
		i.Image != m.Image || i.Config.Labels[containerLabel] != "1" ||
		i.Config.Labels[containerLabel+".session"] != m.SessionID || i.HostConfig.Privileged ||
		i.HostConfig.NetworkMode != "bridge" || i.HostConfig.PidMode != "" ||
		len(i.HostConfig.CapAdd) != 0 || len(i.Mounts) != 0 {
		return zero, &Error{Code: "validator_identity_mismatch"}
	}
	return i, nil
}

func (d *Docker) containers(ctx context.Context, endpoint string) ([]string, error) {
	data, err := d.at(
		ctx,
		endpoint,
		nil,
		"ps",
		"-a",
		"--no-trunc",
		"--filter",
		"label="+containerLabel+"=1",
		"--format",
		"{{.ID}}",
	)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(data)), nil
}

func (d *Docker) worker(ctx context.Context, m *manifest, operation string, input []byte) (protocol.Receipt, error) {
	var receipt protocol.Receipt
	if operation == "recover" && m.Version == 2 {
		remaining := 60 * time.Second
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < remaining {
			remaining = time.Until(deadline)
		}
		if remaining <= 0 {
			return receipt, context.DeadlineExceeded
		}
		var err error
		input, err = json.Marshal(protocol.Operation{RemainingUS: remaining.Microseconds()})
		if err != nil {
			return receipt, &Error{Code: "validator_protocol_error"}
		}
	}
	data, err := d.at(ctx, m.Endpoint, input, "exec", "-i", m.ContainerID, "/usr/local/bin/wpc-worker", operation)
	if err != nil {
		return receipt, err
	}
	if json.Unmarshal(
		data,
		&receipt,
	) != nil || receipt.Version != m.Version || receipt.SessionID != m.SessionID {
		return receipt, &Error{Code: "validator_protocol_error"}
	}
	return receipt, nil
}

func (d *Docker) ready(ctx context.Context, m *manifest) error {
	for {
		_, err := d.at(ctx, m.Endpoint, nil, "exec", m.ContainerID, "/usr/local/bin/wpc-worker", "ready")
		if err == nil {
			return nil
		}
		var diagnostic *Error
		if errors.As(err, &diagnostic) && diagnostic.Code == "validator_version_mismatch" {
			return err
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (d *Docker) remove(ctx context.Context, m *manifest) error {
	if !m.Cleaned {
		return &Error{Code: "validation_cleanup_pending"}
	}
	ids, err := d.containers(ctx, m.Endpoint)
	if err != nil {
		return err
	}
	found := false
	for _, id := range ids {
		if id == m.ContainerID {
			found = true
		}
	}
	if found {
		if _, err := d.inspect(ctx, m); err != nil {
			return err
		}
		if _, err := d.at(ctx, m.Endpoint, nil, "rm", "--force", m.ContainerID); err != nil {
			return err
		}
	}
	remove := func() error { return privatefs.Remove(d.path()) }
	if d.mutation != nil {
		err = d.mutation(remove)
	} else {
		err = remove()
	}
	if err != nil {
		return &Error{Code: "validator_state_error"}
	}
	return nil
}

func (d *Docker) reconcile(ctx context.Context, endpoint, daemonID string) error {
	m, err := d.read()
	if err != nil {
		return err
	}
	ids, err := d.containers(ctx, endpoint)
	if err != nil {
		return err
	}
	if m == nil {
		if len(ids) != 0 {
			return &Error{Code: "validator_orphaned_session"}
		}
		return nil
	}
	if m.Endpoint != endpoint || m.DaemonID != daemonID {
		return &Error{Code: "validator_daemon_mismatch"}
	}
	if m.Cleaned {
		for _, id := range ids {
			if id != m.ContainerID {
				return &Error{Code: "validator_orphaned_session"}
			}
		}
		return d.remove(ctx, m)
	}
	info, err := d.inspect(ctx, m)
	if err != nil {
		// No remote operation starts until the container ID was durably recorded.
		if m.ContainerID == "" && len(ids) == 0 {
			remove := func() error { return privatefs.Remove(d.path()) }
			if d.mutation != nil {
				err = d.mutation(remove)
			} else {
				err = remove()
			}
			if err != nil {
				return &Error{Code: "validator_state_error"}
			}
			return nil
		}
		return &Error{Code: "validation_cleanup_pending"}
	}
	if m.ContainerID == "" {
		m.ContainerID = info.ID
		if err := d.save(m); err != nil {
			return err
		}
	}
	if len(ids) != 1 || ids[0] != m.ContainerID {
		return &Error{Code: "validator_orphaned_session"}
	}
	if !m.Submitted {
		m.Cleaned = true
		if err := d.save(m); err != nil {
			return err
		}
		return d.remove(ctx, m)
	}
	if !info.State.Running {
		if _, err := d.at(ctx, m.Endpoint, nil, "start", m.ContainerID); err != nil {
			return err
		}
	}
	ready, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = d.ready(ready, m)
	cancel()
	if err != nil {
		return err
	}
	// A surviving worker sees cancellation before any subsequent attachment.
	_, _ = d.at(ctx, m.Endpoint, nil, "exec", m.ContainerID, "/usr/local/bin/wpc-worker", "cancel")
	for {
		receipt, err := d.worker(ctx, m, "recover", nil)
		if err == nil && receipt.Phase == "cleaned" {
			m.Cleaned = true
			if err := d.save(m); err != nil {
				return err
			}
			return d.remove(ctx, m)
		}
		var diagnostic *Error
		if !errors.As(err, &diagnostic) || diagnostic.Code != "validator_busy" {
			return &Error{Code: "validation_cleanup_pending"}
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Recover reconciles all retained sessions without new registration or attachment.
func (d *Docker) Recover(parent context.Context) error {
	owner, err := d.acquire(parent, false)
	if err != nil {
		return err
	}
	return owner.Close()
}

// Check cannot bypass batch ownership or shared mutation admission.
func (d *Docker) Check(context.Context, Input) (Result, error) {
	return Result{}, &Error{Code: "validation_batch_required"}
}
