package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/stretchr/testify/require"
)

// covers: spec 0007 AC-1. Defaults and explicit false flags retain independent lanes.
func TestResolve_ScheduleDefaultsAndOverrides(t *testing.T) {
	dir := isolatedConfig(t)
	s, err := config.Resolve(config.Overrides{})
	require.NoError(t, err)
	require.True(t, s.CrawlEnabled)
	require.True(t, s.ValidationEnabled)
	require.Equal(t, time.Hour, s.CrawlInterval)
	require.Equal(t, time.Hour, s.FailureInterval)
	path := filepath.Join(dir, "schedule.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"crawl_enabled":false,"validation_enabled":true,`+
		`"crawl_interval":"2h","failure_interval":"3h","recheck_interval":"4h"}`), 0600))
	yes, no, interval := true, false, "5h"
	s, err = config.Resolve(config.Overrides{
		ConfigPath: &path, CrawlEnabled: &yes, ValidationEnabled: &no, FailureInterval: &interval,
	})
	require.NoError(t, err)
	require.True(t, s.CrawlEnabled)
	require.False(t, s.ValidationEnabled)
	require.Equal(t, 2*time.Hour, s.CrawlInterval)
	require.Equal(t, 4*time.Hour, s.RecheckInterval)
	require.Equal(t, 5*time.Hour, s.FailureInterval)
	require.NoError(t, s.ValidateRun())
	_, err = os.Stat(s.StateDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// covers: spec 0007 AC-1. Bounds apply even when the corresponding lane is disabled.
func TestValidateRun_ScheduleIntervalBoundaries(t *testing.T) {
	isolatedConfig(t)
	base, err := config.Resolve(config.Overrides{})
	require.NoError(t, err)
	base.ValidationEnabled = false
	for _, field := range []string{"crawl", "routine", "failure"} {
		for _, d := range []time.Duration{
			0, time.Minute - time.Nanosecond, time.Minute, time.Minute + time.Nanosecond,
			720 * time.Hour, 720*time.Hour + time.Nanosecond,
		} {
			t.Run(field+"/"+d.String(), func(t *testing.T) {
				s := base
				switch field {
				case "crawl":
					s.CrawlInterval = d
				case "routine":
					s.RecheckInterval = d
				case "failure":
					s.FailureInterval = d
				}
				if d < time.Minute || d > 720*time.Hour {
					require.ErrorIs(t, s.ValidateRun(), config.ErrInvalid)
				} else {
					require.NoError(t, s.ValidateRun())
				}
			})
		}
	}
	base.CrawlEnabled = false
	require.ErrorIs(t, base.ValidateRun(), config.ErrScheduleDisabled)
}

// covers: spec 0007 AC-1. Malformed JSON is rejected before flag overrides.
func TestResolve_RejectsMalformedScheduleValues(t *testing.T) {
	dir := isolatedConfig(t)
	path := filepath.Join(dir, "schedule.json")
	for _, field := range []string{"crawl_enabled", "validation_enabled", "crawl_interval", "failure_interval"} {
		for _, value := range []string{"null", "42", `"private_secret"`} {
			t.Run(field+"/"+value, func(t *testing.T) {
				body := fmt.Sprintf(`{%q:%s}`, field, value)
				require.NoError(t, os.WriteFile(path, []byte(body), 0600))
				yes, interval := true, "1h"
				_, err := config.Resolve(config.Overrides{
					ConfigPath: &path, CrawlEnabled: &yes, ValidationEnabled: &yes,
					CrawlInterval: &interval, FailureInterval: &interval,
				})
				require.ErrorIs(t, err, config.ErrInvalid)
				require.NotContains(t, err.Error(), "private_secret")
			})
		}
	}
}

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
		t.Run(
			strings.TrimSpace(body[:min(len(body), 80)]), func(t *testing.T) {
				path := filepath.Join(dir, "settings.json")
				require.NoError(t, os.WriteFile(path, []byte(body), 0600))
				profile, backend, interval := filepath.Join(dir, "uncreated"), "unsupported", "1h"
				_, err := config.Resolve(
					config.Overrides{
						ConfigPath: &path, StateDir: &profile, Validator: &backend, RecheckInterval: &interval,
					},
				)
				require.ErrorIs(t, err, config.ErrInvalid)
				require.NotContains(t, err.Error(), "secret")
				_, err = os.Stat(profile)
				require.ErrorIs(t, err, os.ErrNotExist)
			},
		)
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
