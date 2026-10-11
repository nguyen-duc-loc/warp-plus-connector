package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cappedOutput struct {
	sync.Mutex
	stdout   bytes.Buffer
	used     int
	overflow bool
}
type cappedStream struct {
	output  *cappedOutput
	capture bool
}

func (w cappedStream) Write(p []byte) (int, error) {
	w.output.Lock()
	defer w.output.Unlock()
	w.output.used += len(p)
	if w.output.used > 1024*1024 {
		w.output.overflow = true
		return 0, failure("service_manager_unavailable")
	}
	if w.capture {
		return w.output.stdout.Write(p)
	}
	return len(p), nil
}

func command(parent context.Context, limit time.Duration, path string, args ...string) ([]byte, error) {
	return commandEnvironment(parent, limit, nil, path, args...)
}

func commandEnvironment(
	parent context.Context, limit time.Duration, environment []string, path string, args ...string,
) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	if environment != nil {
		cmd.Env = environment
	}
	cmd.WaitDelay = time.Second
	var output cappedOutput
	cmd.Stdout, cmd.Stderr = cappedStream{&output, true}, cappedStream{&output, false}
	err := cmd.Run()
	if err != nil || output.overflow || ctx.Err() != nil {
		return nil, failure("service_manager_unavailable")
	}
	return output.stdout.Bytes(), nil
}

type nativeConfigKey struct{}

func xmlText(v string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(v))
	return b.String()
}

func unitQuote(v string, execution bool) string {
	v = strings.ReplaceAll(v, "%", "%%")
	if execution {
		v = strings.ReplaceAll(v, "$", "$$")
	}
	return strconv.Quote(v)
}

func arguments(m Manifest) []string {
	return []string{m.ExecutablePath, "service", "exec", "--installation-id", m.InstallationID}
}

func renderDefinition(m Manifest) ([]byte, error) {
	var b strings.Builder
	switch m.Manager {
	case "launchd":
		b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
		b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" ")
		b.WriteString("\"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict>\n")
		fmt.Fprintf(
			&b,
			"<key>Label</key><string>%s</string>\n<key>ProgramArguments</key><array>\n",
			xmlText(m.ServiceName),
		)
		for _, arg := range arguments(m) {
			fmt.Fprintf(&b, "<string>%s</string>\n", xmlText(arg))
		}
		b.WriteString("</array>\n<key>RunAtLoad</key><true/>\n")
		b.WriteString("<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>\n")
		b.WriteString("<key>ThrottleInterval</key><integer>30</integer>\n")
		b.WriteString("<key>ExitTimeOut</key><integer>75</integer>\n<key>Umask</key><integer>63</integer>\n")
		fmt.Fprintf(&b, "<key>WorkingDirectory</key><string>%s</string>\n", xmlText(m.WorkingDir))
		b.WriteString("<key>EnvironmentVariables</key><dict>\n")
		for _, pair := range environment(m) {
			key, value, _ := strings.Cut(pair, "=")
			fmt.Fprintf(&b, "<key>%s</key><string>%s</string>\n", key, xmlText(value))
		}
		b.WriteString("</dict>\n</dict></plist>\n")
	case "systemd":
		b.WriteString("[Unit]\nDescription=Warp Plus Connector\n[Service]\nType=exec\nExecStart=")
		for i, arg := range arguments(m) {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(unitQuote(arg, true))
		}
		b.WriteString("\nRestart=on-failure\nRestartSec=30s\nTimeoutStopSec=75s\n")
		b.WriteString("KillMode=mixed\nKillSignal=SIGTERM\nSendSIGKILL=yes\nUMask=0077\nWorkingDirectory=")
		// WorkingDirectory is a literal path, not a quoted command word. A final
		// dot preserves trailing spaces and backslashes through unit file parsing.
		b.WriteString(strings.ReplaceAll(m.WorkingDir, "%", "%%") + "/.")
		for _, pair := range environment(m) {
			b.WriteString("\nEnvironment=" + unitQuote(pair, false))
		}
		b.WriteString("\n[Install]\nWantedBy=default.target\n")
	default:
		return nil, failure("service_record_invalid")
	}
	return []byte(b.String()), nil
}

func environment(m Manifest) []string {
	return []string{"HOME=" + m.HomeDir, "XDG_CONFIG_HOME=" + m.ConfigHome, "XDG_STATE_HOME=" + m.StateHome}
}

// ManagerState contains native observations, with unsupported values left unknown.
type ManagerState struct {
	Loaded            bool    `json:"loaded"`
	Running           *bool   `json:"running"`
	PID               *int    `json:"pid"`
	LastExitCode      *int    `json:"last_exit_code"`
	LastSignal        *int    `json:"last_signal"`
	ActiveState       *string `json:"active_state"`
	PendingJob        *bool   `json:"pending_job"`
	AutostartObserved *bool   `json:"autostart_observed"`
	Identity          string  `json:"identity"`
}

func native(ctx context.Context, a Account, limit time.Duration, args ...string) ([]byte, error) {
	if a.Manager == "launchd" {
		return command(ctx, limit, "/bin/launchctl", args...)
	}
	path, err := systemTool("systemctl")
	if err != nil {
		return nil, err
	}
	configHome, _ := ctx.Value(nativeConfigKey{}).(string)
	if configHome == "" {
		configHome = filepath.Join(a.Home, ".config")
	}
	runtimeDir := "/run/user/" + strconv.Itoa(a.UID)
	// Bind the local user bus and startup links instead of inheriting shell redirection.
	env := []string{"PATH=/usr/bin:/bin", "HOME=" + a.Home, "XDG_CONFIG_HOME=" + configHome,
		"XDG_RUNTIME_DIR=" + runtimeDir, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + runtimeDir + "/bus", "LC_ALL=C"}
	if info, err := os.Stat(runtimeDir); err != nil || !private(info, true) {
		return nil, failure("service_manager_unavailable")
	}
	return commandEnvironment(ctx, limit, env, path, append([]string{"--user"}, args...)...)
}

func target(a Account) string { return fmt.Sprintf("gui/%d/%s", a.UID, a.Name) }

func preflight(ctx context.Context, a Account, m Manifest) error {
	if a.Manager == "launchd" {
		_, err := native(ctx, a, 5*time.Second, "print", fmt.Sprintf("gui/%d", a.UID))
		return err
	}
	data, err := native(ctx, a, 5*time.Second, "show", "--property=UnitPath", "--value")
	if err != nil {
		return err
	}
	paths, err := splitWords(strings.TrimSpace(string(data)))
	if err != nil {
		return err
	}
	for _, p := range paths {
		resolved, err := canonical(p)
		if err == nil && resolved == filepath.Dir(m.DefinitionPath) {
			return nil
		}
	}
	return failure("service_manager_unavailable")
}

func macList(ctx context.Context, a Account) (ManagerState, error) {
	s := ManagerState{Identity: "unverified"}
	name, err := native(ctx, a, 5*time.Second, "managername")
	if err != nil || strings.TrimSpace(string(name)) != "Aqua" {
		return s, failure("service_manager_unavailable")
	}
	uid, err := native(ctx, a, 5*time.Second, "manageruid")
	if err != nil || strings.TrimSpace(string(uid)) != strconv.Itoa(a.UID) {
		return s, failure("service_manager_unavailable")
	}
	data, err := native(ctx, a, 5*time.Second, "list")
	if err != nil {
		return s, err
	}
	found := false
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if i == 0 && len(fields) == 3 && fields[0] == "PID" && fields[2] == "Label" {
			continue
		}
		if len(fields) != 3 {
			return s, failure("service_manager_unavailable")
		}
		if fields[2] != a.Name {
			continue
		}
		if found {
			return s, failure("service_manager_unavailable")
		}
		found, s.Loaded = true, true
		if fields[0] != "-" {
			pid, err := strconv.Atoi(fields[0])
			if err != nil || pid <= 0 {
				return s, failure("service_manager_unavailable")
			}
			s.PID = &pid
		}
		exit, err := strconv.Atoi(fields[1])
		if err != nil {
			return s, failure("service_manager_unavailable")
		}
		if exit < 0 {
			signal := -exit
			s.LastSignal = &signal
		} else {
			s.LastExitCode = &exit
		}
	}
	running := s.PID != nil
	s.Running = &running
	if !s.Loaded {
		s.Identity = "absent"
	}
	return s, nil
}

func properties(data []byte) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, failure("service_manager_unavailable")
		}
		if _, ok := out[key]; ok {
			return nil, failure("service_manager_unavailable")
		}
		out[key] = value
	}
	return out, nil
}

func linuxState(ctx context.Context, a Account, m *Manifest) (ManagerState, error) {
	s := ManagerState{Identity: "unverified"}
	data, err := native(ctx, a, 5*time.Second, "show", a.Name, "--all", "--no-pager")
	if err != nil {
		return s, err
	}
	return linuxPropertiesState(data, m)
}

func linuxPropertiesState(data []byte, m *Manifest) (ManagerState, error) {
	s := ManagerState{Identity: "unverified"}
	p, err := properties(data)
	if err != nil {
		return s, err
	}
	jobValue, ok := p["Job"]
	if !ok {
		return s, failure("service_manager_unavailable")
	}
	// systemctl renders an absent job as an empty value, not a numeric zero.
	job := 0
	if jobValue != "" {
		job, err = strconv.Atoi(jobValue)
		if err != nil || job < 0 {
			return s, failure("service_manager_unavailable")
		}
	}
	if p["LoadState"] == "not-found" {
		if p["MainPID"] != "0" || job != 0 {
			return s, failure("service_manager_unavailable")
		}
		s.Identity = "absent"
		return s, nil
	}
	if p["LoadState"] != "loaded" {
		return s, failure("service_manager_unavailable")
	}
	s.Loaded = true
	pid, err := strconv.Atoi(p["MainPID"])
	if err != nil || pid < 0 {
		return s, failure("service_manager_unavailable")
	}
	s.PID = &pid
	running := pid > 0
	s.Running = &running
	active := p["ActiveState"]
	s.ActiveState = &active
	pending := job != 0
	s.PendingJob = &pending
	status, err := strconv.Atoi(p["ExecMainStatus"])
	if err != nil {
		return s, failure("service_manager_unavailable")
	}
	switch p["ExecMainCode"] {
	case "1":
		s.LastExitCode = &status
	case "2", "3":
		s.LastSignal = &status
	}
	switch p["UnitFileState"] {
	case "enabled":
		b := true
		s.AutostartObserved = &b
	case "disabled":
		b := false
		s.AutostartObserved = &b
	}
	if m != nil && m.Operation == "uninstall" && oneOf(
		m.Phase,
		"manager_applied",
		"definition_removed",
		"manager_removed",
	) {
		// The owned unit can remain cached after its acknowledged file removal.
		if p["NeedDaemonReload"] == "yes" {
			p["NeedDaemonReload"] = "no"
		}
	}
	if m != nil && verifyLinuxProperties(p, *m) == nil {
		s.Identity = "verified"
	}
	return s, nil
}

func verifyLinuxProperties(p map[string]string, m Manifest) error {
	expected := map[string]string{
		"FragmentPath": m.DefinitionPath, "DropInPaths": "", "Type": "exec", "Restart": "on-failure",
		"RestartUSec": "30s", "TimeoutStopUSec": "1min 15s", "KillMode": "mixed", "KillSignal": "15",
		"SendSIGKILL": "yes", "UMask": "0077", "WorkingDirectory": m.WorkingDir,
		"NeedDaemonReload": "no",
	}
	for k, v := range expected {
		if actual, ok := p[k]; !ok || actual != v {
			return failure("service_definition_changed")
		}
	}
	// systemctl omits empty struct arrays even with --all. Nonempty arrays
	// produce records and must still reject extra commands or environment files.
	for _, k := range []string{
		"EnvironmentFiles", "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "ExecCondition", "ExecReload",
	} {
		if p[k] != "" {
			return failure("service_definition_changed")
		}
	}
	env, err := splitWords(p["Environment"])
	if err != nil || len(env) != len(environment(m)) {
		return failure("service_definition_changed")
	}
	for _, value := range environment(m) {
		found := false
		for _, e := range env {
			if e == value {
				found = true
			}
		}
		if !found {
			return failure("service_definition_changed")
		}
	}
	start := p["ExecStart"]
	if strings.Count(start, "{ path=") != 1 {
		return failure("service_definition_changed")
	}
	path, rest, ok := strings.Cut(strings.TrimPrefix(start, "{ path="), " ; argv[]=")
	if !ok || path != m.ExecutablePath {
		return failure("service_definition_changed")
	}
	argv, suffix, ok := strings.Cut(rest, " ; ignore_errors=no ;")
	if !ok || !strings.HasSuffix(suffix, " }") {
		return failure("service_definition_changed")
	}
	args, err := splitWords(argv)
	if err != nil || len(args) != len(arguments(m)) {
		return failure("service_definition_changed")
	}
	for i, v := range arguments(m) {
		if args[i] != v {
			return failure("service_definition_changed")
		}
	}
	return nil
}

// splitWords accepts quoted native words and C escapes, rejecting incomplete forms.
func splitWords(s string) ([]string, error) {
	var out []string
	for len(strings.TrimSpace(s)) > 0 {
		s = strings.TrimLeft(s, " \t\r\n")
		var b strings.Builder
		quote := byte(0)
		for len(s) > 0 {
			c := s[0]
			if quote == 0 && (c == ' ' || c == '\t' || c == '\n') {
				break
			}
			s = s[1:]
			if c == '"' || c == '\'' {
				if quote == 0 {
					quote = c
					continue
				}
				if quote == c {
					quote = 0
					continue
				}
			}
			if c == '\\' {
				if len(s) == 0 {
					return nil, failure("service_manager_unavailable")
				}
				if s[0] == 'x' {
					if len(s) < 3 {
						return nil, failure("service_manager_unavailable")
					}
					n, err := strconv.ParseUint(s[1:3], 16, 8)
					if err != nil {
						return nil, failure("service_manager_unavailable")
					}
					b.WriteByte(byte(n))
					s = s[3:]
					continue
				}
				c, s = s[0], s[1:]
			}
			b.WriteByte(c)
		}
		if quote != 0 {
			return nil, failure("service_manager_unavailable")
		}
		out = append(out, b.String())
	}
	return out, nil
}

func inspect(ctx context.Context, a Account, m *Manifest) (ManagerState, error) {
	if a.Manager == "systemd" {
		return linuxState(ctx, a, m)
	}
	s, err := macList(ctx, a)
	if err != nil || m == nil || !s.Loaded || s.PID == nil {
		return s, err
	}
	first, err := readObservation(a.Root)
	if err != nil {
		return s, nil
	}
	pid, err := ownerPID(filepath.Join(a.Root, "runner.lock"))
	if err != nil || pid == nil || *pid != *s.PID {
		return s, nil
	}
	second, err := readObservation(a.Root)
	if err == nil && first.InstallationID == m.InstallationID && first.InvocationID == second.InvocationID &&
		first.PID == second.PID && first.PID == *pid && !first.terminal() && !second.terminal() &&
		second.InstallationID == m.InstallationID && verifyDefinition(*m) == nil {
		s.Identity = "verified"
	}
	return s, nil
}

func checkedNative(ctx context.Context, a Account, m Manifest) (ManagerState, error) {
	if err := verifyDefinition(m); err != nil {
		return ManagerState{}, err
	}
	s, err := inspect(ctx, a, &m)
	if err != nil {
		return s, err
	}
	if s.Identity == "unverified" {
		return s, failure("service_identity_unverified")
	}
	return s, nil
}

func startup(ctx context.Context, a Account, m Manifest, enabled bool) error {
	ctx = context.WithValue(ctx, nativeConfigKey{}, m.ConfigHome)
	action := "disable"
	if enabled {
		action = "enable"
	}
	arg := m.DefinitionPath
	if a.Manager == "launchd" {
		arg = target(a)
	}
	_, err := native(ctx, a, 10*time.Second, action, arg)
	return err
}

func reload(ctx context.Context, a Account) error {
	if a.Manager == "launchd" {
		return nil
	}
	_, err := native(ctx, a, 10*time.Second, "daemon-reload")
	return err
}

func nativeGone(s ManagerState) bool {
	return !s.Loaded || (s.Running != nil && !*s.Running && s.PendingJob != nil && !*s.PendingJob &&
		s.ActiveState != nil && oneOf(*s.ActiveState, "inactive", "failed"))
}

func uncertain(err error, changed bool) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return &Error{Code: e.Code, Changed: changed || e.Changed}
	}
	return &Error{Code: "service_access_error", Changed: changed}
}
