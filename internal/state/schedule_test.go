package state_test

import (
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/stretchr/testify/require"
)

// covers: spec 0007 AC-7. Exact intervals round upward, never admitting work early.
func TestScheduleDue_RoundsUpAndRejectsUnrepresentableTimes(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.FixedZone("local", 7*3600))
	for _, tc := range []struct {
		name     string
		at       time.Time
		interval time.Duration
		want     time.Time
	}{
		{"immediate", at, 0, at.UTC()},
		{"one nanosecond", at, time.Nanosecond, at.UTC().Add(time.Microsecond)},
		{"exact microsecond", at, time.Microsecond, at.UTC().Add(time.Microsecond)},
		{"carry second", at.Add(time.Second - time.Nanosecond), 0, at.UTC().Add(time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := state.ScheduleDue(tc.at, tc.interval)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	for _, tc := range []struct {
		at       time.Time
		interval time.Duration
	}{
		{time.Time{}, time.Hour}, {at, -time.Nanosecond},
		{time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC), 0},
	} {
		_, err := state.ScheduleDue(tc.at, tc.interval)
		require.Error(t, err)
	}
}
