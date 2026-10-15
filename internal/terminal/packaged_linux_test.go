package terminal_test

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	for _, mode := range []string{
		"quit", "interrupt", "terminate", "hangup", "child_crash", "absent", "scheduled",
		"results", "results_compact", "results_failure", "controls", "controls_compact", "controls_cancel",
	} {
		t.Run(
			mode, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "state")
				if mode != "absent" {
					if strings.HasPrefix(mode, "results") {
						seedOverview(t, dir, 338)
					} else {
						seedOverview(t, dir)
					}
				}
				if mode == "scheduled" {
					seedOverviewSchedule(t, dir)
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
				size := &unix.Winsize{Row: 30, Col: 110}
				if mode == "results_compact" || mode == "controls_compact" {
					size = &unix.Winsize{Row: 16, Col: 60}
				}
				require.NoError(
					t,
					unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, size),
				)
				cmd := exec.Command(executable, "--state-dir", dir, "--color", "never")
				cmd.Env = append(os.Environ(), "TERM=xterm-256color", "NO_COLOR=1", "NODE_OPTIONS=--invalid-option")
				if mode == "controls_cancel" {
					proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodConnect {
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						select {
						case <-r.Context().Done():
						case <-time.After(10 * time.Second):
						}
					}))
					t.Cleanup(func() { proxy.CloseClientConnections(); proxy.Close() })
					cmd.Env = append(cmd.Env, "HTTPS_PROXY="+proxy.URL, "NO_PROXY=", "no_proxy=")
				}
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
					if strings.HasPrefix(mode, "results") {
						seen("338 TOTAL CANDIDATES")
					} else {
						seen("105 TOTAL CANDIDATES")
					}
				}
				seen("Overview  | Ready")
				want := 0
				switch mode {
				case "controls_cancel":
					verifyPackagedCancellation(t, master, &output)
				case "controls", "controls_compact":
					verifyPackagedControls(t, dir, master, &output)
				case "results", "results_compact", "results_failure":
					verifyPackagedResults(t, executable, dir, master, &output, mode)
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
				case "scheduled":
					_, err = master.WriteString("\t")
					require.NoError(t, err)
					seen("> Recent work")
					_, err = master.WriteString("\t")
					require.NoError(t, err)
					seen("> Schedule")
					seen("Crawl due")
					seen("| completed")
					seen("Validation wait provider_cooldown")
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
				case "hangup":
					want = 1
					require.NoError(t, cmd.Process.Signal(syscall.SIGHUP))
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
				if !strings.HasPrefix(mode, "results") {
					require.NotContains(t, output.text(), "synthetic_private_key")
				}
				require.NotRegexp(t, `\x1b\[[0-9;]*m`, output.text(), "no decorative color")
				if mode == "absent" {
					_, err := os.Stat(dir)
					require.True(t, os.IsNotExist(err))
				}
			},
		)
	}
	for _, mode := range []string{"hello", "overview", "parent_keeps_reply_open"} {
		t.Run("malformed_"+mode, func(t *testing.T) { verifyChildProtocol(t, executable, mode) })
	}
	t.Run("cancel_before_start", func(t *testing.T) { verifyChildProtocol(t, executable, "cancel_before_start") })
	t.Run("startup_deadline", func(t *testing.T) { verifyStartupDeadline(t, executable) })
}

// Seed through the writer so the UI must accept the actual saved schedule vocabulary.
func seedOverviewSchedule(t *testing.T, dir string) {
	t.Helper()
	w, err := store.OpenWriter(t.Context(), dir, time.Now)
	require.NoError(t, err)
	at := time.Now().UTC().Truncate(time.Microsecond)
	_, err = w.PrepareSchedule(
		t.Context(), at, state.DefaultSchedulePolicy(),
		[]state.SourcePlan{{Channel: "warpplus", PageLimit: 1}}, false,
	)
	require.NoError(t, err)
	r, err := w.StartCrawl(
		t.Context(), state.CrawlInput{
			RequestID: rand.Text(), StartedAt: at, SourceID: "telegram:warpplus",
			RequestedURL: "https://t.me/s/warpplus", CoverageMode: "latest_preview", PageLimit: 1,
		},
	)
	require.NoError(t, err)
	_, err = w.RecordCrawlPage(t.Context(), state.CrawlPage{RunID: r.Run.ID, FetchedAt: at})
	require.NoError(t, err)
	_, err = w.FinishRun(
		t.Context(), state.RunCompletion{
			RunID: r.Run.ID, Status: state.Completed, FinishedAt: at,
		},
	)
	require.NoError(t, err)
	require.NoError(
		t, w.SaveValidationWait(
			t.Context(), at, &state.ValidationWait{
				Reason: "provider_cooldown", BasisAt: at, RetryAt: at.Add(time.Hour),
				ObservedAt: at, Interval: time.Hour,
			}, false,
		),
	)
	s, err := w.GetSchedule(t.Context(), 0, time.Now)
	require.NoError(t, err)
	require.NotNil(t, s.Crawl)
	require.Equal(t, "completed", s.Crawl.Reason)
	require.NoError(t, w.Close())
}

func seedOverview(t *testing.T, dir string, count ...int) {
	t.Helper()
	w, err := store.OpenWriter(t.Context(), dir, time.Now)
	require.NoError(t, err)
	size := 105
	if len(count) > 0 {
		size = count[0]
	}
	keys := make([]string, size)
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
