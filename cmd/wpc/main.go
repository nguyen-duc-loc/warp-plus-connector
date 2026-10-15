package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/cli"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/service"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/terminal"
)

// Release builds supply these values through the Go linker.
var (
	version = "dev"
	commit  = ""
)

func main() {
	os.Exit(run())
}

const (
	terminalPending uint32 = iota
	terminalForced
	terminalFinished
)

// terminalWatchdog keeps final output inside the first shutdown deadline.
type terminalWatchdog struct {
	once       sync.Once
	phase      atomic.Uint32
	exitCode   *atomic.Int32
	report     func()
	reportDone chan struct{}
}

func (w *terminalWatchdog) start(_ int) {
	w.once.Do(func() {
		w.reportDone = make(chan struct{})
		deadline := time.Now().Add(5 * time.Second)
		go func() {
			// Reserve the last 100 milliseconds for output, without extending process lifetime.
			time.Sleep(time.Until(deadline.Add(-100 * time.Millisecond)))
			if !w.phase.CompareAndSwap(terminalPending, terminalForced) {
				return
			}
			go func() {
				defer close(w.reportDone)
				if w.report != nil {
					w.report()
				}
			}()
			time.Sleep(time.Until(deadline))
			os.Exit(w.forcedExitCode())
		}()
	})
}

func (w *terminalWatchdog) forcedExitCode() int {
	if code := w.exitCode.Load(); code != 0 {
		return int(code)
	}
	return 1
}

// finish lets normal completion win only after its final output has finished.
func (w *terminalWatchdog) finish(code int) int {
	if w.phase.CompareAndSwap(terminalPending, terminalFinished) {
		return code
	}
	if w.phase.Load() == terminalForced {
		// The independent watchdog still exits on time if the original report is blocked.
		<-w.reportDone
		return w.forcedExitCode()
	}
	return code
}

func run() int {
	// Let broken output writes return errors so batch cleanup and finalization can run.
	signal.Ignore(syscall.SIGPIPE)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	hangups := make(chan os.Signal, 1)
	defer signal.Stop(hangups)
	var exitCode atomic.Int32
	var managed atomic.Bool
	var watchdog sync.Once
	var terminalActive atomic.Bool
	cmd := cli.NewRootCommand(version, commit)
	cmd.SetIn(os.Stdin)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	var timeoutReporter atomic.Pointer[func()]
	reportTimeout := func() {
		if report := timeoutReporter.Load(); report != nil {
			(*report)()
		}
	}
	terminalDeadline := terminalWatchdog{exitCode: &exitCode, report: reportTimeout}
	ctx = terminal.WithLifecycle(
		ctx, func() {
			report := cli.PrepareTerminalTimeout(cmd)
			timeoutReporter.Store(&report)
			// Only admitted UI sessions replace the default hangup behavior.
			signal.Notify(hangups, syscall.SIGHUP)
			terminalActive.Store(true)
		}, terminalDeadline.start,
		func() int { return int(exitCode.Load()) },
	)
	ctx = service.WithLifecycle(
		ctx, func() { managed.Store(true) }, func() {
			watchdog.Do(func() { go func() { time.Sleep(75 * time.Second); os.Exit(0) }() })
		}, func() int { return int(exitCode.Load()) },
	)
	go func() {
		select {
		case sig := <-signals:
			code := int32(130)
			if sig == syscall.SIGTERM {
				code = 143
			}
			exitCode.Store(code)
			cancel()
			if terminalActive.Load() {
				terminalDeadline.start(1)
			}
		case <-hangups:
			cancel()
			terminalDeadline.start(1)
		case <-ctx.Done():
		}
	}()
	err := cmd.ExecuteContext(ctx)
	if managed.Load() {
		return 0
	}
	code := int(exitCode.Load())
	if code == 0 && err != nil {
		code = 1
	}
	if terminalActive.Load() {
		terminalDeadline.start(code)
	}
	if terminalDeadline.phase.Load() != terminalForced {
		cli.RenderFailure(cmd, err, int(exitCode.Load()))
	}
	return terminalDeadline.finish(code)
}
