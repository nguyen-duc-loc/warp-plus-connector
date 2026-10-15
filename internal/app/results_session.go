package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

// ResultsView describes one fixed filtered collection, without candidate keys.
type ResultsView struct {
	SnapshotID, ID                   int64
	Evaluation                       state.Evaluation
	Filters                          CandidateQuery
	Count, Marked, Revision, Removed int64
	FocusID, FocusOffset             *int64
	Retained                         bool
	ErrorCode                        string
}

// ResultsDetail describes a separate fixed observation of a candidate's history.
type ResultsDetail struct {
	ID, ViewID, CandidateID int64
	Evaluation              state.Evaluation
	Summary                 state.Candidate
	Sources, Attempts       int64
}

// ResultsProgress reports successfully prepared rows, never candidate data.
type ResultsProgress func(phase string, count int64)

// ResultsCommit serializes activation with cancellation admission in the owner.
type ResultsCommit func(func() error) error

// ResultsSession owns private snapshots. Its owner serializes calls, and cancels
// and waits for entered work before closing. Polling never calls this object.
type ResultsSession struct {
	app                                *App
	data                               *store.BrowseStore
	identity                           store.CollectionIdentity
	View                               *ResultsView
	Detail                             *ResultsDetail
	nextSnapshot, nextView, nextDetail int64
	retired                            *store.BrowseStore
	oldView, oldDetail                 int64
	Warning, Remaining                 string
}

// NewResultsSession allocates no state until explicitly opened.
func (a *App) NewResultsSession() *ResultsSession { return &ResultsSession{app: a} }

func snapshotError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, store.ErrBrowseCommit) {
		return &Error{Code: "ui_protocol_invalid"}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return classified(err)
	}
	return &Error{Code: "ui_snapshot_failed"}
}

// Prepare retries previously failed owned cleanup before allocating more state.
func (s *ResultsSession) Prepare(ctx context.Context) error {
	s.Warning, s.Remaining = "", ""
	if s.retired != nil {
		if err := s.retired.Close(); err != nil {
			s.Remaining = s.retired.Directory
			return &Error{Code: "ui_snapshot_cleanup_failed"}
		}
		s.retired = nil
	}
	if s.data != nil {
		var err error
		if s.oldView != 0 {
			err = s.data.DropView(ctx, s.oldView)
			if err == nil {
				s.oldView = 0
			}
		}
		if err == nil && s.oldDetail != 0 {
			err = s.data.DropDetail(ctx, s.oldDetail)
			if err == nil {
				s.oldDetail = 0
			}
		}
		if err != nil {
			s.Remaining = s.data.Directory
			return &Error{Code: "ui_snapshot_cleanup_failed"}
		}
	}
	return nil
}

func (s *ResultsSession) cleanupAfterCommit() {
	if err := s.Prepare(context.Background()); err != nil {
		s.Warning = "ui_snapshot_cleanup_failed"
	}
}

// Close releases session storage only after the owner has stopped its users.
func (s *ResultsSession) Close() error {
	err := s.Prepare(context.Background())
	if s.data != nil {
		if e := s.data.Close(); e != nil {
			s.Remaining = s.data.Directory
			err = &Error{Code: "ui_snapshot_cleanup_failed"}
		}
	}
	return err
}

// RequireView rejects absent and superseded handles.
func (s *ResultsSession) RequireView(id int64) error {
	if s.View == nil || s.View.ID != id {
		return &Error{Code: "ui_handle_stale"}
	}
	return nil
}

// Candidate reads one current member without exposing its key to the UI.
func (s *ResultsSession) Candidate(ctx context.Context, id int64) (store.BrowseRow, error) {
	if s.View == nil {
		return store.BrowseRow{}, &Error{Code: "ui_handle_stale"}
	}
	r, err := s.data.Candidate(ctx, s.View.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		return r, &Error{Code: "ui_candidate_unavailable"}
	}
	return r, snapshotError(err)
}

// Open captures a complete replacement before activation. Refresh failure keeps
// the prior evaluation, marks and position, including when live state disappears.
func (s *ResultsSession) Open(
	ctx context.Context, refresh bool, focus *int64,
	progress ResultsProgress, commit ResultsCommit,
) (err error) {
	if refresh != (s.View != nil) {
		return &Error{Code: "ui_handle_stale"}
	}
	defer func() {
		if err != nil && refresh {
			s.View.Retained = true
			s.View.ErrorCode = classified(err).(*Error).Code
			if e := s.data.RetainView(context.Background(), s.View.ID, s.View.ErrorCode); e != nil {
				err = &Error{Code: "ui_protocol_invalid"}
			}
		}
	}()
	var focusKey *string
	if focus != nil {
		r, e := s.Candidate(ctx, *focus)
		if e != nil {
			return e
		}
		focusKey = &r.Candidate.Key
	}
	if err = s.Prepare(ctx); err != nil {
		return err
	}
	identity, err := store.IdentifyCollection(s.app.Settings.StateDir)
	if err != nil {
		return classified(err)
	}
	data, err := store.NewBrowseStore(ctx)
	if err != nil {
		if data != nil {
			s.retired = data
			s.Remaining = data.Directory
		}
		return snapshotError(err)
	}
	activated := false
	defer func() {
		if !activated {
			s.retired = data
			if e := s.Prepare(context.Background()); e != nil && err == nil {
				err = e
			}
		}
	}()
	relative, pathErr := filepath.Rel(identity.Directory, data.Directory)
	if pathErr != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return &Error{Code: "ui_snapshot_failed"}
	}
	progress("collection", 0)
	var count int64
	result, err := s.app.QueryCandidates(
		ctx, CandidateQuery{}, func(c state.Candidate) error {
			if e := data.Add(ctx, c); e != nil {
				return snapshotError(e)
			}
			count++
			progress("collection", count)
			return nil
		},
	)
	if err != nil {
		return err
	}
	if !identity.Same(s.app.Settings.StateDir) {
		return &Error{Code: "ui_collection_changed"}
	}
	s.nextSnapshot++
	if err = data.SaveSnapshot(ctx, s.nextSnapshot, result.Evaluation, identity); err != nil {
		return snapshotError(err)
	}
	s.nextView++
	q, _ := (CandidateQuery{}).Normalize()
	if refresh {
		q = s.View.Filters
	}
	v := &ResultsView{SnapshotID: s.nextSnapshot, ID: s.nextView, Evaluation: result.Evaluation, Filters: q}
	var retain func(state.Candidate) (bool, error)
	if refresh {
		retain = func(c state.Candidate) (bool, error) {
			r, e := s.data.Candidate(ctx, s.View.ID, c.ID)
			if errors.Is(e, store.ErrNotFound) {
				return false, nil
			}
			return e == nil && r.Marked && r.Candidate.Key == c.Key, snapshotError(e)
		}
	}
	progress("view", 0)
	v.Count, v.Marked, err = data.BuildView(ctx, v.ID, q.matches, retain, func(n int64) { progress("view", n) })
	if err != nil {
		return snapshotError(err)
	}
	if err = data.SaveView(ctx, v.ID, [3][]string{q.Status, q.Definitive, q.Freshness}, v.Count, v.Marked); err != nil {
		return snapshotError(err)
	}
	if refresh {
		v.Removed = s.View.Marked - v.Marked
	}
	var fid int64
	if focus != nil {
		fid = *focus
	}
	v.FocusID, v.FocusOffset, err = data.Focus(ctx, v.ID, fid, focusKey)
	if err != nil {
		return snapshotError(err)
	}
	err = commit(
		func() error {
			s.retired = s.data
			s.data = data
			s.identity = identity
			s.View = v
			s.Detail = nil
			s.oldView, s.oldDetail = 0, 0
			activated = true
			return nil
		},
	)
	if err == nil {
		s.cleanupAfterCommit()
	}
	return err
}

// Filter builds a replacement against saved summaries, never the live reader.
func (s *ResultsSession) Filter(
	ctx context.Context, q CandidateQuery, progress ResultsProgress,
	commit ResultsCommit,
) error {
	if q.Limit != nil {
		return ErrCandidateQuery
	}
	normalized, normalizeErr := q.Normalize()
	if normalizeErr != nil {
		return normalizeErr
	}
	q = normalized
	if s.View == nil {
		return &Error{Code: "ui_handle_stale"}
	}
	if err := s.Prepare(ctx); err != nil {
		return err
	}
	if reflect.DeepEqual(q, s.View.Filters) {
		return commit(func() error { return nil })
	}
	s.nextView++
	v := *s.View
	v.ID = s.nextView
	v.Filters = q
	v.Marked = 0
	v.Removed = 0
	v.Revision = 0
	var err error
	v.Count, _, err = s.data.BuildView(ctx, v.ID, q.matches, nil, func(n int64) { progress("view", n) })
	if err != nil {
		return snapshotError(err)
	}
	if err = s.data.SaveView(ctx, v.ID, [3][]string{q.Status, q.Definitive, q.Freshness}, v.Count, v.Marked); err != nil {
		s.oldView = v.ID
		return snapshotError(err)
	}
	v.FocusID, v.FocusOffset, err = s.data.Focus(ctx, v.ID, 0, nil)
	if err != nil {
		s.oldView = v.ID
		return snapshotError(err)
	}
	v.Retained = false
	v.ErrorCode = ""
	err = commit(func() error {
		s.oldView = s.View.ID
		if s.Detail != nil {
			s.oldDetail = s.Detail.ID
			s.Detail = nil
		}
		s.View = &v
		return nil
	})
	if err != nil {
		s.oldView = v.ID
	} else {
		s.cleanupAfterCommit()
	}
	return err
}

// Page reads bounded rows from the fixed view.
func (s *ResultsSession) Page(ctx context.Context, offset int64) ([]store.BrowseRow, error) {
	if s.View == nil {
		return nil, &Error{Code: "ui_handle_stale"}
	}
	if offset < 0 || offset > s.View.Count {
		return nil, &Error{Code: "ui_page_invalid"}
	}
	r, err := s.data.Page(ctx, s.View.ID, offset, false)
	return r, snapshotError(err)
}

// Mark sets one mark and advances the revision only for an effective change.
func (s *ResultsSession) Mark(ctx context.Context, id int64, value bool) error {
	r, err := s.Candidate(ctx, id)
	if err != nil {
		return err
	}
	if r.Marked == value {
		return nil
	}
	if err = s.data.SetMark(ctx, s.View.ID, id, value); err != nil {
		// A failed SQL mutation can have an unobserved commit. Never fabricate a count.
		return &Error{Code: "ui_protocol_invalid"}
	}
	if value {
		s.View.Marked++
	} else {
		s.View.Marked--
	}
	s.View.Revision++
	return nil
}

// ClearMarks commits cancellation and SQL admission under the same owner gate.
func (s *ResultsSession) ClearMarks(ctx context.Context, commit ResultsCommit) error {
	if err := s.Prepare(ctx); err != nil {
		return err
	}
	return commit(
		func() error {
			if err := s.data.ClearMarks(context.Background(), s.View.ID); err != nil {
				return &Error{Code: "ui_protocol_invalid"}
			}
			s.View.Marked = 0
			s.View.Revision++
			return nil
		},
	)
}

// CloseDetail invalidates the handle before deleting its private records.
func (s *ResultsSession) CloseDetail(ctx context.Context) error {
	if s.Detail != nil {
		s.oldDetail = s.Detail.ID
		s.Detail = nil
	}
	return s.Prepare(ctx)
}

// OpenDetail captures one separate transaction after validating collection identity.
func (s *ResultsSession) OpenDetail(
	ctx context.Context, id int64, progress ResultsProgress,
	commit ResultsCommit,
) (err error) {
	r, err := s.Candidate(ctx, id)
	if err != nil {
		return err
	}
	if err = s.CloseDetail(ctx); err != nil {
		return err
	}
	if !s.identity.Same(s.app.Settings.StateDir) {
		return &Error{Code: "ui_collection_changed"}
	}
	reader, err := store.OpenSnapshotReader(ctx, s.app.Settings.StateDir)
	if err != nil {
		return classified(err)
	}
	s.nextDetail++
	d := &ResultsDetail{ID: s.nextDetail, ViewID: s.View.ID, CandidateID: id}
	progress("summary", 0)
	err = reader.VisitDetail(
		ctx, id, r.Candidate.Key, s.identity, s.app.Settings.StateDir, s.app.Clock, s.View.Evaluation.MaxAge,
		func(c state.Candidate, e state.Evaluation) error { d.Summary = c; d.Evaluation = e; return nil },
		func(row state.Sighting) error {
			progress("sources", d.Sources)
			e := s.data.AddHistory(ctx, d.ID, "sources", d.Sources, row)
			if e == nil {
				d.Sources++
				progress("sources", d.Sources)
			}
			return snapshotError(e)
		}, func(row store.DetailAttempt) error {
			progress("attempts", d.Attempts)
			e := s.data.AddHistory(ctx, d.ID, "attempts", d.Attempts, row)
			if e == nil {
				d.Attempts++
				progress("attempts", d.Attempts)
			}
			return snapshotError(e)
		},
	)
	closeErr := reader.Close()
	if err == nil {
		err = closeErr
	}
	if errors.Is(err, store.ErrNotFound) {
		err = &Error{Code: "ui_collection_changed"}
	}
	if err == nil {
		err = snapshotError(s.data.SaveDetail(ctx, d.ID, d.ViewID, d.Evaluation, d.Summary, d.Sources, d.Attempts))
	}
	if err == nil {
		err = commit(func() error { s.Detail = d; return nil })
	}
	if err != nil {
		s.oldDetail = d.ID
		_ = s.Prepare(context.Background())
		return classified(err)
	}
	return nil
}

// History returns exact private records for the terminal projection.
func (s *ResultsSession) History(ctx context.Context, section string, offset int64) ([]json.RawMessage, error) {
	if s.Detail == nil {
		return nil, &Error{Code: "ui_handle_stale"}
	}
	total := s.Detail.Sources
	if section == "attempts" {
		total = s.Detail.Attempts
	}
	if offset < 0 || offset > total {
		return nil, &Error{Code: "ui_page_invalid"}
	}
	rows, err := s.data.History(ctx, s.Detail.ID, section, offset)
	return rows, snapshotError(err)
}

// VisitExport streams only the reviewed view, holding storage until callbacks return.
func (s *ResultsSession) VisitExport(ctx context.Context, marked bool, emit func(state.Candidate) error) error {
	var offset int64
	for {
		rows, err := s.data.Page(ctx, s.View.ID, offset, marked)
		if err != nil {
			return snapshotError(err)
		}
		if len(rows) == 0 {
			return nil
		}
		for _, r := range rows {
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = emit(r.Candidate); err != nil {
				return err
			}
			offset = r.Ordinal + 1
		}
	}
}

// StateExclusion is the canonical state location captured by this snapshot.
func (s *ResultsSession) StateExclusion() string { return s.identity.Directory }
