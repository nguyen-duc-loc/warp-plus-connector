package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// This fixture follows systemd 259 output: empty execution and environment file
// arrays are omitted, while empty scalar and string array properties remain.
func linuxIdentityFixture() (map[string]string, Manifest) {
	m := Manifest{
		ExecutablePath: "/home/wpc/.local/bin/wpc", InstallationID: strings.Repeat("a", 32),
		DefinitionPath: "/home/wpc/.config/systemd/user/warp-plus-connector.service",
		WorkingDir:     "/tmp", HomeDir: "/home/wpc", ConfigHome: "/home/wpc/.config", StateHome: "/home/wpc/.local/state",
	}
	p := map[string]string{
		"FragmentPath": m.DefinitionPath, "DropInPaths": "", "NeedDaemonReload": "no",
		"Type": "exec", "Restart": "on-failure", "RestartUSec": "30s", "TimeoutStopUSec": "1min 15s",
		"UMask": "0077", "WorkingDirectory": "/tmp", "KillMode": "mixed", "KillSignal": "15", "SendSIGKILL": "yes",
		"Environment": "HOME=/home/wpc XDG_CONFIG_HOME=/home/wpc/.config XDG_STATE_HOME=/home/wpc/.local/state",
		"ExecStart": "{ path=/home/wpc/.local/bin/wpc ; argv[]=/home/wpc/.local/bin/wpc service exec " +
			"--installation-id " + m.InstallationID + " ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; " +
			"pid=0 ; code=(null) ; status=0/0 }",
	}
	return p, m
}

// Spec 0008 AC-3 and AC-14: omitted empty native arrays are safe, added commands are not.
func TestLinuxIdentityAcceptsEmptyArraysButRejectsOverrides(t *testing.T) {
	p, m := linuxIdentityFixture()
	require.NoError(t, verifyLinuxProperties(p, m))
	for _, name := range []string{
		"EnvironmentFiles", "ExecStartPre", "ExecStartPost", "ExecStop", "ExecStopPost", "ExecCondition", "ExecReload",
	} {
		p[name] = "/foreign"
		require.Error(t, verifyLinuxProperties(p, m), name)
		delete(p, name)
	}
	delete(p, "DropInPaths")
	require.Error(t, verifyLinuxProperties(p, m))
}

// Spec 0008 AC-4 and AC-14: native absence must include an explicit lack of pending work.
func TestLinuxAbsentUnitJobObservation(t *testing.T) {
	for _, job := range []string{"", "0"} {
		s, err := linuxPropertiesState([]byte("LoadState=not-found\nMainPID=0\nJob="+job+"\n"), nil)
		require.NoError(t, err)
		require.Equal(t, "absent", s.Identity)
	}
	for _, job := range []string{"Job=42\n", "Job=-1\n", "Job=unknown\n", ""} {
		_, err := linuxPropertiesState([]byte("LoadState=not-found\nMainPID=0\n"+job), nil)
		require.Error(t, err)
	}
}

// Spec 0008 AC-4: loaded units distinguish no pending job from an accepted native operation.
func TestLinuxLoadedUnitJobObservation(t *testing.T) {
	for _, job := range []string{"", "0", "42"} {
		data := "LoadState=loaded\nMainPID=0\nActiveState=inactive\nExecMainStatus=0\nJob=" + job + "\n"
		s, err := linuxPropertiesState([]byte(data), nil)
		require.NoError(t, err)
		require.NotNil(t, s.PendingJob)
		require.Equal(t, job == "42", *s.PendingJob)
		require.Equal(t, job != "42", nativeGone(s))
	}
}
