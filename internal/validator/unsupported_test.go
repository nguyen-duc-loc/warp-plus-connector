package validator_test

import (
	"context"
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/stretchr/testify/require"
)

// covers: spec 0003 AC-4, AC-6. Unsupported validation never invents evidence.
func TestUnsupported_RecordsNoEvidenceAndHonorsCancellation(t *testing.T) {
	backend := validator.Unsupported{Version: "test"}
	result, err := backend.Check(t.Context(), validator.Input{Key: "ABCDEFGH-12345678-AbCdEf01"})
	require.NoError(t, err)
	require.Equal(t, state.Inconclusive, result.Outcome)
	require.Equal(t, "validation_unsupported", result.ReasonCode)
	require.Nil(t, result.Evidence)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = backend.Check(ctx, validator.Input{})
	require.ErrorIs(t, err, context.Canceled)
}
