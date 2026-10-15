package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/app"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/config"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/terminal"
	"github.com/stretchr/testify/require"
)

// Spec 0011 AC-7 through AC-9: selected exports use the displayed snapshot even
// when the live collection has disappeared, and preserve exact schema metadata.
func TestTerminalExport_RetainedMarkedSnapshot(t *testing.T) {
	_, dir, _ := seedOutputStateCount(t, 338)
	a := &app.App{Settings: config.Settings{StateDir: dir, RecheckInterval: time.Hour}, Clock: time.Now}
	s := a.NewResultsSession()
	defer func() { require.NoError(t, s.Close()) }()
	ctx := t.Context()
	commit := func(f func() error) error { return f() }
	require.NoError(t, s.Open(ctx, false, nil, func(string, int64) {}, commit))
	require.NoError(t, s.Mark(ctx, 1, true))
	require.NoError(t, s.Mark(ctx, 338, true))
	exclusions := []string{s.StateExclusion(), dir}
	require.NoError(t, os.Rename(dir, dir+"-moved"))
	dest, info, err := terminalDestination(ctx, filepath.Join(t.TempDir(), "selected.json"), exclusions)
	require.NoError(t, err)
	review := terminal.ExportReview{View: *s.View, Selection: "marked", Format: "json", Count: 2,
		Destination: dest, ParentIdentity: info,
		Exclusions: exclusions}
	receipt := terminalExport(ctx, review, func(ctx context.Context, emit func(state.Candidate) error) error {
		return s.VisitExport(ctx, true, emit)
	}, func(string, terminal.ExportReceipt) {})
	require.Empty(t, receipt.ErrorCode)
	require.Equal(t, true, *receipt.Published)
	require.Equal(t, "confirmed", receipt.Durability)
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	var doc struct {
		Items     []candidateJSON `json:"items"`
		Selection exportSelection `json:"selection"`
		Returned  int64           `json:"returned_count"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Equal(t, int64(2), doc.Returned)
	require.Equal(t, exportSelection{Mode: "marked", MatchingCount: 338}, doc.Selection)
	require.Equal(t, int64(338), doc.Items[1].ID)
	require.Nil(t, doc.Items[0].Key)
	_, _, err = terminalDestination(ctx, filepath.Join(dir, "inside.json"), exclusions)
	require.Error(t, err)
}

// covers: spec 0011 AC-7, AC-8, AC-9. Empty exports and exact integer JSON retain the public format.
func TestTerminalExport_ExactFormatsAndEmptyMatching(t *testing.T) {
	for _, tc := range []struct {
		name, format string
		keys, empty  bool
	}{
		{"hidden JSON", "json", false, false},
		{"disclosed JSON", "json", true, false},
		{"exact lines", "keys", true, false},
		{"empty JSON", "json", false, true},
		{"empty lines", "keys", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			review := terminalExportReviewFixture(t, tc.format, tc.keys)
			if tc.empty {
				review.Count, review.View.Count = 0, 0
			}
			const id int64 = 9007199254740993
			const key = "  exact-é-value  "
			receipt := terminalExport(t.Context(), review,
				func(_ context.Context, emit func(state.Candidate) error) error {
					if tc.empty {
						return nil
					}
					return emit(state.Candidate{ID: id, Key: key, LatestOutcome: state.Unchecked})
				}, func(string, terminal.ExportReceipt) {})
			require.Empty(t, receipt.ErrorCode)
			require.True(t, *receipt.Published)
			require.Equal(t, "confirmed", receipt.Durability)
			require.Equal(t, review.Count, receipt.Prepared)
			info, err := os.Stat(review.Destination)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), info.Mode().Perm())
			data, err := os.ReadFile(review.Destination)
			require.NoError(t, err)
			if tc.format == "keys" {
				want := key + "\n"
				if tc.empty {
					want = ""
				}
				require.Equal(t, want, string(data))
				return
			}
			var doc struct {
				Schema    int             `json:"schema_version"`
				Items     []candidateJSON `json:"items"`
				Selection exportSelection `json:"selection"`
				Count     int64           `json:"returned_count"`
				Limit     *int64          `json:"limit"`
				Truncated bool            `json:"truncated"`
			}
			require.NoError(t, json.Unmarshal(data, &doc))
			require.Equal(t, 1, doc.Schema)
			require.Nil(t, doc.Limit)
			require.False(t, doc.Truncated)
			require.Equal(t, review.Count, doc.Count)
			require.Equal(t, exportSelection{Mode: "matching", MatchingCount: review.Count}, doc.Selection)
			require.Len(t, doc.Items, int(review.Count))
			if !tc.empty {
				require.Equal(t, id, doc.Items[0].ID)
				if tc.keys {
					require.Equal(t, key, *doc.Items[0].Key)
				} else {
					require.Nil(t, doc.Items[0].Key)
					require.NotContains(t, string(data), key)
				}
			}
			require.Equal(t, byte('\n'), data[len(data)-1])
		})
	}
}

// covers: spec 0011 AC-8, AC-9, AC-12. Any row failure discards all prepared output.
func TestTerminalExport_RowFailuresNeverPublish(t *testing.T) {
	for _, mode := range []string{"missing row", "extra row", "source error", "invalid UTF8", "line separator"} {
		t.Run(mode, func(t *testing.T) {
			review := terminalExportReviewFixture(t, "keys", true)
			receipt := terminalExport(t.Context(), review,
				func(_ context.Context, emit func(state.Candidate) error) error {
					if mode == "missing row" {
						return nil
					}
					key := "safe"
					if mode == "invalid UTF8" {
						key = "invalid\xff"
					}
					if mode == "line separator" {
						key = "unsafe\u2028line"
					}
					if err := emit(state.Candidate{ID: 1, Key: key}); err != nil {
						return err
					}
					if mode == "extra row" {
						return emit(state.Candidate{ID: 2, Key: "second"})
					}
					return errors.New("private source error")
				}, func(string, terminal.ExportReceipt) {})
			require.NotEmpty(t, receipt.ErrorCode)
			require.NotContains(t, receipt.ErrorCode, "private")
			require.False(t, *receipt.Published)
			require.Equal(t, "not_published", receipt.Durability)
			entries, err := os.ReadDir(filepath.Dir(review.Destination))
			require.NoError(t, err)
			require.Empty(t, entries, "failed preparation must remove its stage and leave no destination")
		})
	}
}

// covers: spec 0011 AC-9. Atomic publication refuses directories and symlinks created after review.
func TestTerminalExport_PreservesDestinationRaces(t *testing.T) {
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			review := terminalExportReviewFixture(t, "keys", true)
			target := filepath.Join(t.TempDir(), "target")
			require.NoError(t, os.WriteFile(target, []byte("untouched"), 0600))
			receipt := terminalExport(t.Context(), review,
				func(_ context.Context, emit func(state.Candidate) error) error {
					if kind == "directory" {
						require.NoError(t, os.Mkdir(review.Destination, 0700))
					} else {
						require.NoError(t, os.Symlink(target, review.Destination))
					}
					return emit(state.Candidate{ID: 1, Key: "exported"})
				}, func(string, terminal.ExportReceipt) {})
			require.Equal(t, "export_exists", receipt.ErrorCode)
			require.False(t, *receipt.Published)
			info, err := os.Lstat(review.Destination)
			require.NoError(t, err)
			if kind == "directory" {
				require.True(t, info.IsDir())
			} else {
				require.NotZero(t, info.Mode()&os.ModeSymlink)
			}
			data, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, "untouched", string(data))
			entries, err := os.ReadDir(filepath.Dir(review.Destination))
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

func terminalExportReviewFixture(t *testing.T, format string, keys bool) terminal.ExportReview {
	t.Helper()
	parent := t.TempDir()
	exclusions := []string{filepath.Join(t.TempDir(), "absent-state")}
	dest, info, err := terminalDestination(t.Context(), filepath.Join(parent, "result"), exclusions)
	require.NoError(t, err)
	q, err := (app.CandidateQuery{}).Normalize()
	require.NoError(t, err)
	return terminal.ExportReview{View: app.ResultsView{Filters: q, Count: 1,
		Evaluation: state.Evaluation{AsOf: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), MaxAge: time.Hour}},
		Selection: "matching", Format: format, IncludeKeys: keys, Count: 1,
		Destination: dest, ParentIdentity: info, Exclusions: exclusions}
}

// Spec 0011 AC-9, AC-10, AC-15: deterministic barriers distinguish cancellation
// before publication from cancellation racing with the successful atomic link.
func TestTerminalExport_PublicationFacts(t *testing.T) {
	for _, mode := range []string{"cancel_before", "cancel_at_link", "destination_race", "unsafe_key", "changed_parent"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			root := t.TempDir()
			parent := filepath.Join(root, "output")
			require.NoError(t, os.Mkdir(parent, 0700))
			dest, info, err := terminalDestination(
				ctx, filepath.Join(parent, "result"), []string{filepath.Join(root, "absent-state")},
			)
			require.NoError(t, err)
			q, err := (app.CandidateQuery{}).Normalize()
			require.NoError(t, err)
			review := terminal.ExportReview{View: app.ResultsView{Filters: q, Count: 1,
				Evaluation: state.Evaluation{AsOf: time.Now(), MaxAge: time.Hour}}, Selection: "matching", Format: "keys",
				IncludeKeys: true, Count: 1, Destination: dest, ParentIdentity: info,
				Exclusions: []string{filepath.Join(root, "state")}}
			if mode == "changed_parent" {
				require.NoError(t, os.Rename(parent, parent+"-old"))
				require.NoError(t, os.Mkdir(parent, 0700))
			}
			var finalPhase string
			receipt := terminalExport(ctx, review, func(ctx context.Context, emit func(state.Candidate) error) error {
				key := "exact-key"
				if mode == "unsafe_key" {
					key = "unsafe\nkey"
				}
				err := emit(state.Candidate{ID: 9007199254740993, Key: key})
				if mode == "cancel_before" {
					cancel()
				}
				if mode == "destination_race" {
					require.NoError(t, os.WriteFile(dest, []byte("existing"), 0600))
				}
				return err
			}, func(phase string, r terminal.ExportReceipt) {
				finalPhase = phase
				if mode == "cancel_at_link" && phase == "publication" && r.Published == nil {
					cancel()
				}
			})
			wantPhase := "encoding"
			switch mode {
			case "changed_parent":
				wantPhase = "destination"
			case "cancel_at_link", "destination_race":
				wantPhase = "publication"
			}
			require.Equal(t, wantPhase, finalPhase, "cleanup must not replace the original failure phase")
			switch mode {
			case "cancel_at_link":
				require.Equal(t, true, *receipt.Published)
				require.Equal(t, "uncertain", receipt.Durability)
				data, err := os.ReadFile(dest)
				require.NoError(t, err)
				require.Equal(t, "exact-key\n", string(data))
			case "destination_race":
				require.False(t, *receipt.Published)
				require.Equal(t, "export_exists", receipt.ErrorCode)
				data, err := os.ReadFile(dest)
				require.NoError(t, err)
				require.Equal(t, "existing", string(data))
			default:
				require.False(t, *receipt.Published)
				_, err := os.Lstat(dest)
				require.True(t, os.IsNotExist(err))
			}
			require.NotEmpty(t, receipt.ErrorCode)
			entries, err := os.ReadDir(parent)
			require.NoError(t, err)
			for _, entry := range entries {
				require.Equal(t, "result", entry.Name())
			}
		})
	}
}
