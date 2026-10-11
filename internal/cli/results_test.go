package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// covers: spec 0006 AC-1, AC-4, AC-5, AC-6, AC-12. Every format crosses all internal pages.
func TestResults_CompleteCollectionsAndLimitBoundaries(t *testing.T) {
	configPath, stateDir, key := seedOutputStateCount(t, 338)
	for _, limit := range []int{0, 1, 100, 338, 339} {
		for _, format := range []string{"text", "json", "keys"} {
			t.Run(fmt.Sprintf("%s/%d", format, limit), func(t *testing.T) {
				args := []string{"export", "--format", format}
				if format == "text" {
					args = []string{"list"}
				}
				if limit > 0 {
					args = append(args, "--limit", strconv.Itoa(limit))
				}
				out, diagnostic, err := runOutputCommand(t.Context(), configPath, stateDir, nil, args...)
				require.NoError(t, err)
				count := 338
				if limit > 0 && limit < count {
					count = limit
				}
				truncated := count < 338
				if format == "json" {
					var doc struct {
						Items []struct {
							ID int64 `json:"id"`
						} `json:"items"`
						SchemaVersion int  `json:"schema_version"`
						ReturnedCount int  `json:"returned_count"`
						Limit         *int `json:"limit"`
						Truncated     bool `json:"truncated"`
					}
					require.NoError(t, json.Unmarshal([]byte(out), &doc))
					require.Equal(t, 1, doc.SchemaVersion)
					require.Equal(t, count, doc.ReturnedCount)
					require.Equal(t, truncated, doc.Truncated)
					if limit == 0 {
						require.Nil(t, doc.Limit)
					} else {
						require.Equal(t, limit, *doc.Limit)
					}
					require.Len(t, doc.Items, count)
					for i, item := range doc.Items {
						require.Equal(t, int64(i+1), item.ID)
					}
					require.Empty(t, diagnostic)
				} else {
					lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
					if format == "text" {
						lines = lines[1:]
					}
					require.Len(t, lines, count)
					for i, line := range lines {
						if format == "keys" {
							require.Equal(t, fmt.Sprintf("%08d-12345678-AbCdEf01", i+1), line)
						} else {
							require.Equal(t, strconv.Itoa(i+1), strings.Split(line, "\t")[0])
						}
					}
					limitText := "none"
					if limit != 0 {
						limitText = strconv.Itoa(limit)
					}
					require.Equal(t, fmt.Sprintf("returned_count=%d limit=%s truncated=%t\n",
						count, limitText, truncated), diagnostic)
				}
				if format != "keys" {
					require.NotContains(t, out, key)
				}
				require.NotContains(t, diagnostic, key)
			})
		}
	}
}

// covers: spec 0006 AC-2, AC-4, AC-6, AC-11. Parsing fails before configuration or staging.
func TestResults_InvalidOptionsDoNotAccessRuntimePaths(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	configPath := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(configPath, []byte("invalid config"), 0600))
	stateDir := filepath.Join(t.TempDir(), "absent")
	options := [][]string{
		{"--status", "Confirmed"}, {"--status", "confirmed,"}, {"--status", ""},
		{"--definitive", "inconclusive"}, {"--freshness", "never"},
		{"--limit", "0"}, {"--limit", "-1"}, {"--limit", "+1"}, {"--limit", "1.0"},
		{"--limit", ""}, {"--limit", "9223372036854775808"}, {"unexpected"},
	}
	for _, command := range []string{"list", "export"} {
		for _, option := range options {
			t.Run(command+"/"+strings.Join(option, " "), func(t *testing.T) {
				out, diagnostic, err := runOutputCommand(t.Context(), configPath, stateDir, nil,
					append([]string{command}, option...)...)
				require.ErrorIs(t, err, errArguments)
				require.Empty(t, out)
				require.Empty(t, diagnostic)
			})
		}
	}
	for _, option := range [][]string{
		{"--format", "csv"}, {"--format", "keys", "--show-keys"},
		{"--format", "keys", "--show-keys=false"},
	} {
		_, _, err := runOutputCommand(t.Context(), configPath, stateDir, nil,
			append([]string{"export"}, option...)...)
		require.ErrorIs(t, err, errArguments)
	}
	entries, err := os.ReadDir(temporary)
	require.NoError(t, err)
	require.Empty(t, entries)
	_, err = os.Stat(stateDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// covers: spec 0006 AC-2, AC-5, AC-7. Empty selections are explicit and filters have canonical metadata.
func TestResults_EmptySelectionsAndCanonicalFilters(t *testing.T) {
	configPath, stateDir, _ := seedOutputState(t)
	filters := []string{"--status", "invalid, confirmed", "--status", "invalid",
		"--definitive", "invalid,none", "--freshness", "unknown,fresh", "--limit", "1"}
	for _, format := range []string{"text", "json", "keys"} {
		args := []string{"export", "--format", format}
		if format == "text" {
			args = []string{"list"}
		}
		out, diagnostic, err := runOutputCommand(t.Context(), configPath, stateDir, nil, append(args, filters...)...)
		require.NoError(t, err)
		switch format {
		case "text":
			require.Equal(t, "No matching candidates.\n", out)
		case "keys":
			require.Empty(t, out)
		case "json":
			var doc map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(out), &doc))
			require.JSONEq(t, `[]`, string(doc["items"]))
			require.JSONEq(t, `{"status":["confirmed","invalid"],"definitive":["none","invalid"],`+
				`"freshness":["fresh","unknown"]}`, string(doc["filters"]))
			require.Equal(t, "0", string(doc["returned_count"]))
			require.Equal(t, "false", string(doc["truncated"]))
		}
		if format != "json" {
			require.Equal(t, "returned_count=0 limit=1 truncated=false\n", diagnostic)
		}
	}
}

// covers: spec 0006 AC-5, AC-7, AC-8, AC-11. Missing and damaged state cannot look like empty success.
func TestExport_EmptyStateDiffersFromUnavailableState(t *testing.T) {
	for _, kind := range []string{"empty", "missing", "damaged"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			configPath, stateDir := filepath.Join(root, "config.json"), filepath.Join(root, "state")
			require.NoError(t, os.WriteFile(configPath, []byte(`{}`), 0600))
			if kind != "missing" {
				w, err := store.OpenWriter(t.Context(), stateDir, time.Now)
				require.NoError(t, err)
				require.NoError(t, w.Close())
				if kind == "damaged" {
					require.NoError(t, os.WriteFile(filepath.Join(stateDir, "state.db"), []byte("broken"), 0600))
				}
			}
			for _, format := range []string{"json", "keys"} {
				out, diagnostic, err := runOutputCommand(t.Context(), configPath, stateDir, nil,
					"export", "--format", format)
				if kind != "empty" {
					require.Error(t, err)
					require.Empty(t, out)
					require.Empty(t, diagnostic)
					continue
				}
				require.NoError(t, err)
				if format == "keys" {
					require.Empty(t, out)
					require.Equal(t, "returned_count=0 limit=none truncated=false\n", diagnostic)
				} else {
					var doc map[string]json.RawMessage
					require.NoError(t, json.Unmarshal([]byte(out), &doc))
					require.Equal(t, "[]", string(doc["items"]))
					require.Equal(t, "null", string(doc["limit"]))
					require.Equal(t, "0", string(doc["returned_count"]))
					require.Equal(t, "false", string(doc["truncated"]))
				}
			}
		})
	}
}

// covers: spec 0006 AC-6, AC-8. A late unsafe key discards earlier prepared rows in every destination mode.
func TestExport_UnsafeKeysNeverPublishPartialSelections(t *testing.T) {
	for _, unsafe := range []string{"bad\nkey", "bad\rkey", "bad\tkey", "bad\u007fkey", "bad\u0085key",
		"bad\u2028key", "bad\u2029key"} {
		t.Run(strconv.Quote(unsafe), func(t *testing.T) {
			configPath, stateDir, key := seedOutputState(t)
			w, err := store.OpenWriter(t.Context(), stateDir, time.Now)
			require.NoError(t, err)
			run, err := w.StartRun(t.Context(), state.RunInput{
				RequestID: "unsafe", Kind: state.Crawl, StartedAt: time.Now(),
			})
			require.NoError(t, err)
			_, err = w.RecordMessage(t.Context(), state.MessageInput{
				RunID: run.ID, SourceID: "fixture", MessageID: "unsafe", ObservedAt: time.Now(), Keys: []string{unsafe},
			})
			require.NoError(t, err)
			require.NoError(t, w.Close())
			for _, named := range []bool{false, true} {
				destination := filepath.Join(t.TempDir(), "keys.txt")
				args := []string{"export", "--format", "keys"}
				if named {
					args = append(args, "--output", destination)
				}
				out, diagnostic, err := runOutputCommand(t.Context(), configPath, stateDir, nil, args...)
				require.Equal(t, "export_key_unrepresentable candidate_id=2", Diagnostic(err))
				require.Empty(t, out)
				require.Empty(t, diagnostic)
				_, err = os.Lstat(destination)
				require.ErrorIs(t, err, os.ErrNotExist)
				stages, err := filepath.Glob(filepath.Join(filepath.Dir(destination), "wpc-output-*"))
				require.NoError(t, err)
				require.Empty(t, stages)
			}
			out, diagnostic, err := runOutputCommand(t.Context(), configPath, stateDir, nil,
				"export", "--format", "keys", "--limit", "1")
			require.NoError(t, err, "the omitted extra match is not encoded")
			require.Equal(t, key+"\n", out)
			require.Contains(t, diagnostic, "truncated=true")
			out, _, err = runOutputCommand(t.Context(), configPath, stateDir, nil, "export", "--show-keys")
			require.NoError(t, err)
			var doc struct {
				Items []struct {
					Key string `json:"key"`
				} `json:"items"`
			}
			require.NoError(t, json.Unmarshal([]byte(out), &doc))
			require.Equal(t, unsafe, doc.Items[1].Key)
		})
	}
}

// covers: spec 0006 AC-2, AC-5, AC-6, AC-11. Export separates completed, definitive, and active attempts.
func TestExport_SummaryPreservesHistoryReferencesAndDisclosure(t *testing.T) {
	configPath, stateDir, key := seedOutputState(t)
	at := time.Date(2026, 10, 11, 0, 0, 0, 123456000, time.UTC)
	w, err := store.OpenWriter(t.Context(), stateDir, func() time.Time { return at })
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	crawl, err := w.StartRun(t.Context(), state.RunInput{
		RequestID: "rediscovery", Kind: state.Crawl, StartedAt: at,
	})
	require.NoError(t, err)
	_, err = w.RecordMessage(t.Context(), state.MessageInput{
		RunID: crawl.ID, SourceID: "fixture", MessageID: "later", ObservedAt: at, Keys: []string{key},
	})
	require.NoError(t, err)
	run, err := w.StartRun(t.Context(), state.RunInput{
		RequestID: "summary-history", Kind: state.Validation, StartedAt: at,
	})
	require.NoError(t, err)
	var attempts []state.Attempt
	for i := range 3 {
		// Timestamps run backwards, while attempt IDs still establish recency.
		started := at.Add(-time.Duration(i) * time.Minute)
		attempt, err := w.BeginAttempt(t.Context(), state.AttemptInput{
			RequestID: fmt.Sprintf("summary-%d", i), RunID: run.ID, CandidateID: 1,
			BackendID: "fixture", BackendVersion: "1", StartedAt: started,
		})
		require.NoError(t, err)
		attempts = append(attempts, attempt)
		if i == 2 {
			continue
		}
		completion := state.AttemptCompletion{
			AttemptID: attempt.ID, Outcome: state.Inconclusive, ReasonCode: "fixture_timeout", FinishedAt: started,
		}
		if i == 0 {
			completion.Outcome, completion.ReasonCode = state.Confirmed, ""
			completion.Evidence = &state.Evidence{Kind: state.AccountType, AccountType: "Unlimited", ObservedAt: at}
		}
		_, err = w.CompleteAttempt(t.Context(), completion)
		require.NoError(t, err)
	}
	for _, show := range []bool{false, true} {
		out, diagnostic, err := runOutputCommand(t.Context(), configPath, stateDir, nil,
			"export", fmt.Sprintf("--show-keys=%t", show))
		require.NoError(t, err)
		require.Empty(t, diagnostic)
		var doc struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &doc))
		require.Len(t, doc.Items, 1)
		item := doc.Items[0]
		expected := map[string]string{
			"id": "1", "first_seen_at": `"2026-10-11T00:00:00Z"`,
			"last_seen_at": `"2026-10-11T00:00:00.123456Z"`, "latest_outcome": `"inconclusive"`,
			"latest_reason": `"fixture_timeout"`, "last_definitive": `"confirmed"`,
			"evidence_at":           `"2026-10-11T00:00:00.123456Z"`,
			"latest_attempt_id":     fmt.Sprint(attempts[1].ID),
			"definitive_attempt_id": fmt.Sprint(attempts[0].ID), "active_attempt_id": fmt.Sprint(attempts[2].ID),
		}
		for field, want := range expected {
			require.Equal(t, want, string(item[field]), field)
		}
		require.Contains(t, item, "freshness")
		require.Contains(t, item, "clock_anomaly")
		if show {
			require.Len(t, item, 13)
			require.Equal(t, strconv.Quote(key), string(item["key"]))
		} else {
			require.Len(t, item, 12)
			require.NotContains(t, item, "key")
			require.NotContains(t, out, key)
		}
	}
	r, err := store.OpenReader(t.Context(), stateDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	candidate, err := r.GetCandidate(t.Context(), 1, state.Evaluation{AsOf: at, MaxAge: time.Hour})
	require.NoError(t, err)
	require.NotNil(t, candidate.UnresolvedAttempt, "export must not recover an unresolved attempt")
	require.Equal(t, attempts[2].ID, candidate.UnresolvedAttempt.ID)
}
