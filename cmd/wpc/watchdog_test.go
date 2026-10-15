package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/cli"
	"github.com/stretchr/testify/require"
)

type watchdogWriter struct{ blocked bool }

type delayedWatchdogWriter struct{ entered chan struct{} }

func (w delayedWatchdogWriter) Write(p []byte) (int, error) {
	close(w.entered)
	time.Sleep(50 * time.Millisecond)
	return os.Stderr.Write(p)
}

func (w watchdogWriter) Write([]byte) (int, error) {
	if _, err := fmt.Fprintln(os.Stdout, "diagnostic write attempted"); err != nil {
		return 0, err
	}
	if w.blocked {
		select {}
	}
	return 0, errors.New("private destination failure")
}

// Spec 0010 AC-13: forced exit is a failure even if the UI child quit normally.
// An earlier recorded interruption still wins, and cleanup cannot reset the deadline.
func TestTerminalWatchdog(t *testing.T) {
	if recorded := os.Getenv("WPC_TEST_TERMINAL_WATCHDOG"); recorded != "" {
		code, err := strconv.Atoi(recorded)
		require.NoError(t, err)
		var interruption atomic.Int32
		interruption.Store(int32(code))
		root := cli.NewRootCommand("dev", "")
		root.SetContext(t.Context())
		root.SetErr(os.Stderr)
		require.NoError(t, root.PersistentFlags().Set("log-format", os.Getenv("WPC_TEST_TERMINAL_FORMAT")))
		require.NoError(t, root.PersistentFlags().Set("log-level", "error"))
		mode := os.Getenv("WPC_TEST_TERMINAL_WRITER")
		writeEntered := make(chan struct{})
		switch mode {
		case "blocked", "failed":
			root.SetErr(watchdogWriter{blocked: mode == "blocked"})
		case "completion during write":
			root.SetErr(delayedWatchdogWriter{entered: writeEntered})
		}
		report := cli.PrepareTerminalTimeout(root)
		reported := make(chan struct{})
		shutdown := terminalWatchdog{exitCode: &interruption, report: func() { report(); close(reported) }}
		shutdown.start(1)
		// The UI has quit successfully, but an owned read remains stuck.
		time.Sleep(time.Second)
		shutdown.start(0)
		if mode == "completion during write" {
			<-writeEntered
			os.Exit(shutdown.finish(0))
		}
		if mode == "late completion" {
			<-reported
			cli.RenderFailure(root, errors.New("private late failure"), 0)
			os.Exit(shutdown.finish(0))
		}
		select {}
	}
	for _, tc := range []struct {
		name     string
		recorded int
		want     int
		format   string
		writer   string
	}{
		{"normal child quit with stalled cleanup", 0, 1, "text", ""},
		{"recorded interrupt", 130, 130, "json", ""},
		{"recorded termination", 143, 143, "pretty", ""},
		{"blocked diagnostic output", 0, 1, "text", "blocked"},
		{"failed diagnostic output", 0, 1, "json", "failed"},
		{"normal completion loses after timeout", 0, 1, "json", "late completion"},
		{"normal completion during timeout write", 0, 1, "json", "completion during write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			executable, err := os.Executable()
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestTerminalWatchdog$")
			cmd.Env = append(os.Environ(), "WPC_TEST_TERMINAL_WATCHDOG="+strconv.Itoa(tc.recorded),
				"WPC_TEST_TERMINAL_FORMAT="+tc.format, "WPC_TEST_TERMINAL_WRITER="+tc.writer,
				"GORACE="+os.Getenv("GORACE")+" atexit_sleep_ms=0")
			started := time.Now()
			output, err := cmd.CombinedOutput()
			require.NoError(t, ctx.Err(), "watchdog did not end the process: %s", output)
			require.Error(t, err, "forced exit must not report successful cleanup")
			require.Equal(t, tc.want, cmd.ProcessState.ExitCode(), string(output))
			if tc.writer == "blocked" || tc.writer == "failed" {
				require.Equal(t, 1, strings.Count(string(output), "diagnostic write attempted"))
			} else {
				require.Equal(t, 1, strings.Count(string(output), "ui_shutdown_timeout"), string(output))
				require.Contains(t, string(output), "Terminal cleanup timed out")
				if tc.format == "json" {
					var record map[string]any
					require.NoError(t, json.Unmarshal(output, &record), "timeout output must be a complete record")
				}
			}
			require.NotContains(t, string(output), "private")
			require.Less(t, time.Since(started), 6*time.Second, "cleanup reset the first deadline")
		})
	}
}
