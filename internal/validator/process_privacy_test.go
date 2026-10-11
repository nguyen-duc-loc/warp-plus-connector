package validator

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type processPrivacy struct {
	Input            string
	Host             string
	DockerContext    string
	KeyInArguments   bool
	KeyInEnvironment bool
}

// The real subprocess boundary carries candidate input only through stdin and
// removes Docker environment settings that could redirect the selected endpoint.
func TestDockerCommandPrivateInputAndExplicitEndpoint(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") +
		"' -test.run=^TestDockerCommandPrivacyHelper$ -- \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WPC_DOCKER_PRIVACY_HELPER", "1")
	t.Setenv("DOCKER_HOST", "tcp://untrusted.invalid:2375")
	t.Setenv("DOCKER_CONTEXT", "untrusted")
	input := []byte(`{"session_id":"0123456789abcdefghijklmnop","key":"ABCDEFGH-12345678-AbCdEf01"}`)
	data, err := dockerCommand(context.Background(), input,
		"--host", "unix:///tmp/docker.sock", "exec", "-i", testContainer, "/usr/local/bin/wpc-worker", "initialize")
	require.NoError(t, err)
	var got processPrivacy
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, string(input), got.Input)
	require.Empty(t, got.Host)
	require.Empty(t, got.DockerContext)
	require.False(t, got.KeyInArguments)
	require.False(t, got.KeyInEnvironment)
}

func TestDockerCommandPrivacyHelper(t *testing.T) {
	if os.Getenv("WPC_DOCKER_PRIVACY_HELPER") != "1" {
		return
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	result := processPrivacy{
		Input: string(input), Host: os.Getenv("DOCKER_HOST"), DockerContext: os.Getenv("DOCKER_CONTEXT"),
	}
	for _, arg := range os.Args {
		result.KeyInArguments = result.KeyInArguments || strings.Contains(arg, "ABCDEFGH-12345678-AbCdEf01")
	}
	for _, entry := range os.Environ() {
		result.KeyInEnvironment = result.KeyInEnvironment || strings.Contains(entry, "ABCDEFGH-12345678-AbCdEf01")
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
