package cli

import (
	"context"
	"encoding/json"
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
				if mode == "cancel_at_link" && phase == "publication" && r.Published == nil {
					cancel()
				}
			})
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
