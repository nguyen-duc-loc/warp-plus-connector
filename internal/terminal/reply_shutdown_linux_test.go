package terminal_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Spec 0010 AC-12 and AC-13: use anonymous OS pipes, matching the real Go supervisor.
// Node child process "pipe" stdio can be sockets and does not reproduce blocked filesystem reads.
func verifyChildProtocol(t *testing.T, executable, mode string) {
	t.Helper()
	root := filepath.Join(filepath.Dir(executable), "lib", "wpc-ui")
	manifest, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	require.NoError(t, err)
	var identity struct{ Release, Commit string }
	require.NoError(t, json.Unmarshal(manifest, &identity))
	requests, childRequests, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = requests.Close() }()
	defer func() { _ = childRequests.Close() }()
	childReplies, replies, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = childReplies.Close() }()
	defer func() { _ = replies.Close() }()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	require.NoError(t, err)
	defer func() { _ = master.Close() }()
	require.NoError(t, unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0))
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	require.NoError(t, err)
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	require.NoError(t, err)
	defer func() { _ = slave.Close() }()
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	require.NoError(t, err)
	require.NoError(t, unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 110}))
	cmd := exec.Command(filepath.Join(root, "node", "bin", "node"), "--no-warnings",
		filepath.Join(root, "app", "dist", "main.js"), identity.Release, identity.Commit)
	cmd.Env = []string{"TERM=xterm-256color", "NO_COLOR=1"}
	cmd.Stdin, cmd.Stdout = slave, slave
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.ExtraFiles = []*os.File{childRequests, childReplies}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Process.Kill() }()
	require.NoError(t, childRequests.Close())
	require.NoError(t, childReplies.Close())
	var output terminalOutput
	go func() { _, _ = io.Copy(&output, master) }()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	reader := bufio.NewReader(requests)
	readRequest := func(method string) string {
		t.Helper()
		require.NoError(t, requests.SetReadDeadline(time.Now().Add(5*time.Second)))
		line, err := reader.ReadBytes('\n')
		require.NoError(t, err)
		var request struct{ ID, Method string }
		require.NoError(t, json.Unmarshal(line, &request))
		require.Equal(t, method, request.Method)
		return request.ID
	}
	writeReply := func(id string, value any) {
		t.Helper()
		data, err := json.Marshal(map[string]any{"version": 2, "id": id, "result": value})
		require.NoError(t, err)
		_, err = replies.Write(append(data, '\n'))
		require.NoError(t, err)
	}
	id := readRequest("hello")
	hello := map[string]any{
		"release": identity.Release, "commit": identity.Commit, "protocol": 2,
		"parent_pid":   strconv.Itoa(os.Getpid()),
		"capabilities": []string{"overview", "results", "details", "reveal", "export"},
		"color":        "never", "refresh_delay_ns": "5000000000", "timezone": "UTC",
		"profile": map[string]any{
			"state_dir": t.TempDir(), "config_path": nil,
			"suggestions": []any{
				map[string]any{"action": "sources", "command_text": "wpc sources"},
				map[string]any{"action": "crawl", "command_text": "wpc crawl"},
			},
		},
	}
	if mode != "overview" && mode != "cancel_before_start" {
		hello["unexpected"] = "private_reply_text"
	}
	writeReply(id, hello)
	if mode == "overview" {
		id = readRequest("overview")
		writeReply(id, map[string]any{"unexpected": "private_reply_text"})
	}
	if mode == "cancel_before_start" {
		driveEarlyCancellation(t, master, &output, readRequest, writeReply)
	}
	// A responsive parent closes replies after request EOF. Also prove exit with that pipe held open.
	if mode != "parent_keeps_reply_open" {
		go func() { _, _ = io.Copy(io.Discard, reader); _ = replies.Close() }()
	}
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-exited
		t.Fatal("UI rejection did not exit within its cleanup allowance")
	}
	want := 1
	if mode == "cancel_before_start" {
		want = 0
	}
	require.Equal(t, want, cmd.ProcessState.ExitCode())
	require.Regexp(t, `^(?:\x1b\[[0-?]*[ -/]*[@-~])*$`, stderr.String(), "raw errors must not escape")
	require.NotContains(t, output.text(), "private_reply_text")
	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	require.NoError(t, err)
	require.Equal(t, before, after)
	if mode == "overview" {
		require.Contains(t, output.text(), "\x1b[?1049l")
	}
}
