package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
)

// AC-10: supplied JSON must be valid before an explicit flag can override it.
func TestCheckPolicyJSONBounds(t *testing.T) {
	for _, input := range []string{
		`{"check_workers":0}`, `{"check_workers":5}`, `{"check_workers":1.5}`, `{"check_workers":null}`,
		`{"check_workers":"2"}`, `{"check_max_attempts":0}`, `{"check_max_attempts":6}`,
		`{"check_mutation_interval":"1500ms"}`, `{"check_mutation_interval":"61s"}`,
		`{"check_candidate_timeout":"59s"}`, `{"check_candidate_timeout":"61m"}`,
		`{"check_candidate_timeout":null}`,
	} {
		t.Run(
			input, func(t *testing.T) {
				var values map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(input), &values))
				settings := Settings{CheckPolicy: state.DefaultCheckPolicy()}
				require.ErrorIs(t, applyJSON(&settings, values), ErrInvalid)
			},
		)
	}
	for _, input := range []string{
		`{"check_workers":1,"check_max_attempts":1,"check_mutation_interval":"1s","check_candidate_timeout":"1m"}`,
		`{"check_workers":4,"check_max_attempts":5,"check_mutation_interval":"60s","check_candidate_timeout":"60m"}`,
	} {
		var values map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(input), &values))
		settings := Settings{CheckPolicy: state.DefaultCheckPolicy()}
		require.NoError(t, applyJSON(&settings, values))
		require.True(t, settings.CheckPolicy.Valid())
	}
}

// covers: spec 0005 AC-10. Resolve applies only explicit overrides and validates JSON first.
func TestResolve_CheckPolicyPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		flag bool
		fail bool
	}{
		{"defaults", `{}`, false, false},
		{"JSON", `{"check_workers":4,"check_max_attempts":5,` +
			`"check_mutation_interval":"60s","check_candidate_timeout":"60m"}`, false, false},
		{"explicit default values override JSON", `{"check_workers":4,"check_max_attempts":5,` +
			`"check_mutation_interval":"60s","check_candidate_timeout":"60m"}`, true, false},
		{"invalid JSON cannot be hidden", `{"check_workers":0}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.body), 0600))
			in := Overrides{ConfigPath: &path}
			workers, attempts, interval, timeout := "2", "3", "1s", "15m"
			if tc.flag {
				in.CheckWorkers, in.CheckMaxAttempts = &workers, &attempts
				in.CheckMutationInterval, in.CheckCandidateTimeout = &interval, &timeout
			}
			settings, err := Resolve(in)
			if tc.fail {
				require.ErrorIs(t, err, ErrInvalid)
				return
			}
			require.NoError(t, err)
			want := state.DefaultCheckPolicy()
			if tc.name == "JSON" {
				want.Workers, want.MaxAttempts = 4, 5
				want.MutationIntervalUS, want.CandidateTimeoutUS = 60000000, 3600000000
			}
			require.Equal(t, want, settings.CheckPolicy)
		})
	}
}
