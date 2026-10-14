package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type metadataWriter struct {
	bytes.Buffer
	writes int
	first  func([]byte) (int, error)
}

func (w *metadataWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		return w.first(p)
	}
	return w.Buffer.Write(p)
}

// Spec 0009 AC-6 and AC-10: failed metadata delivery suppresses final errors and signal warnings.
func TestResults_FailedDiagnosticStreamIsNotRetried(t *testing.T) {
	configPath, stateDir, key := seedOutputState(t)
	for _, command := range []string{"list", "keys", "named"} {
		for _, format := range []string{"pretty", "text", "json"} {
			for _, fault := range []struct {
				name     string
				short    bool
				exitCode int
				full     bool
			}{
				{name: "broken"}, {name: "short", short: true},
				{name: "interrupt", exitCode: 130},
				{name: "terminate_short", short: true, exitCode: 143},
				{name: "full_write_with_error", full: true},
			} {
				t.Run(
					command+"/"+format+"/"+fault.name, func(t *testing.T) {
						ctx, cancel := context.WithCancel(t.Context())
						defer cancel()
						stderr := &metadataWriter{
							first: func(p []byte) (int, error) {
								if fault.exitCode != 0 {
									cancel()
								}
								if fault.short {
									return len(p) - 1, nil
								}
								if fault.full {
									return len(p), errors.New("private writer error: credential=synthetic-secret")
								}
								return 0, io.ErrClosedPipe
							},
						}
						var stdout bytes.Buffer
						cmd := NewRootCommand("test", "")
						cmd.SetOut(&stdout)
						cmd.SetErr(stderr)
						destination := filepath.Join(t.TempDir(), "result.json")
						args := []string{"list"}
						switch command {
						case "keys":
							args = []string{"export", "--format", "keys"}
						case "named":
							args = []string{"export", "--output", destination}
						}
						cmd.SetArgs(
							append(
								args,
								"--config",
								configPath,
								"--state-dir",
								stateDir,
								"--log-format",
								format,
							),
						)
						err := cmd.ExecuteContext(ctx)
						require.Error(t, err)
						require.Equal(t, 1, stderr.writes)
						if fault.exitCode != 0 {
							require.ErrorIs(t, err, context.Canceled)
						} else {
							require.Contains(t, Diagnostic(err), "output_error")
						}
						require.NotContains(t, Diagnostic(err), "synthetic-secret")
						require.NotContains(t, err.Error(), "synthetic-secret")
						if command == "named" {
							require.Contains(t, Diagnostic(err), "published=true")
							data, readErr := os.ReadFile(destination)
							require.NoError(t, readErr)
							var doc struct {
								Items []json.RawMessage `json:"items"`
							}
							require.NoError(t, json.Unmarshal(data, &doc))
							require.Len(t, doc.Items, 1)
							require.NotContains(t, string(data), key)
							require.Empty(t, stdout.String())
						} else {
							require.Contains(t, Diagnostic(err), "delivered=true")
							require.NotEmpty(t, stdout.String())
						}
						RenderFailure(cmd, fmt.Errorf("result delivery: %w", err), fault.exitCode)
						require.Equal(t, 1, stderr.writes, "a failed stream must not receive the final diagnostic")
						require.Empty(t, stderr.String())
					},
				)
			}
		}
	}
}

// Spec 0009 AC-6 and AC-9: failed results retain delivery facts on working stderr.
func TestResults_StdoutFailureStillReportsDiagnostic(t *testing.T) {
	configPath, stateDir, _ := seedOutputState(t)
	stdout := &metadataWriter{first: func([]byte) (int, error) { return 1, io.ErrClosedPipe }}
	var stderr bytes.Buffer
	cmd := NewRootCommand("test", "")
	cmd.SetOut(stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"export", "--config", configPath, "--state-dir", stateDir, "--log-format", "json"})
	err := cmd.ExecuteContext(t.Context())
	require.Error(t, err)
	require.Empty(t, stderr.String())
	RenderFailure(cmd, err, 0)
	var record struct {
		Code      string `json:"code"`
		Delivered bool   `json:"delivered"`
	}
	require.NoError(t, json.Unmarshal(stderr.Bytes(), &record))
	require.Equal(t, "output_error", record.Code)
	require.True(t, record.Delivered)
	require.Equal(t, 1, stdout.writes)
}

// Spec 0009 AC-6: cancellation after successful metadata delivery still explains the signal exit.
func TestResults_CancellationKeepsWorkingDiagnostics(t *testing.T) {
	configPath, stateDir, _ := seedOutputState(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stderr := &metadataWriter{
		first: func(p []byte) (int, error) {
			cancel()
			return len(p), nil
		},
	}
	cmd := NewRootCommand("test", "")
	cmd.SetOut(io.Discard)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"list", "--config", configPath, "--state-dir", stateDir, "--log-format", "json"})
	err := cmd.ExecuteContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, Diagnostic(err), "delivered=true")
	RenderFailure(cmd, err, 130)
	require.Equal(t, 2, stderr.writes)
	var record struct {
		Event    string `json:"event"`
		ExitCode int    `json:"exit_code"`
	}
	require.NoError(t, json.Unmarshal(stderr.Bytes(), &record))
	require.Equal(t, "command_cancelled", record.Event)
	require.Equal(t, 130, record.ExitCode)
}

// Spec 0009 AC-6, AC-9, AC-10: cancellation alone does not mark healthy stderr as failed.
// Signal warnings remain visible at error verbosity, before or after metadata delivery.
func TestResults_CancellationAtDeliveryBoundaryKeepsSignalDiagnostic(t *testing.T) {
	configPath, stateDir, key := seedOutputState(t)
	for _, scenario := range []struct {
		name     string
		command  string
		metadata bool
	}{
		{"list before metadata", "list", false},
		{"keys before metadata", "keys", false},
		{"list after metadata", "list", true},
		{"keys after metadata", "keys", true},
		{"named after metadata", "named", true},
	} {
		for _, exitCode := range []int{130, 143} {
			t.Run(fmt.Sprintf("%s/%d", scenario.name, exitCode), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				stdout, stderr := &metadataWriter{}, &metadataWriter{}
				stdout.first = func(p []byte) (int, error) {
					n, err := stdout.Buffer.Write(p)
					if !scenario.metadata {
						cancel()
					}
					return n, err
				}
				stderr.first = func(p []byte) (int, error) {
					n, err := stderr.Buffer.Write(p)
					if scenario.metadata {
						cancel()
					}
					return n, err
				}
				destination := filepath.Join(t.TempDir(), "result.json")
				args := []string{"list"}
				switch scenario.command {
				case "keys":
					args = []string{"export", "--format", "keys"}
				case "named":
					args = []string{"export", "--output", destination}
				}
				cmd := NewRootCommand("test", "")
				cmd.SetOut(stdout)
				cmd.SetErr(stderr)
				cmd.SetArgs(append(args, "--config", configPath, "--state-dir", stateDir,
					"--log-format", "json", "--log-level", "error"))

				err := cmd.ExecuteContext(ctx)
				require.ErrorIs(t, err, context.Canceled)
				if scenario.command == "named" {
					require.Contains(t, Diagnostic(err), "published=true")
					data, readErr := os.ReadFile(destination)
					require.NoError(t, readErr)
					require.True(t, json.Valid(data))
					require.NotContains(t, string(data), key)
					require.Empty(t, stdout.String())
				} else {
					require.Contains(t, Diagnostic(err), "delivered=true")
					require.NotEmpty(t, stdout.String())
					if scenario.command == "keys" {
						require.Equal(t, key+"\n", stdout.String())
					} else {
						require.NotContains(t, stdout.String(), key)
					}
				}
				metadata := append([]byte(nil), stderr.Bytes()...)
				if scenario.metadata {
					require.Equal(t, 1, stderr.writes)
					require.Contains(t, string(metadata), "returned_count=1 limit=none truncated=false")
				} else {
					require.Zero(t, stderr.writes)
				}
				result := stdout.String()
				RenderFailure(cmd, fmt.Errorf("result delivery: %w", err), exitCode)
				var record struct {
					Event    string `json:"event"`
					Level    string `json:"level"`
					ExitCode int    `json:"exit_code"`
				}
				require.NoError(t, json.Unmarshal(stderr.Bytes()[len(metadata):], &record))
				require.Equal(t, "command_cancelled", record.Event)
				require.Equal(t, "warn", record.Level)
				require.Equal(t, exitCode, record.ExitCode)
				require.NotContains(t, stderr.String(), key)
				require.Equal(t, result, stdout.String())
			})
		}
	}
}
