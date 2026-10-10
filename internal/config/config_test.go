package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/stretchr/testify/require"
)

func isolatedConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	return dir
}

// covers: spec 0003 AC-5. Resolve never creates state, and flags win over JSON.
func TestResolve_PrecedenceAndSharedRecoveryDirectory(t *testing.T) {
	dir := isolatedConfig(t)
	defaults, err := config.Resolve(config.Overrides{})
	require.NoError(t, err)
	require.Equal(t, 6*time.Hour, defaults.RecheckInterval)
	require.Equal(t, "unsupported", defaults.Validator)
	path := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"state_dir":"json-profile","recheck_interval":"2h"}`), 0600))
	fromJSON, err := config.Resolve(config.Overrides{ConfigPath: &path})
	require.NoError(t, err)
	require.Equal(t, 2*time.Hour, fromJSON.RecheckInterval)
	require.True(t, filepath.IsAbs(fromJSON.StateDir))
	require.Equal(t, "json-profile", filepath.Base(fromJSON.StateDir))
	profile, interval := filepath.Join(dir, "profile"), "3h"
	flags, err := config.Resolve(config.Overrides{ConfigPath: &path, StateDir: &profile, RecheckInterval: &interval})
	require.NoError(t, err)
	require.Equal(t, profile, flags.StateDir)
	require.Equal(t, 3*time.Hour, flags.RecheckInterval)
	require.Equal(t, defaults.ValidatorStateDir, flags.ValidatorStateDir)
	_, err = os.Stat(profile)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(defaults.StateDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// covers: spec 0003 AC-5, AC-6. Invalid settings cannot be masked by valid flags.
func TestResolve_RejectsInvalidPrivateJSON(t *testing.T) {
	dir := isolatedConfig(t)
	for _, body := range []string{
		`{`, `[]`, `null`, `{} {}`, `{"unknown":"secret"}`, `{"validator":null}`,
		`{"validator":42}`, `{"validator":"unsupported","validator":"unsupported"}`,
		`{"state_dir":""}`, `{"recheck_interval":"0s"}`, `{"recheck_interval":"-1h"}`,
		`{"recheck_interval":"tomorrow"}`, `{"validator":"secret"}`, strings.Repeat(" ", 64*1024+1),
	} {
		t.Run(strings.TrimSpace(body[:min(len(body), 80)]), func(t *testing.T) {
			path := filepath.Join(dir, "settings.json")
			require.NoError(t, os.WriteFile(path, []byte(body), 0600))
			profile, backend, interval := filepath.Join(dir, "uncreated"), "unsupported", "1h"
			_, err := config.Resolve(config.Overrides{ConfigPath: &path, StateDir: &profile, Validator: &backend, RecheckInterval: &interval})
			require.ErrorIs(t, err, config.ErrInvalid)
			require.NotContains(t, err.Error(), "secret")
			_, err = os.Stat(profile)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

// covers: spec 0003 AC-5. Explicit absence and unsafe files are errors.
func TestResolve_RejectsMissingPermissiveAndSymlinkedFiles(t *testing.T) {
	dir := isolatedConfig(t)
	path := filepath.Join(dir, "settings.json")
	_, err := config.Resolve(config.Overrides{ConfigPath: &path})
	require.ErrorIs(t, err, config.ErrAccess)
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0644))
	_, err = config.Resolve(config.Overrides{ConfigPath: &path})
	require.ErrorIs(t, err, config.ErrAccess)
	require.NoError(t, os.Chmod(path, 0600))
	link := filepath.Join(dir, "link.json")
	require.NoError(t, os.Symlink(path, link))
	_, err = config.Resolve(config.Overrides{ConfigPath: &link})
	require.ErrorIs(t, err, config.ErrAccess)
	_, err = config.Resolve(config.Overrides{ConfigPath: &dir})
	require.ErrorIs(t, err, config.ErrAccess)
}
