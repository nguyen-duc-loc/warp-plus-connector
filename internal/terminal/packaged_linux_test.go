package terminal_test

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type terminalOutput struct {
	sync.Mutex
	bytes.Buffer
}

func (b *terminalOutput) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}
func (b *terminalOutput) text() string {
	b.Lock()
	defer b.Unlock()
	return b.String()
}

// Spec 0010 AC-1, AC-3, AC-8 through AC-15: run the actual archive with real PTYs.
// The caller supplies a matching packaged executable, never a host Node fallback.
func TestPackagedTerminal(t *testing.T) {
	executable := os.Getenv("WPC_UI_PACKAGE")
	if executable == "" {
		t.Skip("set WPC_UI_PACKAGE to a matching packaged Linux executable")
	}
	for _, mode := range []string{"quit", "interrupt", "terminate", "child_crash", "absent"} {
		t.Run(
			mode, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "state")
				if mode != "absent" {
					seedOverview(t, dir)
				}
				master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
				require.NoError(t, err)
				t.Cleanup(func() { _ = master.Close() })
				require.NoError(t, unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0))
				number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
				require.NoError(t, err)
				slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
				require.NoError(t, err)
				defer func() { _ = slave.Close() }()
				before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
				require.NoError(t, err)
				require.NoError(
					t,
					unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 110}),
				)
				cmd := exec.Command(executable, "--state-dir", dir, "--color", "never")
				cmd.Env = append(os.Environ(), "TERM=xterm-256color", "NO_COLOR=1", "NODE_OPTIONS=--invalid-option")
				cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
				cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
				require.NoError(t, cmd.Start())
				t.Cleanup(func() { _ = cmd.Process.Kill() })
				var output terminalOutput
				go func() { _, _ = io.Copy(&output, master) }()
				wait := make(chan error, 1)
				go func() { wait <- cmd.Wait() }()
				seen := func(needle string) {
					t.Helper()
					require.Eventually(
						t, func() bool { return strings.Contains(output.text(), needle) },
						10*time.Second, 10*time.Millisecond, "missing %q in %s", needle, output.text(),
					)
				}
				if mode == "absent" {
					seen("No saved collection yet")
				} else {
					seen("105 TOTAL CANDIDATES")
				}
				seen("Overview  | Ready")
				want := 0
				switch mode {
				case "quit":
					_, err = master.WriteString("\t")
					require.NoError(t, err)
					seen("> Recent work")
					_, err = master.WriteString("?")
					require.NoError(t, err)
					seen("Keyboard help")
					_, err = master.WriteString("?")
					require.NoError(t, err)
					require.NoError(
						t,
						unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 14, Col: 45}),
					)
					seen("Resize to at least")
					require.NoError(
						t,
						unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 110}),
					)
					refreshStart := len(output.text())
					_, err = master.WriteString("r")
					require.NoError(t, err)
					// A later refresh must complete and preserve section focus.
					require.Eventually(
						t, func() bool {
							text := output.text()[refreshStart:]
							start := strings.Index(text, "Overview  | Refreshing")
							return start >= 0 && strings.Contains(text[start:], "Overview  | Ready")
						},
						10*time.Second, 10*time.Millisecond,
					)
					_, err = master.WriteString("q")
					require.NoError(t, err)
				case "absent":
					_, err = master.WriteString("q")
					require.NoError(t, err)
				case "interrupt":
					want = 130
					_, err = master.Write([]byte{3})
					require.NoError(t, err)
				case "terminate":
					want = 143
					require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
				case "child_crash":
					want = 1
					paths, err := filepath.Glob(fmt.Sprintf("/proc/%d/task/*/children", cmd.Process.Pid))
					require.NoError(t, err)
					var fields []string
					for _, path := range paths {
						children, err := os.ReadFile(path)
						require.NoError(t, err)
						fields = append(fields, strings.Fields(string(children))...)
					}
					require.Len(t, fields, 1)
					pid, err := strconv.Atoi(fields[0])
					require.NoError(t, err)
					require.NoError(t, syscall.Kill(pid, syscall.SIGKILL))
				}
				select {
				case <-wait:
				case <-time.After(6 * time.Second):
					t.Fatal("terminal shutdown exceeded its deadline")
				}
				require.Equal(t, want, cmd.ProcessState.ExitCode(), output.text())
				after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
				require.NoError(t, err)
				require.Equal(t, before, after, "original terminal settings must be restored")
				seen("\x1b[?1049l")
				require.NotContains(t, output.text(), "synthetic_private_key")
				require.NotRegexp(t, `\x1b\[[0-9;]*m`, output.text(), "no decorative color")
				if mode == "absent" {
					_, err := os.Stat(dir)
					require.True(t, os.IsNotExist(err))
				}
			},
		)
	}
}

func seedOverview(t *testing.T, dir string) {
	t.Helper()
	w, err := store.OpenWriter(t.Context(), dir, time.Now)
	require.NoError(t, err)
	keys := make([]string, 105)
	for i := range keys {
		keys[i] = fmt.Sprintf("synthetic_private_key_%d", i)
	}
	r, err := w.StartRun(t.Context(), state.RunInput{RequestID: rand.Text(), Kind: state.Crawl, StartedAt: time.Now()})
	require.NoError(t, err)
	_, err = w.RecordMessage(
		t.Context(), state.MessageInput{
			RunID: r.ID, SourceID: "fixture_source",
			MessageID: "1", ObservedAt: time.Now(), Keys: keys,
		},
	)
	require.NoError(t, err)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{RunID: r.ID, Status: state.Completed, FinishedAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, w.Close())
}
