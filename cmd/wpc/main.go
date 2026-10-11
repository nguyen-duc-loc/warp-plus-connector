package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/cli"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/service"
)

// Release builds supply these values through the Go linker.
var (
	version = "dev"
	commit  = ""
)

func main() {
	os.Exit(run())
}

func run() int {
	// Let broken output writes return errors so batch cleanup and finalization can run.
	signal.Ignore(syscall.SIGPIPE)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	var exitCode atomic.Int32
	var managed atomic.Bool
	var watchdog sync.Once
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
		case <-ctx.Done():
		}
	}()
	cmd := cli.NewRootCommand(version, commit)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	err := cmd.ExecuteContext(ctx)
	if managed.Load() {
		return 0
	}
	var failed interface{ DiagnosticFailed() bool }
	diagnosticOK := !errors.As(err, &failed) || !failed.DiagnosticFailed()
	if code := exitCode.Load(); code != 0 {
		if diagnosticOK {
			fmt.Fprintln(os.Stderr, "Error: cancelled")
		}
		return int(code)
	}
	if err != nil {
		if diagnosticOK {
			fmt.Fprintln(os.Stderr, "Error:", cli.Diagnostic(err))
		}
		return 1
	}
	return 0
}
