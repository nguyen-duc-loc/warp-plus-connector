package store_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// covers: spec 0011 AC-8, AC-12, AC-13. Private pages preserve exact integers and arbitrary key bytes.
func TestBrowseStore_PrivateBoundedPagesPreserveExactValues(t *testing.T) {
	s, err := store.NewBrowseStore(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	info, err := os.Stat(s.Directory)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), info.Mode().Perm())
	info, err = os.Stat(filepath.Join(s.Directory, "snapshot.db"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	const base int64 = 9007199254740993
	for i := int64(0); i < 103; i++ {
		require.NoError(t, s.Add(t.Context(), state.Candidate{ID: base + i, Key: fmt.Sprintf("key-%d-\xff", i)}))
	}
	count, marked, err := s.BuildView(t.Context(), 1, func(state.Candidate) bool { return true },
		func(c state.Candidate) (bool, error) { return c.ID%2 == 1, nil }, func(int64) {})
	require.NoError(t, err)
	require.Equal(t, int64(103), count)
	require.Equal(t, int64(52), marked)
	for offset := int64(0); offset < count; {
		page, err := s.Page(t.Context(), 1, offset, false)
		require.NoError(t, err)
		require.NotEmpty(t, page)
		require.LessOrEqual(t, len(page), 50)
		for _, row := range page {
			require.Equal(t, offset, row.Ordinal)
			require.Equal(t, base+offset, row.Candidate.ID)
			require.Equal(t, fmt.Sprintf("key-%d-\xff", offset), row.Candidate.Key)
			offset++
		}
	}
	var ids []int64
	for offset := int64(0); ; {
		page, err := s.Page(t.Context(), 1, offset, true)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			require.True(t, row.Marked)
			ids = append(ids, row.Candidate.ID)
			offset = row.Ordinal + 1
		}
	}
	require.Len(t, ids, 52)
	require.Equal(t, base+102, ids[51])
	require.NoError(t, s.Close())
	_, err = os.Stat(s.Directory)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// covers: spec 0011 AC-11, AC-12. A failed preparation rolls back partial membership and permits a clean retry.
func TestBrowseStore_FailedViewLeavesNoPartialRows(t *testing.T) {
	s, err := store.NewBrowseStore(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	for id := int64(1); id <= 60; id++ {
		require.NoError(t, s.Add(t.Context(), state.Candidate{ID: id, Key: fmt.Sprint(id)}))
	}
	sentinel := errors.New("mark reconciliation stopped")
	_, _, err = s.BuildView(t.Context(), 1, func(state.Candidate) bool { return true },
		func(c state.Candidate) (bool, error) {
			if c.ID == 51 {
				return false, sentinel
			}
			return true, nil
		}, func(int64) {})
	require.ErrorIs(t, err, sentinel)
	page, err := s.Page(t.Context(), 1, 0, false)
	require.NoError(t, err)
	require.Empty(t, page)
	count, marked, err := s.BuildView(t.Context(), 1, func(state.Candidate) bool { return true }, nil, func(int64) {})
	require.NoError(t, err)
	require.Equal(t, int64(60), count)
	require.Zero(t, marked)
}
