package terminal

import (
	"context"
	"os"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// ExportReview binds a destination and disclosure choice to a fixed view revision.
type ExportReview struct {
	ID                             int64
	View                           app.ResultsView
	Selection, Format, Destination string
	IncludeKeys                    bool
	Count                          int64
	ParentIdentity                 os.FileInfo
	Exclusions                     []string
}

// ExportReceipt retains observed publication facts even when delivery fails.
type ExportReceipt struct {
	Destination, Selection, Format   string
	IncludeKeys                      bool
	Count, Prepared                  int64
	Published                        *bool
	Durability, ErrorCode, Remaining string
}

// ExportRows streams exact private records synchronously, with no live reader.
type ExportRows func(context.Context, func(state.Candidate) error) error

// ExportOperation uses CLI encoding and publication without printing over Ink.
type ExportOperation func(context.Context, ExportReview, ExportRows, func(string, ExportReceipt)) ExportReceipt

// ExportDestination validates paths without creating staging files.
type ExportDestination func(context.Context, string, []string) (string, os.FileInfo, error)

func reviewRecord(r ExportReview) any {
	return record{
		"review_id": decimal(r.ID), "snapshot_id": decimal(r.View.SnapshotID), "view_id": decimal(r.View.ID),
		"selection": r.Selection, "selected_count": decimal(r.Count), "matching_count": decimal(r.View.Count),
		"filters": r.View.Filters, "as_of": stamp(r.View.Evaluation.AsOf),
		"max_age_ns": decimal(int64(r.View.Evaluation.MaxAge)),
		"retained":   r.View.Retained, "format": r.Format, "include_keys": r.IncludeKeys, "destination": r.Destination,
	}
}

func receiptRecord(r ExportReceipt) any {
	return record{
		"destination": r.Destination, "selection": r.Selection, "format": r.Format, "include_keys": r.IncludeKeys,
		"selected_count": decimal(r.Count), "prepared_count": decimal(r.Prepared), "published": r.Published,
		"durability": r.Durability, "error_code": nullable(r.ErrorCode), "remaining_path": nullable(r.Remaining),
	}
}
