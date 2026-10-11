package config

import (
	"encoding/json"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
	"testing"
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
