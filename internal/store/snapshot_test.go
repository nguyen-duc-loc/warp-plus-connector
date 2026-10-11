package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// covers: spec 0006 AC-8, AC-10. Callers can stop normally, preserve errors, and reuse the reader.
func TestVisitCandidates_StopAndErrorReleaseSnapshot(t *testing.T) {
	w, dir := openWriter(t)
	run := startRun(t, w, state.Crawl)
	discover(t, w, run.ID, "first", "second")
	r, err := store.OpenReader(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	sentinel := errors.New("caller failure")
	for _, callbackErr := range []error{nil, sentinel} {
		calls := 0
		err := r.VisitCandidates(t.Context(), evaluation(), func(state.Candidate) (bool, error) {
			calls++
			return false, callbackErr
		})
		require.ErrorIs(t, err, callbackErr)
		require.Equal(t, 1, calls)
		page, err := r.ListCandidates(t.Context(), state.PageRequest{}, evaluation())
		require.NoError(t, err)
		require.Len(t, page.Items, 2, "the prior transaction released its connection")
	}
	require.ErrorIs(t, r.VisitCandidates(t.Context(), evaluation(), nil), store.ErrInvalidInput)
	require.NoError(t, r.Close())
	err = r.VisitCandidates(t.Context(), evaluation(), func(state.Candidate) (bool, error) { return true, nil })
	require.ErrorIs(t, err, store.ErrClosed)
}

// covers: spec 0006 AC-8, AC-10. Cancellation cannot return ownership while an entered callback is using it.
func TestVisitCandidates_CancellationWaitsForEnteredCallback(t *testing.T) {
	w, dir := openWriter(t)
	run := startRun(t, w, state.Crawl)
	discover(t, w, run.ID, "first", "second")
	r, err := store.OpenReader(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	done := make(chan error, 1)
	go func() {
		done <- r.VisitCandidates(ctx, evaluation(), func(state.Candidate) (bool, error) {
			close(entered)
			<-release
			close(exited)
			return true, nil
		})
	}()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal("callback never entered")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("returned before the entered callback was released: %v", err)
	case <-time.After(50 * time.Millisecond):
		// Cancellation has had an opportunity to wake any detached operation.
	}
	unblock()
	require.ErrorIs(t, <-done, context.Canceled)
	select {
	case <-exited:
	default:
		t.Fatal("traversal returned before its callback exited")
	}
	page, err := r.ListCandidates(t.Context(), state.PageRequest{}, evaluation())
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
}
