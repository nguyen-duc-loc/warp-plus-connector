package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
)

// Manifest retains the exact local binding and acknowledged control operation.
type Manifest struct {
	SchemaVersion     int       `json:"schema_version"`
	InstallationID    string    `json:"installation_id"`
	UID               int       `json:"uid"`
	Manager           string    `json:"manager"`
	ServiceName       string    `json:"service_name"`
	InstalledAt       time.Time `json:"installed_at"`
	HomeDir           string    `json:"home_dir"`
	ServiceDir        string    `json:"service_dir"`
	DefinitionPath    string    `json:"definition_path"`
	ExecutablePath    string    `json:"executable_path"`
	ConfigPath        string    `json:"config_path"`
	StateDir          string    `json:"state_dir"`
	ValidatorStateDir string    `json:"validator_state_dir"`
	WorkingDir        string    `json:"working_dir"`
	ConfigHome        string    `json:"config_home"`
	StateHome         string    `json:"state_home"`
	DockerExecutable  *string   `json:"docker_executable"`
	DockerSocket      *string   `json:"docker_socket"`
	DefinitionSHA256  string    `json:"definition_sha256"`
	AutostartEnabled  *bool     `json:"autostart_enabled"`
	Operation         string    `json:"operation"`
	Phase             string    `json:"phase"`
}

// Account is resolved from the native effective account database.
type Account struct {
	UID                       int
	Home, Root, Manager, Name string
}

// ResolveAccount never uses the caller's HOME as authority.
func ResolveAccount(ctx context.Context) (Account, error) {
	a := Account{UID: os.Geteuid()}
	if runtime.GOARCH != "arm64" {
		return a, failure("service_platform_unsupported")
	}
	uid := strconv.Itoa(a.UID)
	switch runtime.GOOS {
	case "darwin":
		data, err := command(ctx, 5*time.Second, "/usr/bin/dscacheutil", "-q", "user", "-a", "uid", uid)
		if err != nil {
			return a, failure("service_account_unavailable")
		}
		uids, homes := 0, 0
		for _, line := range strings.Split(string(data), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "uid":
				uids++
				if strings.TrimSpace(value) != uid {
					return a, failure("service_account_unavailable")
				}
			case "dir":
				homes++
				a.Home = strings.TrimSpace(value)
			}
		}
		if uids != 1 || homes != 1 {
			return a, failure("service_account_unavailable")
		}
		a.Manager, a.Name = "launchd", "io.github.nguyen-duc-loc.wpc"
	case "linux":
		path, err := systemTool("getent")
		if err != nil {
			return a, failure("service_account_unavailable")
		}
		data, err := command(ctx, 5*time.Second, path, "passwd", uid)
		if err != nil {
			return a, failure("service_account_unavailable")
		}
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		if len(lines) != 1 {
			return a, failure("service_account_unavailable")
		}
		fields := strings.Split(lines[0], ":")
		if len(fields) != 7 || fields[2] != uid {
			return a, failure("service_account_unavailable")
		}
		a.Home = fields[5]
		a.Manager, a.Name = "systemd", "warp-plus-connector.service"
	default:
		return a, failure("service_platform_unsupported")
	}
	if !safePath(a.Home) {
		return a, failure("service_account_unavailable")
	}
	home, err := filepath.EvalSymlinks(a.Home)
	if err != nil {
		return a, failure("service_account_unavailable")
	}
	a.Home = home
	base := filepath.Join(home, ".local", "state", "warp-plus-connector")
	if a.Manager == "launchd" {
		base = filepath.Join(home, "Library", "Application Support", "warp-plus-connector")
	}
	a.Root = filepath.Join(base, "service")
	return a, nil
}

func safePath(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && !strings.ContainsAny(p, "\x00\r\n")
}

func systemTool(name string) (string, error) {
	for _, dir := range []string{"/usr/bin", "/bin"} {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return p, nil
		}
	}
	return "", failure("service_manager_unavailable")
}

func executable(path string) error {
	i, err := os.Lstat(path)
	if err != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0111 == 0 || i.Mode().Perm()&0022 != 0 {
		return failure("service_executable_invalid")
	}
	return nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (a Account) manifest() (Manifest, error) {
	var m Manifest
	if err := checkDirectory(a.Root); err != nil {
		return m, err
	}
	if err := readRecord(filepath.Join(a.Root, "installation.json"), &m); err != nil {
		return m, err
	}
	if m.SchemaVersion != 1 || !identityPattern.MatchString(m.InstallationID) || m.UID != a.UID ||
		m.HomeDir != a.Home || m.ServiceDir != a.Root || m.Manager != a.Manager || m.ServiceName != a.Name ||
		!validTime(m.InstalledAt) || !hashPattern.MatchString(m.DefinitionSHA256) {
		return m, failure("service_record_invalid")
	}
	for _, p := range []string{
		m.HomeDir, m.ServiceDir, m.DefinitionPath, m.ExecutablePath, m.ConfigPath,
		m.StateDir, m.ValidatorStateDir, m.WorkingDir, m.ConfigHome, m.StateHome,
	} {
		if !safePath(p) {
			return m, failure("service_record_invalid")
		}
	}
	if m.StateDir == a.Root || strings.HasPrefix(m.StateDir, a.Root+string(os.PathSeparator)) ||
		(strings.HasPrefix(a.Root, m.StateDir+string(os.PathSeparator)) && m.StateDir != filepath.Dir(a.Root)) {
		return m, failure("service_record_invalid")
	}
	if (m.DockerExecutable == nil) != (m.DockerSocket == nil) {
		return m, failure("service_record_invalid")
	}
	if m.DockerSocket != nil && (!safePath(*m.DockerSocket) || !safePath(*m.DockerExecutable)) {
		return m, failure("service_record_invalid")
	}
	if m.DefinitionPath != definitionPath(a, m.ConfigHome) {
		return m, failure("service_record_invalid")
	}
	valid := false
	switch m.Operation {
	case "install":
		valid = m.Phase == "prepared" || m.Phase == "definition_written" || m.Phase == "manager_applied"
	case "enable", "disable":
		valid = m.Phase == "prepared" || m.Phase == "manager_applied"
	case "uninstall":
		valid = oneOf(m.Phase, "prepared", "manager_applied", "definition_removed") ||
			(a.Manager == "systemd" && m.Phase == "manager_removed")
	}
	if m.Phase == "ready" && oneOf(m.Operation, "install", "enable", "disable") {
		valid = true
	}
	initial := m.Operation == "install" && oneOf(m.Phase, "prepared", "definition_written")
	if !valid || (m.AutostartEnabled == nil && !initial) {
		return m, failure("service_record_invalid")
	}
	return m, nil
}

func definitionPath(a Account, configHome string) string {
	if a.Manager == "launchd" {
		return filepath.Join(a.Home, "Library", "LaunchAgents", a.Name+".plist")
	}
	return filepath.Join(configHome, "systemd", "user", a.Name)
}

func (m Manifest) save() error {
	return writeRecord(filepath.Join(m.ServiceDir, "installation.json"), m)
}

// Settings rereads configuration under the binding and rejects path drift before work.
func (m Manifest) Settings() (config.Settings, error) {
	s, err := config.ResolveAt(
		config.Overrides{ConfigPath: &m.ConfigPath}, config.ResolutionContext{
			ConfigHome: m.ConfigHome, DefaultStateDir: filepath.Join(m.StateHome, "warp-plus-connector"),
			WorkingDir: m.WorkingDir,
		},
	)
	if err != nil || s.ValidateRun() != nil {
		return s, failure("service_config_invalid")
	}
	stateDir, err := canonical(s.StateDir)
	if err != nil {
		return s, err
	}
	validatorDir, err := canonical(s.ValidatorStateDir)
	if err != nil {
		return s, err
	}
	if stateDir != m.StateDir || validatorDir != m.ValidatorStateDir ||
		(s.ValidationEnabled && m.DockerSocket == nil) ||
		(s.ValidatorSocket != "" && (m.DockerSocket == nil || s.ValidatorSocket != *m.DockerSocket)) {
		return s, failure("service_binding_changed")
	}
	s.StateDir, s.ValidatorStateDir = stateDir, validatorDir
	if m.DockerSocket != nil {
		s.ValidatorSocket = *m.DockerSocket
	}
	return s, nil
}

func prepare(ctx context.Context, a Account, configPath string) (Manifest, error) {
	m := Manifest{
		SchemaVersion: 1, UID: a.UID, HomeDir: a.Home, ServiceDir: a.Root,
		Manager: a.Manager, ServiceName: a.Name, Operation: "install", Phase: "prepared",
	}
	var err error
	m.InstallationID, err = newIdentity()
	if err != nil {
		return m, err
	}
	m.InstalledAt = time.Now().UTC().Truncate(time.Microsecond)
	m.WorkingDir, err = os.Getwd()
	if err != nil {
		return m, failure("service_access_error")
	}
	m.WorkingDir, err = canonical(m.WorkingDir)
	if err != nil {
		return m, err
	}
	m.ConfigHome = filepath.Join(a.Home, ".config")
	m.StateHome = filepath.Join(a.Home, ".local", "state")
	if a.Manager == "launchd" {
		m.ConfigHome = filepath.Join(a.Home, "Library", "Application Support")
		m.StateHome = m.ConfigHome
	} else {
		if p := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(p) {
			m.ConfigHome = filepath.Clean(p)
		}
		if p := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(p) {
			m.StateHome = filepath.Clean(p)
		}
	}
	m.ConfigHome, err = canonical(m.ConfigHome)
	if err != nil {
		return m, err
	}
	m.StateHome, err = canonical(m.StateHome)
	if err != nil {
		return m, err
	}
	m.ConfigPath, err = filepath.Abs(configPath)
	if err != nil {
		return m, failure("service_config_invalid")
	}
	// Resolve before canonicalizing so a symlink at the configuration file stays forbidden.
	s, err := config.ResolveAt(
		config.Overrides{ConfigPath: &m.ConfigPath}, config.ResolutionContext{
			ConfigHome: m.ConfigHome, DefaultStateDir: filepath.Join(m.StateHome, "warp-plus-connector"),
			WorkingDir: m.WorkingDir,
		},
	)
	if err != nil || s.ValidateRun() != nil {
		return m, failure("service_config_invalid")
	}
	m.ConfigPath, err = canonical(m.ConfigPath)
	if err != nil {
		return m, err
	}
	m.StateDir, err = canonical(s.StateDir)
	if err != nil {
		return m, err
	}
	m.ValidatorStateDir, err = canonical(s.ValidatorStateDir)
	if err != nil {
		return m, err
	}
	defaultState := filepath.Dir(a.Root)
	if m.StateDir == a.Root || strings.HasPrefix(m.StateDir, a.Root+string(os.PathSeparator)) ||
		(strings.HasPrefix(a.Root, m.StateDir+string(os.PathSeparator)) && m.StateDir != defaultState) {
		return m, failure("service_config_invalid")
	}
	p, err := os.Executable()
	if err != nil {
		return m, failure("service_executable_invalid")
	}
	m.ExecutablePath, err = filepath.EvalSymlinks(p)
	if err != nil {
		return m, failure("service_executable_invalid")
	}
	if err := executable(m.ExecutablePath); err != nil {
		return m, err
	}
	if s.ValidationEnabled {
		p, err := exec.LookPath("docker")
		if err != nil {
			return m, failure("service_config_invalid")
		}
		p, err = filepath.EvalSymlinks(p)
		if err != nil {
			return m, failure("service_config_invalid")
		}
		if err := executable(p); err != nil {
			return m, err
		}
		d := validator.Docker{Options: validator.DockerOptions{Executable: p, Socket: s.ValidatorSocket}}
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		socket, err := d.LocalSocket(probe)
		cancel()
		if err != nil {
			return m, failure("service_config_invalid")
		}
		m.DockerExecutable, m.DockerSocket = &p, &socket
	}
	m.DefinitionPath = definitionPath(a, m.ConfigHome)
	for _, p := range []string{
		m.DefinitionPath, m.ExecutablePath, m.ConfigPath, m.StateDir,
		m.ValidatorStateDir, m.WorkingDir, m.ConfigHome, m.StateHome,
	} {
		if !safePath(p) {
			return m, failure("service_config_invalid")
		}
	}
	data, err := renderDefinition(m)
	if err != nil {
		return m, err
	}
	m.DefinitionSHA256 = digest(data)
	return m, nil
}

func verifyDefinition(m Manifest) error {
	data, err := readBytes(m.DefinitionPath, 64*1024)
	if err != nil {
		return err
	}
	expected, err := renderDefinition(m)
	if err != nil || digest(data) != m.DefinitionSHA256 || string(data) != string(expected) {
		return failure("service_definition_changed")
	}
	return nil
}

func missing(err error) bool { return errors.Is(err, os.ErrNotExist) }
