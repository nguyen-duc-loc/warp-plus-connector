package terminal

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

type lifecycleKey struct{}
type lifecycle struct {
	started    func()
	shutdown   func(int)
	signalCode func() int
}

// WithLifecycle binds the process owned five second watchdog and signal precedence.
func WithLifecycle(ctx context.Context, started func(), shutdown func(int), signalCode func() int) context.Context {
	return context.WithValue(ctx, lifecycleKey{}, lifecycle{started, shutdown, signalCode})
}

// Start marks admitted terminal startup so process signals also bound configuration and asset reads.
func Start(ctx context.Context) {
	if life, ok := ctx.Value(lifecycleKey{}).(lifecycle); ok && life.started != nil {
		life.started()
	}
}

// Admitted checks actual terminal descriptors before any configuration or state access.
func Admitted(in io.Reader, out io.Writer) bool {
	i, iok := in.(*os.File)
	o, ook := out.(*os.File)
	return runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") &&
		iok && ook && isatty.IsTerminal(i.Fd()) && isatty.IsTerminal(o.Fd()) &&
		os.Getenv("TERM") != "" && os.Getenv("TERM") != "dumb"
}

// Options fixes release identity, presentation, and the selected read boundary.
type Options struct {
	Release, Commit, Color string
	StateDir               string
	ConfigPath             *string
	Suggestions            [2]string
	Read                   func(context.Context) state.Overview
	Results                *app.ResultsSession
	Destination            ExportDestination
	Export                 ExportOperation
	ObserveExport          func(ExportReceipt)
}

// Run supervises one child. It restores the terminal before waiting for read cleanup.
func Run(parent context.Context, in, out *os.File, o Options) (err error) {
	root, err := bundle(o.Release, o.Commit)
	if err != nil {
		return err
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	restore, err := capture(in, out)
	if err != nil {
		return failure("ui_startup_failed")
	}
	defer restore()
	requests, childRequests, err := os.Pipe()
	if err != nil {
		return failure("ui_startup_failed")
	}
	defer func() { _ = requests.Close() }()
	defer func() { _ = childRequests.Close() }()
	childReplies, replies, err := os.Pipe()
	if err != nil {
		return failure("ui_startup_failed")
	}
	defer func() { _ = childReplies.Close() }()
	defer func() { _ = replies.Close() }()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	child := exec.Command(
		filepath.Join(root, "node", "bin", "node"),
		"--no-warnings", filepath.Join(root, "app", "dist", "main.js"), o.Release, o.Commit,
	)
	child.Dir = filepath.Join(root, "app")
	child.Stdin, child.Stdout, child.Stderr = in, out, io.Discard
	child.ExtraFiles = []*os.File{childRequests, childReplies}
	child.Env = []string{"NODE_NO_WARNINGS=1"}
	for _, key := range []string{"TERM", "COLORTERM", "LANG", "LC_ALL", "LC_CTYPE", "TZ"} {
		if value, ok := os.LookupEnv(key); ok {
			child.Env = append(child.Env, key+"="+value)
		}
	}
	if o.Color == "never" || os.Getenv("NO_COLOR") != "" {
		o.Color = "never"
		child.Env = append(child.Env, "NO_COLOR=1", "FORCE_COLOR=0")
	}
	if err = child.Start(); err != nil {
		return failure("ui_startup_failed")
	}
	_ = childRequests.Close()
	_ = childReplies.Close()
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	bridge := newResultsBridge(o)
	var work sync.WaitGroup
	var shutdownAt time.Time
	beginShutdown := func() {
		if shutdownAt.IsZero() {
			shutdownAt = time.Now()
			if life, ok := parent.Value(lifecycleKey{}).(lifecycle); ok && life.shutdown != nil {
				life.shutdown(1)
			}
		}
		cancel()
		_ = requests.Close()
		_ = replies.Close()
	}
	defer func() {
		beginShutdown()
		if life, ok := parent.Value(lifecycleKey{}).(lifecycle); ok && life.shutdown != nil {
			code := 1
			if err == nil {
				code = 0
			}
			life.shutdown(code)
		}
		work.Wait()
		if e := bridge.close(); e != nil {
			code := codeFor(err)
			if err == nil {
				code = codeFor(e)
			}
			err = &Error{Code: code, Remaining: bridge.session.Remaining}
		}
	}()
	var stopped bool
	defer func() {
		beginShutdown()
		if !stopped {
			sig := syscall.SIGTERM
			if life, ok := parent.Value(lifecycleKey{}).(lifecycle); ok && life.signalCode != nil && life.signalCode() == 130 {
				sig = syscall.SIGINT
			}
			_ = child.Process.Signal(sig)
			timer := time.NewTimer(time.Until(shutdownAt.Add(2 * time.Second)))
			defer timer.Stop()
			select {
			case <-exited:
			case <-timer.C:
				_ = child.Process.Kill()
			}
		}
		restore()
	}()
	type incoming struct {
		request request
		err     error
	}
	frames := make(chan incoming)
	go func() {
		reader := bufio.NewReaderSize(requests, requestLimit)
		previous := new(big.Int)
		for {
			r, err := readRequest(reader, previous)
			select {
			case frames <- incoming{r, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	hello := false
	ready := false
	overviewStarted := false
	busy := false
	completed := make(chan error, 1)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	lastIdentity := ""
	for {
		select {
		case fatal := <-bridge.fatal:
			return fatal
		case <-ctx.Done():
			return ctx.Err()
		case err := <-exited:
			stopped = true
			if err != nil || !ready {
				return failure("ui_child_failed")
			}
			return nil
		case <-deadline.C:
			if !overviewStarted {
				return failure("ui_startup_timeout")
			}
			return failure("ui_read_timeout")
		case err := <-completed:
			busy = false
			if overviewStarted {
				deadline.Stop()
			}
			if err != nil {
				return err
			}
			ready = true
		case frame := <-frames:
			if errors.Is(frame.err, io.EOF) {
				// EOF cancels entered work and begins bounded child cleanup immediately.
				beginShutdown()
				select {
				case err := <-exited:
					stopped = true
					if ready && err == nil {
						return nil
					}
				case <-time.After(time.Until(shutdownAt.Add(2 * time.Second))):
				}
				return failure("ui_child_failed")
			}
			if frame.err != nil {
				return frame.err
			}
			if busy {
				select {
				case err := <-completed:
					if overviewStarted {
						deadline.Stop()
					}
					if err != nil {
						return err
					}
					ready = true
				default:
					return failure("ui_protocol_invalid")
				}
			}
			r := frame.request
			if !hello {
				var p struct {
					Release  string `json:"release"`
					Commit   string `json:"commit"`
					Protocol int    `json:"protocol"`
				}
				if r.Method != "hello" || strictJSON(r.Params, &p) != nil {
					return failure("ui_protocol_invalid")
				}
				if p.Release != o.Release || p.Commit != o.Commit || p.Protocol != 2 {
					return failure("ui_version_mismatch")
				}
				hello = true
				busy = true
				work.Add(1)
				go func() { defer work.Done(); completed <- writeReply(replies, r.ID, helloResult(o)) }()
				continue
			}
			if r.Method != "overview" {
				if !overviewStarted {
					return failure("ui_protocol_invalid")
				}
				busy = true
				deadline.Reset(12 * time.Second)
				work.Add(1)
				go func() {
					defer work.Done()
					value, e := bridge.dispatch(ctx, r)
					if e != nil {
						code := codeFor(e)
						if code == "ui_protocol_invalid" {
							completed <- e
							return
						}
						value = record{"error_code": code}
					}
					completed <- writeReply(replies, r.ID, value)
				}()
				continue
			}
			var p struct{}
			if r.Method != "overview" || strictJSON(r.Params, &p) != nil || string(r.Params) == "null" {
				return failure("ui_protocol_invalid")
			}
			// The first overview proves the child accepted hello; writing hello alone does not.
			overviewStarted = true
			busy = true
			deadline.Reset(12 * time.Second)
			work.Add(1)
			go func() {
				defer work.Done()
				if bridge.active() {
					completed <- writeReply(replies, r.ID, record{"error_code": "ui_busy"})
					return
				}
				if bridge.session != nil {
					bridge.review = nil
					if e := bridge.session.CloseDetail(ctx); e != nil {
						completed <- writeReply(replies, r.ID, record{"error_code": codeFor(e)})
						return
					}
				}
				value := o.Read(ctx)
				if ctx.Err() != nil {
					completed <- ctx.Err()
					return
				}
				s := &value.Service
				if s.IdentityState == "verified" && s.InstallationID != nil {
					if *s.InstallationID == lastIdentity {
						s.IdentityCachePolicy = "retain"
					}
					lastIdentity = *s.InstallationID
				} else {
					lastIdentity = ""
				}
				completed <- writeReply(replies, r.ID, overviewResult(value))
			}()
		}
	}
}

func helloResult(o Options) any {
	zone, _ := time.Now().In(time.Local).Zone()
	return map[string]any{
		"release": o.Release, "commit": o.Commit, "protocol": 2,
		"parent_pid":   strconv.Itoa(os.Getpid()),
		"capabilities": []string{"overview", "results", "details", "reveal", "export"},
		"color":        o.Color, "refresh_delay_ns": "5000000000", "timezone": zone,
		"profile": map[string]any{
			"state_dir": o.StateDir, "config_path": o.ConfigPath,
			"suggestions": []any{
				map[string]any{"action": "sources", "command_text": o.Suggestions[0]},
				map[string]any{"action": "crawl", "command_text": o.Suggestions[1]},
			},
		},
	}
}
