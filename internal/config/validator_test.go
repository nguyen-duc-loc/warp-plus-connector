package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDockerSettingsRequireImmutableImageAndLocalSocket(t *testing.T) {
	image := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{
			"configured",
			`{"validator":"docker","validator_image":"` + image + `","validator_socket":"/tmp/docker.sock"}`, true,
		},
		{"missing_image", `{"validator":"docker"}`, false},
		{"mutable_tag", `{"validator":"docker","validator_image":"wpc-validator:latest"}`, false},
		{
			"remote_socket",
			`{"validator":"docker","validator_image":"` + image + `","validator_socket":"tcp://remote:2375"}`, false,
		},
		{
			"relative_socket",
			`{"validator":"docker","validator_image":"` + image + `","validator_socket":"docker.sock"}`, false,
		},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "config.json")
				require.NoError(t, os.WriteFile(path, []byte(tc.body), 0600))
				stateDir := filepath.Join(dir, "uncreated")
				settings, err := config.Resolve(config.Overrides{ConfigPath: &path, StateDir: &stateDir})
				if tc.valid {
					require.NoError(t, err)
					require.Equal(t, "docker", settings.Validator)
					require.Equal(t, image, settings.ValidatorImage)
					require.NotEqual(t, filepath.Join(stateDir, "validator"), settings.ValidatorStateDir)
				} else {
					require.ErrorIs(t, err, config.ErrInvalid)
				}
				_, err = os.Stat(stateDir)
				require.ErrorIs(t, err, os.ErrNotExist)
			},
		)
	}
}
