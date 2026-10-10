package store_test

import (
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

func beginAttempt(t *testing.T, w *store.Writer, run, candidate int64, at time.Time) state.Attempt {
	t.Helper()
	a, err := w.BeginAttempt(t.Context(), state.AttemptInput{RequestID: rand.Text(), RunID: run, CandidateID: candidate, BackendID: "controlled_fixture", BackendVersion: "dev", StartedAt: at})
	require.NoError(t, err)
	return a
}

// covers: AC-2, AC-3, AC-12.
func TestDiscoveryIdentityAndReplay(t *testing.T) {
	w, _ := openWriter(t)
	input := state.RunInput{RequestID: rand.Text(), Kind: state.Crawl, StartedAt: fixtureTime}
	run, err := w.StartRun(t.Context(), input)
	require.NoError(t, err)
	again, err := w.StartRun(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, run, again)
	conflict := input
	conflict.Kind = state.Validation
	_, err = w.StartRun(t.Context(), conflict)
	require.ErrorIs(t, err, store.ErrConflict)
	msg := state.MessageInput{RunID: run.ID, SourceID: "source", MessageID: "message", ObservedAt: fixtureTime, Keys: []string{" key ", "key", "Key", "k ey"}}
	first, err := w.RecordMessage(t.Context(), msg)
	require.NoError(t, err)
	require.Equal(t, 3, first.NewCandidates)
	require.Equal(t, 3, first.NewSightings)
	require.Equal(t, 3, first.NewObservations)
	url := "https://example.com/message/1"
	publication := fixtureTime.Add(-time.Hour)
	msg.PublishedAt, msg.MessageURL = &publication, &url
	msg.ObservedAt = fixtureTime.Add(time.Hour)
	replay, err := w.RecordMessage(t.Context(), msg)
	require.NoError(t, err)
	require.Equal(t, first.Discoveries, replay.Discoveries)
	require.Zero(t, replay.NewObservations)
	_, err = w.ListObservations(t.Context(), run.ID, state.PageRequest{})
	require.ErrorIs(t, err, store.ErrRunActive)
	finished := state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: fixtureTime.Add(time.Minute)}
	terminal, err := w.FinishRun(t.Context(), finished)
	require.NoError(t, err)
	again, err = w.StartRun(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, terminal, again)
	msg.PublishedAt = nil
	replay, err = w.RecordMessage(t.Context(), msg)
	require.NoError(t, err)
	require.Equal(t, first.Discoveries, replay.Discoveries)
	msg.Keys = append(msg.Keys, "new")
	_, err = w.RecordMessage(t.Context(), msg)
	require.ErrorIs(t, err, store.ErrRunClosed)
	observations, err := w.ListObservations(t.Context(), run.ID, state.PageRequest{Size: 2})
	require.NoError(t, err)
	require.Len(t, observations.Items, 2)
	require.NotNil(t, observations.Next)
	for _, o := range observations.Items {
		require.Equal(t, fixtureTime, o.ObservedAt)
	}
	next, err := w.ListObservations(t.Context(), run.ID, state.PageRequest{Size: 2, Cursor: observations.Next})
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.Nil(t, next.Next)
	second := startRun(t, w, state.Crawl)
	msg.RunID = second.ID
	msg.Keys = []string{"Key"}
	msg.ObservedAt = fixtureTime.Add(-2 * time.Hour)
	_, err = w.RecordMessage(t.Context(), msg)
	require.NoError(t, err)
	c, err := w.GetCandidate(t.Context(), first.Discoveries[0].CandidateID, evaluation())
	require.NoError(t, err)
	require.Equal(t, "Key", c.Key)
	require.Equal(t, msg.ObservedAt, c.FirstSeenAt)
	require.Equal(t, fixtureTime, c.LastSeenAt)
	sightings, err := w.ListSightings(t.Context(), c.ID, state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, sightings.Items, 1)
	require.Equal(t, &publication, sightings.Items[0].PublishedAt)
	msg.SourceID = "another_source"
	_, err = w.RecordMessage(t.Context(), msg)
	require.NoError(t, err)
	sightings, err = w.ListSightings(t.Context(), c.ID, state.PageRequest{})
	require.NoError(t, err)
	require.Len(t, sightings.Items, 2)
}

// covers: AC-3, AC-4, AC-5, AC-12.
func TestEvidenceFreshnessAndTerminalRetries(t *testing.T) {
	w, _ := openWriter(t)
	crawl := startRun(t, w, state.Crawl)
	candidate := discover(t, w, crawl.ID, "synthetic").Discoveries[0].CandidateID
	run := startRun(t, w, state.Validation)
	input := state.AttemptInput{RequestID: rand.Text(), RunID: run.ID, CandidateID: candidate, BackendID: "fixture", BackendVersion: "dev", StartedAt: fixtureTime}
	a, err := w.BeginAttempt(t.Context(), input)
	require.NoError(t, err)
	again, err := w.BeginAttempt(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, a, again)
	input.RequestID = rand.Text()
	_, err = w.BeginAttempt(t.Context(), input)
	require.ErrorIs(t, err, store.ErrAttemptActive)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: fixtureTime})
	require.ErrorIs(t, err, store.ErrAttemptActive)
	completion := state.AttemptCompletion{AttemptID: a.ID, FinishedAt: fixtureTime.Add(time.Second), Outcome: state.Confirmed, Evidence: &state.Evidence{Kind: state.AccountType, ObservedAt: fixtureTime, AccountType: "Unlimited"}}
	completed, err := w.CompleteAttempt(t.Context(), completion)
	require.NoError(t, err)
	again, err = w.CompleteAttempt(t.Context(), completion)
	require.NoError(t, err)
	require.Equal(t, completed, again)
	completion.FinishedAt = fixtureTime.Add(2 * time.Second)
	_, err = w.CompleteAttempt(t.Context(), completion)
	require.ErrorIs(t, err, store.ErrConflict)
	// A retry is later by identity, even with an earlier event clock.
	retry := beginAttempt(t, w, run.ID, candidate, fixtureTime.Add(-time.Minute))
	_, err = w.CompleteAttempt(t.Context(), state.AttemptCompletion{AttemptID: retry.ID, FinishedAt: fixtureTime.Add(-2 * time.Minute), Outcome: state.Inconclusive, ReasonCode: "transport_error"})
	require.NoError(t, err)
	for _, test := range []struct {
		at      time.Time
		want    state.Freshness
		anomaly bool
	}{
		{fixtureTime.Add(6*time.Hour - time.Microsecond), state.Fresh, false},
		{fixtureTime.Add(6 * time.Hour), state.Stale, false},
		{fixtureTime.Add(-time.Microsecond), state.Unknown, true},
	} {
		c, err := w.GetCandidate(t.Context(), candidate, state.Evaluation{AsOf: test.at, MaxAge: 6 * time.Hour})
		require.NoError(t, err)
		require.Equal(t, state.Inconclusive, c.LatestOutcome)
		require.Equal(t, a.ID, c.LastDefinitiveAttempt.ID)
		require.Equal(t, retry.ID, c.LatestAttempt.ID)
		require.True(t, c.LatestAttempt.ClockAnomaly)
		require.Equal(t, test.want, c.Freshness)
		require.Equal(t, test.anomaly, c.ClockAnomaly)
	}
	rejection := beginAttempt(t, w, run.ID, candidate, fixtureTime)
	_, err = w.CompleteAttempt(t.Context(), state.AttemptCompletion{AttemptID: rejection.ID, FinishedAt: fixtureTime, Outcome: state.Invalid, ReasonCode: "fixture_rejected", Evidence: &state.Evidence{Kind: state.Rejection, ObservedAt: fixtureTime, RejectionCode: "fixture_rejected"}})
	require.NoError(t, err)
	c, err := w.GetCandidate(t.Context(), candidate, evaluation())
	require.NoError(t, err)
	require.Equal(t, state.Invalid, c.LatestOutcome)
	require.Equal(t, state.NotApplicable, c.Freshness)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: fixtureTime})
	require.NoError(t, err)
	input.RequestID = a.RequestID
	again, err = w.BeginAttempt(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, a.ID, again.ID)
	input.BackendVersion = "different"
	_, err = w.BeginAttempt(t.Context(), input)
	require.ErrorIs(t, err, store.ErrConflict)
	history, err := w.ListAttempts(t.Context(), state.AttemptFilter{CandidateID: candidate}, state.PageRequest{Size: 2})
	require.NoError(t, err)
	require.Len(t, history.Items, 2)
	require.NotNil(t, history.Next)
	history, err = w.ListAttempts(t.Context(), state.AttemptFilter{CandidateID: candidate}, state.PageRequest{Size: 2, Cursor: history.Next})
	require.NoError(t, err)
	require.Len(t, history.Items, 1)
	require.Nil(t, history.Next)
}

// covers: AC-3, AC-4, AC-8, AC-12.
func TestRecoveryPreservesEvidenceAndReplay(t *testing.T) {
	w, dir := openWriter(t)
	crawl := startRun(t, w, state.Crawl)
	discovery := discover(t, w, crawl.ID, "synthetic")
	candidate := discovery.Discoveries[0].CandidateID
	run := startRun(t, w, state.Validation)
	a := beginAttempt(t, w, run.ID, candidate, fixtureTime)
	completed, err := w.CompleteAttempt(t.Context(), state.AttemptCompletion{AttemptID: a.ID, FinishedAt: fixtureTime, Outcome: state.Confirmed, Evidence: &state.Evidence{Kind: state.AccountType, ObservedAt: fixtureTime, AccountType: "Unlimited"}})
	require.NoError(t, err)
	pending := beginAttempt(t, w, run.ID, candidate, fixtureTime)
	require.NoError(t, w.Close())
	recoveredAt := fixtureTime.Add(time.Hour)
	recovered, err := store.OpenWriter(t.Context(), dir, func() time.Time { return recoveredAt })
	require.NoError(t, err)
	c, err := recovered.GetCandidate(t.Context(), candidate, evaluation())
	require.NoError(t, err)
	require.Equal(t, &completed, c.LastDefinitiveAttempt)
	require.Equal(t, pending.ID, c.LatestAttempt.ID)
	require.Equal(t, &recoveredAt, c.LatestAttempt.RecoveredAt)
	require.Nil(t, c.LatestAttempt.FinishedAt)
	require.Nil(t, c.UnresolvedAttempt)
	require.Equal(t, state.Inconclusive, c.LatestOutcome)
	replay := discover(t, recovered, crawl.ID, "synthetic")
	require.Equal(t, discovery.Discoveries, replay.Discoveries)
	require.Zero(t, replay.NewCandidates)
	before, err := recovered.ListRuns(t.Context(), "", state.PageRequest{})
	require.NoError(t, err)
	for _, run := range before.Items {
		require.Equal(t, state.Interrupted, run.Status)
		require.Nil(t, run.FinishedAt)
		require.Equal(t, &recoveredAt, run.RecoveredAt)
	}
	require.NoError(t, recovered.Close())
	recovered, err = store.OpenWriter(t.Context(), dir, func() time.Time { return recoveredAt.Add(time.Hour) })
	require.NoError(t, err)
	defer func() { require.NoError(t, recovered.Close()) }()
	after, err := recovered.ListRuns(t.Context(), "", state.PageRequest{})
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// covers: AC-3, AC-4, AC-10. Rejected evidence must leave the attempt unresolved.
func TestCompleteAttempt_RejectsInvalidEvidenceWithoutChangingHistory(t *testing.T) {
	w, _ := openWriter(t)
	crawl := startRun(t, w, state.Crawl)
	candidate := discover(t, w, crawl.ID, "synthetic_secret").Discoveries[0].CandidateID
	run := startRun(t, w, state.Validation)
	a := beginAttempt(t, w, run.ID, candidate, fixtureTime)
	confirmation := state.Evidence{Kind: state.AccountType, ObservedAt: fixtureTime, AccountType: "Unlimited"}
	rejection := state.Evidence{Kind: state.Rejection, ObservedAt: fixtureTime, RejectionCode: "license_rejected"}
	for _, tt := range []struct {
		name   string
		change func(*state.AttemptCompletion)
	}{
		{"missing evidence", func(c *state.AttemptCompletion) { c.Evidence = nil }},
		{"unknown account", func(c *state.AttemptCompletion) { c.Evidence.AccountType = "Warp" }},
		{"account case matters", func(c *state.AttemptCompletion) { c.Evidence.AccountType = "unlimited" }},
		{"mixed evidence", func(c *state.AttemptCompletion) { c.Evidence.RejectionCode = "license_rejected" }},
		{"wrong evidence kind", func(c *state.AttemptCompletion) { c.Evidence.Kind = state.Rejection }},
		{"confirmation with reason", func(c *state.AttemptCompletion) { c.ReasonCode = "unexpected" }},
		{"zero evidence time", func(c *state.AttemptCompletion) { c.Evidence.ObservedAt = time.Time{} }},
		{"zero finish time", func(c *state.AttemptCompletion) { c.FinishedAt = time.Time{} }},
		{"unknown outcome", func(c *state.AttemptCompletion) { c.Outcome = state.Unchecked }},
		{"inconclusive with evidence", func(c *state.AttemptCompletion) { c.Outcome = state.Inconclusive; c.ReasonCode = "timeout" }},
		{"inconclusive without reason", func(c *state.AttemptCompletion) { c.Outcome = state.Inconclusive; c.Evidence = nil }},
		{"free text reason", func(c *state.AttemptCompletion) {
			c.Outcome = state.Inconclusive
			c.Evidence = nil
			c.ReasonCode = "synthetic_secret raw output"
		}},
		{"reason too long", func(c *state.AttemptCompletion) {
			c.Outcome = state.Inconclusive
			c.Evidence = nil
			c.ReasonCode = strings.Repeat("a", 65)
		}},
		{"invalid without reason", func(c *state.AttemptCompletion) { c.Outcome = state.Invalid; c.Evidence = &rejection }},
		{"invalid with account evidence", func(c *state.AttemptCompletion) { c.Outcome = state.Invalid; c.ReasonCode = "license_rejected" }},
		{"rejection without code", func(c *state.AttemptCompletion) {
			c.Outcome = state.Invalid
			c.ReasonCode = "license_rejected"
			c.Evidence = &state.Evidence{Kind: state.Rejection, ObservedAt: fixtureTime}
		}},
		{"rejection with free text", func(c *state.AttemptCompletion) {
			c.Outcome = state.Invalid
			c.ReasonCode = "license_rejected"
			c.Evidence = &state.Evidence{Kind: state.Rejection, ObservedAt: fixtureTime, RejectionCode: "synthetic_secret raw output"}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			evidence := confirmation
			input := state.AttemptCompletion{AttemptID: a.ID, FinishedAt: fixtureTime, Outcome: state.Confirmed, Evidence: &evidence}
			tt.change(&input)
			_, err := w.CompleteAttempt(t.Context(), input)
			require.ErrorIs(t, err, store.ErrInvalidInput)
			require.NotContains(t, err.Error(), "synthetic_secret")
			history, err := w.ListAttempts(t.Context(), state.AttemptFilter{CandidateID: candidate}, state.PageRequest{})
			require.NoError(t, err)
			require.Equal(t, []state.Attempt{a}, history.Items)
		})
	}
	// The maximum valid reason length remains usable after all rejected writes.
	result, err := w.CompleteAttempt(t.Context(), state.AttemptCompletion{AttemptID: a.ID, FinishedAt: fixtureTime, Outcome: state.Inconclusive, ReasonCode: strings.Repeat("a", 64)})
	require.NoError(t, err)
	require.Equal(t, state.Inconclusive, result.Outcome)
	require.Nil(t, result.Evidence)
}

// covers: AC-3, AC-4, AC-12.
func TestBeginAttempt_SeparatesPendingWorkAndConflictingRetryInputs(t *testing.T) {
	w, _ := openWriter(t)
	crawl := startRun(t, w, state.Crawl)
	keys := discover(t, w, crawl.ID, "first", "second")
	run := startRun(t, w, state.Validation)
	other := startRun(t, w, state.Validation)
	input := state.AttemptInput{RequestID: "stable_attempt", RunID: run.ID, CandidateID: keys.Discoveries[0].CandidateID, BackendID: "fixture", BackendVersion: "v1", StartedAt: fixtureTime.Add(123 * time.Nanosecond)}
	a, err := w.BeginAttempt(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, fixtureTime, a.StartedAt)
	for _, tt := range []struct {
		name   string
		change func(*state.AttemptInput)
	}{
		{"run", func(in *state.AttemptInput) { in.RunID = other.ID }},
		{"candidate", func(in *state.AttemptInput) { in.CandidateID = keys.Discoveries[1].CandidateID }},
		{"backend", func(in *state.AttemptInput) { in.BackendID = "other" }},
		{"version", func(in *state.AttemptInput) { in.BackendVersion = "v2" }},
		{"start", func(in *state.AttemptInput) { in.StartedAt = fixtureTime.Add(time.Microsecond) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed := input
			tt.change(&changed)
			_, err := w.BeginAttempt(t.Context(), changed)
			require.ErrorIs(t, err, store.ErrConflict)
		})
	}
	input.StartedAt = fixtureTime.In(time.FixedZone("fixture", 7*60*60)).Add(999 * time.Nanosecond)
	replay, err := w.BeginAttempt(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, a, replay)
	input.RequestID, input.RunID = "another_attempt", other.ID
	_, err = w.BeginAttempt(t.Context(), input)
	require.ErrorIs(t, err, store.ErrAttemptActive)
	second := beginAttempt(t, w, other.ID, keys.Discoveries[1].CandidateID, fixtureTime)
	for _, active := range []state.Attempt{a, second} {
		c, err := w.GetCandidate(t.Context(), active.CandidateID, evaluation())
		require.NoError(t, err)
		require.Equal(t, &active, c.UnresolvedAttempt)
		require.Equal(t, state.Unchecked, c.LatestOutcome)
		require.Equal(t, state.NotApplicable, c.Freshness)
		require.Nil(t, c.LatestAttempt)
		require.Nil(t, c.LastDefinitiveAttempt)
	}
}

// covers: AC-3, AC-12. Terminal execution results are immutable and repeatable.
func TestFinishRun_RetainsTerminalResultsAndNormalizedRetryIdentity(t *testing.T) {
	for _, status := range []state.RunStatus{state.Completed, state.Failed, state.Cancelled} {
		t.Run(string(status), func(t *testing.T) {
			w, _ := openWriter(t)
			input := state.RunInput{RequestID: "stable_run", Kind: state.Crawl, StartedAt: fixtureTime.Add(123 * time.Nanosecond)}
			run, err := w.StartRun(t.Context(), input)
			require.NoError(t, err)
			finish := state.RunCompletion{RunID: run.ID, Status: status, FinishedAt: fixtureTime.Add(-time.Minute)}
			if status != state.Completed {
				finish.ReasonCode = "fixture_stopped"
			}
			terminal, err := w.FinishRun(t.Context(), finish)
			require.NoError(t, err)
			require.True(t, terminal.ClockAnomaly)
			require.Equal(t, &finish.FinishedAt, terminal.FinishedAt)
			require.Nil(t, terminal.RecoveredAt)
			finish.FinishedAt = finish.FinishedAt.In(time.FixedZone("fixture", 3600)).Add(999 * time.Nanosecond)
			again, err := w.FinishRun(t.Context(), finish)
			require.NoError(t, err)
			require.Equal(t, terminal, again)
			input.StartedAt = fixtureTime
			again, err = w.StartRun(t.Context(), input)
			require.NoError(t, err)
			require.Equal(t, terminal, again)
			input.StartedAt = fixtureTime.Add(time.Microsecond)
			_, err = w.StartRun(t.Context(), input)
			require.ErrorIs(t, err, store.ErrConflict)
			finish.FinishedAt = fixtureTime
			_, err = w.FinishRun(t.Context(), finish)
			require.ErrorIs(t, err, store.ErrConflict)
			page, err := w.ListRuns(t.Context(), state.Crawl, state.PageRequest{})
			require.NoError(t, err)
			require.Equal(t, []state.Run{terminal}, page.Items)
		})
	}
}

// covers: AC-2, AC-3, AC-6, AC-12.
func TestRecordMessage_PreservesKnownMetadataAndTerminalReplay(t *testing.T) {
	w, _ := openWriter(t)
	run := startRun(t, w, state.Crawl)
	input := state.MessageInput{RunID: run.ID, SourceID: "source", MessageID: "message", ObservedAt: fixtureTime}
	empty, err := w.RecordMessage(t.Context(), input)
	require.NoError(t, err)
	require.Empty(t, empty.Discoveries)
	page, err := w.ListCandidates(t.Context(), state.PageRequest{}, evaluation())
	require.NoError(t, err)
	require.Empty(t, page.Items)
	input.Keys = []string{"\u2003synthetic\u00a0"}
	first, err := w.RecordMessage(t.Context(), input)
	require.NoError(t, err)
	url, publication := "https://example.com/original", fixtureTime.Add(-time.Hour)
	input.MessageURL, input.PublishedAt = &url, &publication
	replay, err := w.RecordMessage(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, state.MessageResult{Discoveries: first.Discoveries}, replay)
	saved, err := w.ListSightings(t.Context(), first.Discoveries[0].CandidateID, state.PageRequest{})
	require.NoError(t, err)
	require.Equal(t, &url, saved.Items[0].MessageURL)
	require.Equal(t, &publication, saved.Items[0].PublishedAt)
	otherURL, otherPublication := "https://example.com/replacement", fixtureTime.Add(-2*time.Hour)
	input.MessageURL, input.PublishedAt = &otherURL, &otherPublication
	input.ObservedAt = fixtureTime.Add(time.Hour)
	_, err = w.RecordMessage(t.Context(), input)
	require.NoError(t, err)
	_, err = w.FinishRun(t.Context(), state.RunCompletion{RunID: run.ID, Status: state.Completed, FinishedAt: fixtureTime})
	require.NoError(t, err)
	replay, err = w.RecordMessage(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, state.MessageResult{Discoveries: first.Discoveries}, replay)
	input.Keys = append(input.Keys, "new_candidate")
	_, err = w.RecordMessage(t.Context(), input)
	require.ErrorIs(t, err, store.ErrRunClosed)
	after, err := w.ListSightings(t.Context(), first.Discoveries[0].CandidateID, state.PageRequest{})
	require.NoError(t, err)
	require.Equal(t, saved, after)
	page, err = w.ListCandidates(t.Context(), state.PageRequest{}, evaluation())
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "synthetic", page.Items[0].Key)
	require.Equal(t, fixtureTime, page.Items[0].LastSeenAt)
}
