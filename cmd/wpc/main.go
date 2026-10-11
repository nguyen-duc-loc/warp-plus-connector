package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/cli"
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
	if code := exitCode.Load(); code != 0 {
		fmt.Fprintln(os.Stderr, "Error: cancelled")
		return int(code)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", cli.Diagnostic(err))
		return 1
	}
	return 0
}
