package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
)

// ReportFocus binds a section position to saved structural identities.
type ReportFocus struct {
	Section                            string
	SourceID                           string
	CandidateID, PageNumber, AttemptID *int64
	Offset                             int64
}

// ReportView is a fixed header whose counts share its observation transaction.
type ReportView struct {
	ID                              int64
	Header                          store.ReportHeader
	Sources, Pages, Items, Attempts int64
	Legacy                          bool
	Focus                           ReportFocus
	Retained                        bool
	ErrorCode                       string
}

// ReportSession owns one complete snapshot plus a replacement under preparation.
// Its caller serializes access through the common preparation job.
type ReportSession struct {
	app                *App
	next               int64
	data, retired      *store.BrowseStore
	identity           store.CollectionIdentity
	View               *ReportView
	Warning, Remaining string
	handleID           int64
}

// ValidateFocus rejects a refresh position that is not part of this saved report.
func (s *ReportSession) ValidateFocus(ctx context.Context, f ReportFocus) error {
	invalid := &Error{Code: "state_invalid_input"}
	if f.Section == "summary" {
		if f.SourceID != "" || f.CandidateID != nil || f.PageNumber != nil || f.AttemptID != nil || f.Offset != 0 {
			return invalid
		}
		return nil
	}
	if s.View == nil {
		return &Error{Code: "ui_handle_stale"}
	}
	crawl := s.View.Header.Crawl != nil
	if crawl && f.Section != "sources" && f.Section != "pages" {
		return invalid
	}
	if !crawl && f.Section != "items" && f.Section != "attempts" {
		return invalid
	}
	if f.Section == "sources" && (f.CandidateID != nil || f.PageNumber != nil || f.AttemptID != nil) {
		return invalid
	}
	if f.Section == "pages" && (f.CandidateID != nil || f.AttemptID != nil || f.SourceID == "") {
		return invalid
	}
	if f.Section == "items" && (f.SourceID != "" || f.PageNumber != nil || f.AttemptID != nil || s.View.Legacy) {
		return invalid
	}
	if f.Section == "attempts" && (f.SourceID != "" || f.PageNumber != nil ||
		(s.View.Legacy != (f.CandidateID == nil))) {
		return invalid
	}
	parent := reportParent(f)
	if parent != "" {
		section := "sources"
		if f.Section == "attempts" {
			section = "items"
		}
		if _, err := s.data.ReportPosition(ctx, section, "", parent); err != nil {
			return invalid
		}
	}
	total, err := s.data.ReportCount(ctx, f.Section, &parent)
	if err != nil {
		return snapshotError(err)
	}
	identity := reportIdentity(f)
	if total == 0 {
		if f.Offset != 0 || identity != "" {
			return invalid
		}
		return nil
	}
	position, err := s.data.ReportPosition(ctx, f.Section, parent, identity)
	if err != nil || position != f.Offset {
		return invalid
	}
	return nil
}

// NewReportSession performs no state access until a run is explicitly opened.
func (a *App) NewReportSession() *ReportSession { return &ReportSession{app: a} }

func (s *ReportSession) prepare() error {
	s.Warning, s.Remaining = "", ""
	if s.retired != nil {
		if err := s.retired.Close(); err != nil {
			s.Remaining = s.retired.Directory
			return &Error{Code: "ui_snapshot_cleanup_failed"}
		}
		s.retired = nil
	}
	return nil
}

// Prepare retires failed temporary cleanup before any new preparation can begin.
func (s *ReportSession) Prepare() error { return s.prepare() }

// Close discards only owned temporary records after entered reads have finished.
func (s *ReportSession) Close() error {
	err := s.prepare()
	if s.data != nil {
		if e := s.data.Close(); e != nil {
			s.Remaining = s.data.Directory
			return &Error{Code: "ui_snapshot_cleanup_failed"}
		}
		s.data, s.View = nil, nil
		s.handleID = 0
	}
	return err
}

// Require validates both the handle and its collection identity without a writer.
func (s *ReportSession) Require(id int64) error {
	if s.View == nil || s.View.ID != id {
		return &Error{Code: "ui_handle_stale"}
	}
	if err := s.identity.Check(s.app.Settings.StateDir); errors.Is(err, store.ErrCollectionChanged) {
		s.View = nil
		return &Error{Code: "ui_collection_changed"}
	} else if err != nil && !errors.Is(err, store.ErrStateAccess) {
		return classified(err)
	}
	// Cached reads remain available after failed live admission. Refresh still
	// requires private admission and records its failure on the retained view.
	return nil
}

// Open prepares a complete run snapshot and activates it only at the commit gate.
func (s *ReportSession) Open(
	ctx context.Context, runID int64, focus *ReportFocus,
	progress ResultsProgress, commit ResultsCommit,
) (err error) {
	if err = s.prepare(); err != nil {
		return err
	}
	old := s.View
	defer func() {
		if errors.Is(err, store.ErrCollectionChanged) {
			s.View = nil
		}
		if err != nil && focus != nil && old != nil && s.View == old {
			old.Retained, old.ErrorCode = true, classified(err).(*Error).Code
		}
	}()
	if focus != nil && old != nil {
		if err = s.identity.Check(s.app.Settings.StateDir); err != nil {
			return classified(err)
		}
	}
	identity, err := store.IdentifyCollection(s.app.Settings.StateDir)
	if err != nil {
		return classified(err)
	}
	data, err := store.NewReportStore(ctx)
	if err != nil {
		if data != nil {
			s.retired = data
		}
		return snapshotError(err)
	}
	activated := false
	defer func() {
		if !activated {
			s.retired = data
			if e := s.prepare(); e != nil && err == nil {
				err = e
			}
		}
	}()
	relative, pathErr := filepath.Rel(identity.Directory, data.Directory)
	if pathErr != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return &Error{Code: "ui_snapshot_failed"}
	}
	r, err := store.OpenSnapshotReader(ctx, s.app.Settings.StateDir)
	if err != nil {
		return classified(err)
	}
	s.next++
	v := &ReportView{ID: s.next, Focus: ReportFocus{Section: "summary"}}
	var count int64
	err = r.VisitReport(
		ctx, runID, identity, s.app.Settings.StateDir, s.app.Clock,
		func(h store.ReportHeader) error {
			v.Header = h
			v.Legacy = (h.Crawl != nil && len(h.Crawl.Sources) == 0) || (h.Check != nil && h.Check.Policy == nil)
			return nil
		}, func(section, parent string, row any) error {
			var id string
			switch t := row.(type) {
			case state.CrawlSource:
				id = t.SourceID
				v.Sources++
			case state.CrawlPageRecord:
				id = strconv.Itoa(t.PageNumber)
				v.Pages++
			case state.ValidationItem:
				id = strconv.FormatInt(t.CandidateID, 10)
				v.Items++
			case store.DetailAttempt:
				id = strconv.FormatInt(t.Attempt.ID, 10)
				v.Attempts++
			}
			if e := data.AddReport(ctx, section, parent, id, row); e != nil {
				return snapshotError(e)
			}
			count++
			progress("report", count)
			return nil
		},
	)
	err = errors.Join(err, r.Close())
	if err != nil {
		return classified(err)
	}
	if err = identity.Check(s.app.Settings.StateDir); err != nil {
		return classified(err)
	}
	if focus != nil && old != nil {
		if err = s.identity.Check(s.app.Settings.StateDir); err != nil {
			return classified(err)
		}
	}
	if focus != nil {
		v.Focus = *focus
		if err = restoreReportFocus(ctx, data, &v.Focus); err != nil {
			return snapshotError(err)
		}
	}
	err = commit(
		func() error {
			s.retired, s.data, s.View, s.identity = s.data, data, v, identity
			s.handleID = v.ID
			activated = true
			return nil
		},
	)
	if err == nil {
		if s.prepare() != nil {
			s.Warning = "ui_snapshot_cleanup_failed"
		}
	}
	return err
}

func reportParent(f ReportFocus) string {
	if f.Section == "pages" {
		return f.SourceID
	}
	if f.Section == "attempts" && f.CandidateID != nil {
		return strconv.FormatInt(*f.CandidateID, 10)
	}
	return ""
}

func reportIdentity(f ReportFocus) string {
	switch f.Section {
	case "sources":
		return f.SourceID
	case "pages":
		if f.PageNumber != nil {
			return strconv.FormatInt(*f.PageNumber, 10)
		}
	case "items":
		if f.CandidateID != nil {
			return strconv.FormatInt(*f.CandidateID, 10)
		}
	case "attempts":
		if f.AttemptID != nil {
			return strconv.FormatInt(*f.AttemptID, 10)
		}
	}
	return ""
}

func restoreReportFocus(ctx context.Context, data *store.BrowseStore, f *ReportFocus) error {
	if f.Section == "summary" {
		*f = ReportFocus{Section: "summary"}
		return nil
	}
	parent := reportParent(*f)
	if parent != "" {
		section := "sources"
		if f.Section == "attempts" {
			section = "items"
		}
		if _, err := data.ReportPosition(ctx, section, "", parent); errors.Is(err, store.ErrNotFound) {
			*f = ReportFocus{Section: section}
			parent = ""
		} else if err != nil {
			return err
		}
	}
	if identity := reportIdentity(*f); identity != "" {
		n, err := data.ReportPosition(ctx, f.Section, parent, identity)
		if err == nil {
			f.Offset = n
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	n, err := data.ReportCount(ctx, f.Section, &parent)
	if err != nil {
		return err
	}
	f.Offset = max(int64(0), min(f.Offset, n-1))
	_, ids, err := data.ReportPage(ctx, f.Section, parent, f.Offset)
	if err != nil {
		return err
	}
	f.PageNumber, f.AttemptID = nil, nil
	if f.Section == "sources" {
		f.SourceID = ""
	}
	if f.Section == "items" {
		f.CandidateID = nil
	}
	if len(ids) > 0 {
		id, _ := strconv.ParseInt(ids[0], 10, 64)
		switch f.Section {
		case "sources":
			f.SourceID = ids[0]
		case "pages":
			f.PageNumber = &id
		case "items":
			f.CandidateID = &id
		case "attempts":
			f.AttemptID = &id
		}
	}
	return nil
}

// Page reads a bounded section after validating its parent membership.
func (s *ReportSession) Page(ctx context.Context, f ReportFocus) ([]json.RawMessage, int64, error) {
	if s.View == nil {
		return nil, 0, &Error{Code: "ui_handle_stale"}
	}
	crawl := s.View.Header.Crawl != nil
	if (crawl && f.Section != "sources" && f.Section != "pages") ||
		(!crawl && f.Section != "items" && f.Section != "attempts") ||
		(f.Section == "items" && s.View.Legacy) {
		return nil, 0, &Error{Code: "state_invalid_input"}
	}
	parent := reportParent(f)
	if f.Section == "pages" || (f.Section == "attempts" && !s.View.Legacy) {
		section := "sources"
		if f.Section == "attempts" {
			section = "items"
		}
		if _, err := s.data.ReportPosition(ctx, section, "", parent); err != nil {
			return nil, 0, &Error{Code: "state_invalid_input"}
		}
	}
	total, err := s.data.ReportCount(ctx, f.Section, &parent)
	if err != nil {
		return nil, 0, snapshotError(err)
	}
	if f.Offset < 0 || f.Offset > total {
		return nil, 0, &Error{Code: "ui_page_invalid"}
	}
	rows, _, err := s.data.ReportPage(ctx, f.Section, parent, f.Offset)
	return rows, total, snapshotError(err)
}

// HandleID retains ownership of an invalidated report until explicit closure.
func (s *ReportSession) HandleID() int64 { return s.handleID }
