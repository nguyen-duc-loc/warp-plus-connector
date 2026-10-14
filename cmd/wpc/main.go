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
	var terminalWatchdog sync.Once
	var terminalResult atomic.Int32
	var terminalActive atomic.Bool
	terminalShutdown := func(result int) {
		terminalResult.Store(int32(result))
		terminalWatchdog.Do(
			func() {
				go func() {
					time.Sleep(5 * time.Second)
					code := exitCode.Load()
					if code == 0 {
						code = terminalResult.Load()
					}
					os.Exit(int(code))
				}()
			},
		)
	}
	ctx = terminal.WithLifecycle(
		ctx, func() { terminalActive.Store(true) }, terminalShutdown,
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
				terminalShutdown(1)
			}
		case <-ctx.Done():
		}
	}()
	cmd := cli.NewRootCommand(version, commit)
	cmd.SetIn(os.Stdin)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	err := cmd.ExecuteContext(ctx)
	if managed.Load() {
		return 0
	}
	if code := exitCode.Load(); code != 0 {
		cli.RenderFailure(cmd, err, int(code))
		return int(code)
	}
	if err != nil {
		cli.RenderFailure(cmd, err, 0)
		return 1
	}
	return 0
}
