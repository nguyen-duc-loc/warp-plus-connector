package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
)

// Spec 0004 AC-1 through AC-3: explicit replacement, strict types and selection.
func TestSourcesConfigurationAndSelection(t *testing.T) {
	dir := isolatedConfig(t)
	defaults, err := config.Resolve(config.Overrides{})
	require.NoError(t, err)
	require.Equal(
		t,
		[]config.Source{
			{Channel: "warpplus", Enabled: true, PageLimit: 1}, {Channel: "warppluscn", Enabled: true, PageLimit: 1},
			{Channel: "warpplushome", Enabled: true, PageLimit: 1}, {
				Channel: "warp_veyke",
				Enabled: true, PageLimit: 1,
			},
		},
		defaults.Sources,
	)
	path := filepath.Join(dir, "settings.json")
	require.NoError(
		t,
		os.WriteFile(
			path,
			[]byte(`{"crawl_page_limit":3,"sources":[{"channel":"First"},`+
				`{"channel":"Second","page_limit":5},{"channel":"Off",`+
				`"enabled":false}]}`),
			0600,
		),
	)
	settings, err := config.Resolve(config.Overrides{ConfigPath: &path})
	require.NoError(t, err)
	plan, err := settings.SelectSources([]string{"SECOND", "first"}, nil)
	require.NoError(t, err)
	require.Equal(t, []state.SourcePlan{{Channel: "first", PageLimit: 3}, {Channel: "second", PageLimit: 5}}, plan)
	n := 7
	plan, err = settings.SelectSources(nil, &n)
	require.NoError(t, err)
	require.Equal(t, 7, plan[0].PageLimit)
	require.Equal(t, 7, plan[1].PageLimit)
	for _, names := range [][]string{{""}, {"first", "FIRST"}, {"off"}, {"unknown"}, {"first,second"}} {
		_, err = settings.SelectSources(names, nil)
		require.ErrorIs(t, err, config.ErrSelectionInvalid)
	}
	for _, body := range []string{
		`{"sources":null}`, `{"sources":[null]}`, `{"sources":[{"channel":"x","enabled":null}]}`,
		`{"sources":[{"channel":"x","page_limit":null}]}`,
		`{"sources":[{"channel":"x","channel":"y"}]}`, `{"sources":[{"channel":"X"},{"channel":"x","enabled":false}]}`,
		`{"sources":[{"channel":"@x"}]}`, `{"sources":[{"channel":"x","unknown":true}]}`, `{"crawl_page_limit":null}`,
		`{"crawl_page_limit":1.5}`, `{"crawl_page_limit":"2"}`, `{"crawl_page_limit":101}`,
	} {
		require.NoError(t, os.WriteFile(path, []byte(body), 0600))
		_, err := config.Resolve(config.Overrides{ConfigPath: &path})
		require.ErrorIs(t, err, config.ErrInvalid, body)
	}
	require.NoError(t, os.WriteFile(path, []byte(`{"sources":[]}`), 0600))
	settings, err = config.Resolve(config.Overrides{ConfigPath: &path})
	require.NoError(t, err)
	require.Empty(t, settings.Sources)
	_, err = settings.SelectSources(nil, nil)
	require.ErrorIs(t, err, config.ErrSelectionEmpty)
}

// covers: spec 0004 AC-1, AC-2. Limits count disabled entries and validate them too.
func TestResolve_SourceBoundaries(t *testing.T) {
	dir := isolatedConfig(t)
	path := filepath.Join(dir, "settings.json")
	entries := make([]string, 32)
	for i := range entries {
		entries[i] = fmt.Sprintf(`{"channel":"source%d","enabled":false}`, i)
	}
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"maximum channels", `{"sources":[` + strings.Join(entries, ",") + `]}`, true},
		{
			"too many disabled channels",
			`{"sources":[` + strings.Join(entries, ",") + `,{"channel":"extra","enabled":false}]}`, false,
		},
		{"maximum channel length", `{"sources":[{"channel":"` + strings.Repeat("A", 64) + `"}]}`, true},
		{"excessive channel length", `{"sources":[{"channel":"` + strings.Repeat("a", 65) + `"}]}`, false},
		{"empty channel", `{"sources":[{"channel":""}]}`, false},
		{"unicode channel", `{"sources":[{"channel":"caf\u00e9"}]}`, false},
		{"path channel", `{"sources":[{"channel":"a/b"}]}`, false},
		{"whitespace channel", `{"sources":[{"channel":" a"}]}`, false},
		{"minimum limit", `{"crawl_page_limit":1}`, true},
		{"maximum limit", `{"crawl_page_limit":100}`, true},
		{"zero limit", `{"crawl_page_limit":0}`, false},
		{"negative limit", `{"crawl_page_limit":-1}`, false},
		{"fractional source limit", `{"sources":[{"channel":"a","page_limit":1.5}]}`, false},
		{"invalid disabled limit", `{"sources":[{"channel":"a","enabled":false,"page_limit":101}]}`, false},
		{"string enabled", `{"sources":[{"channel":"a","enabled":"false"}]}`, false},
		{"duplicate enabled", `{"sources":[{"channel":"a","enabled":false,"enabled":true}]}`, false},
		{"duplicate nested limit", `{"sources":[{"channel":"a","page_limit":1,"page_limit":2}]}`, false},
		{"wrong collection type", `{"sources":{}}`, false},
		{"trailing document", `{"sources":[]} {}`, false},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				require.NoError(t, os.WriteFile(path, []byte(tc.body), 0600))
				settings, err := config.Resolve(config.Overrides{ConfigPath: &path})
				if !tc.valid {
					require.ErrorIs(t, err, config.ErrInvalid)
					return
				}
				require.NoError(t, err)
				if tc.name == "maximum channels" {
					require.Len(t, settings.Sources, 32)
					_, err = settings.SelectSources(nil, nil)
					require.ErrorIs(t, err, config.ErrSelectionEmpty)
				}
				if tc.name == "maximum channel length" {
					require.Equal(t, strings.Repeat("a", 64), settings.Sources[0].Channel)
				}
				if strings.HasSuffix(tc.name, "limit") {
					for _, source := range settings.Sources {
						require.Equal(t, settings.CrawlPageLimit, source.PageLimit)
					}
				}
			},
		)
	}
}

// covers: spec 0004 AC-2, AC-3. Explicit limits do not mutate later invocations.
func TestSelectSources_LimitOverridesAreBoundedAndDoNotMutateSettings(t *testing.T) {
	settings := config.Settings{
		Sources: []config.Source{
			{Channel: "first", Enabled: true, PageLimit: 5}, {Channel: "off", Enabled: false, PageLimit: 2},
		},
	}
	for _, limit := range []int{-1, 0, 1, 100, 101} {
		t.Run(
			fmt.Sprint(limit), func(t *testing.T) {
				plan, err := settings.SelectSources(nil, &limit)
				if limit < 1 || limit > 100 {
					require.ErrorIs(t, err, config.ErrSelectionInvalid)
				} else {
					require.NoError(t, err)
					require.Equal(t, []state.SourcePlan{{Channel: "first", PageLimit: limit}}, plan)
				}
				plan, err = settings.SelectSources(nil, nil)
				require.NoError(t, err)
				require.Equal(t, []state.SourcePlan{{Channel: "first", PageLimit: 5}}, plan)
			},
		)
	}
}
