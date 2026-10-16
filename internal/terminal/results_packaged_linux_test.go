package terminal_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var terminalControls = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
var candidateLine = regexp.MustCompile(`(?m)^[> ] \[[ x]\] (?:Candidate )?([0-9]+)`)

type resultsDriver struct {
	t      *testing.T
	master *os.File
	output *terminalOutput
}

// wait requires a fresh, complete Ink frame. Earlier content and loading frames
// cannot satisfy an action's assertion, even when its label is unchanged.
func (d resultsDriver) wait(after int, needle string) string {
	d.t.Helper()
	defer func() {
		if d.t.Failed() {
			d.t.Logf("terminal output after %d waiting for %q: %q", after, needle, d.output.text()[after:])
		}
	}()
	var frame string
	require.Eventually(d.t, func() bool {
		var ready bool
		frame, ready = readyTerminalFrame(d.output.text(), after, needle)
		return ready
	}, 10*time.Second, 10*time.Millisecond, "missing fresh ready frame for %q", needle)
	return frame
}

func readyTerminalFrame(raw string, after int, needle string) (string, bool) {
	start := strings.LastIndex(raw, "\x1b[?2026h")
	if start < after {
		return "", false
	}
	end := strings.Index(raw[start:], "\x1b[?2026l")
	if end < 0 {
		return "", false
	}
	frame := terminalControls.ReplaceAllString(raw[start:start+end], "")
	return frame, strings.Contains(frame, needle) &&
		!strings.Contains(frame, "c or Esc cancel | ? help | q quit") &&
		!strings.Contains(frame, "Preparing:")
}

// A loading controls screen already contains its destination label, but ignores
// navigation until the options reply arrives. Never send the next key to it.
func TestPackagedDriverWaitsForControlsReady(t *testing.T) {
	const label = "Select configured sources"
	frame := func(body string) string { return "\x1b[?2026h" + body + "\x1b[?2026l" }
	loading := frame("WPC | crawl | Idle\r\nPreparing: Waiting for parent\r\n" + label)
	ready := frame("WPC | crawl | Idle\r\n" + label + "\r\n> [x] warpplus | 1 pages")
	_, accepted := readyTerminalFrame(loading, 0, label)
	require.False(t, accepted, "a loading crawl must not admit the next navigation key")
	_, accepted = readyTerminalFrame(loading+ready, len(loading), label)
	require.True(t, accepted, "the completed options reply permits navigation")
	_, accepted = readyTerminalFrame(ready+loading, len(ready), label)
	require.False(t, accepted, "an earlier ready frame cannot satisfy a later request")
	_, accepted = readyTerminalFrame(ready, len(ready), label)
	require.False(t, accepted, "a frame before the input cannot satisfy it")
	_, accepted = readyTerminalFrame(strings.TrimSuffix(ready, "\x1b[?2026l"), 0, label)
	require.False(t, accepted, "a partially written frame is not ready")
}

func (d resultsDriver) send(keys, needle string) string {
	d.t.Helper()
	after := len(d.output.text())
	_, err := d.master.WriteString(keys)
	require.NoError(d.t, err)
	return d.wait(after, needle)
}

func (d resultsDriver) resize(rows, columns uint16, needle string) string {
	d.t.Helper()
	after := len(d.output.text())
	require.NoError(d.t, unix.IoctlSetWinsize(int(d.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Row: rows, Col: columns}))
	return d.wait(after, needle)
}

// Spec 0011 AC-1, AC-3, AC-5 through AC-9, AC-11, AC-14 and AC-16.
// The same keyboard journey runs on each native host and its baseline archive.
func verifyPackagedResults(t *testing.T, executable, dir string, master *os.File, output *terminalOutput, mode string) {
	t.Helper()
	d := resultsDriver{t, master, output}
	compact := mode == "results_compact"
	viewport := 23
	if compact {
		viewport = 3
	}
	frame := d.send("b", fmt.Sprintf("Rows 1 to %d of 338", viewport))
	if mode == "results_failure" {
		verifyResultsFailures(d, dir)
		_, err := master.WriteString("q")
		require.NoError(t, err)
		return
	}
	// Page Down traverses disjoint visible ranges, not just the first and last ID.
	for first := 1; first <= 338; first += viewport {
		last := min(first+viewport-1, 338)
		if first != 1 {
			frame = d.send("\x1b[6~", fmt.Sprintf("Rows %d to %d of 338", first, last))
		}
		matches := candidateLine.FindAllStringSubmatch(strings.ReplaceAll(frame, "\r", ""), -1)
		require.Len(t, matches, last-first+1)
		for i, row := range matches {
			require.Equal(t, strconv.Itoa(first+i), row[1])
		}
		require.NotContains(t, frame, "synthetic_private_key")
	}
	d.send("\x1b[F", "Rows 338 to 338 of 338")
	d.send(" ", "1 marked")
	d.send("\x1b[H", fmt.Sprintf("Rows 1 to %d of 338", viewport))
	d.send("\r", "Sources 1")
	if compact {
		d.send("\x1b[F", "Key hidden.")
	}
	d.send("k", `Key "synthetic_private_key_`)
	d.send("?", "Keyboard help")
	frame = d.send("\x1b", "Key hidden.")
	require.NotContains(t, frame, "synthetic_private_key")
	if compact {
		d.send("k", `Key "synthetic_private_key_`)
		d.resize(14, 50, "Resize to at least")
		frame = d.resize(16, 60, "Key hidden.")
		require.NotContains(t, frame, "synthetic_private_key")
	}
	d.send("\x1b", fmt.Sprintf("Rows 1 to %d of 338", viewport))
	// A draft and unchanged submission preserve marks; a changed filter clears them.
	d.send("f", "Applying changed filters clears marks.")
	d.send("\r", "1 marked")
	d.send("f", "Applying changed filters clears marks.")
	d.send(" ", "> [x] unchecked")
	d.send("\r", "0 marked")
	d.send(" ", "1 marked")
	d.send("\x1b[F", "Rows 338 to 338 of 338")
	d.send(" ", "2 marked")
	d.send("\x1b[H", fmt.Sprintf("Rows 1 to %d of 338", viewport))
	for _, format := range []string{"json", "keys"} {
		path := filepath.Join(t.TempDir(), "qre-export."+format)
		selected := 338
		if format == "keys" {
			selected = 2
		}
		d.exportForm(path, format, format == "keys")
		d.send("\r", "Selection "+map[bool]string{false: "matching", true: "marked"}[format == "keys"])
		// Back is initially focused and has not created any destination.
		_, err := os.Lstat(path)
		require.True(t, os.IsNotExist(err))
		d.send("\t", "> Create file")
		d.send("\r", fmt.Sprintf("%d prepared of %d selected", selected, selected))
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		if format == "json" {
			assertSummaryExport(t, data, 338)
		} else {
			reader, err := store.OpenReader(t.Context(), dir)
			require.NoError(t, err)
			var expected strings.Builder
			for _, id := range []int64{1, 338} {
				candidate, err := reader.GetCandidate(t.Context(), id, state.Evaluation{AsOf: time.Now(), MaxAge: time.Hour})
				require.NoError(t, err)
				expected.WriteString(candidate.Key + "\n")
			}
			require.NoError(t, reader.Close())
			require.Equal(t, expected.String(), string(data))
		}
		d.send("\r", fmt.Sprintf("Rows 1 to %d of 338", viewport))
	}
	// Explicit export still runs as a normal command against the same collection.
	cmd := exec.Command(executable, "export", "--state-dir", dir)
	data, err := cmd.Output()
	require.NoError(t, err)
	assertSummaryExport(t, data, 338)
	d.send("\x1b", "Overview  | Ready")
	d.send("b", fmt.Sprintf("Rows 1 to %d of 338", viewport))
	_, err = master.WriteString("q")
	require.NoError(t, err)
}

func (d resultsDriver) exportForm(path, format string, marked bool) {
	d.t.Helper()
	d.send("e", "> Selection: matching")
	if marked {
		d.send(" ", "> Selection: marked")
	}
	d.send("\t", "> Format: json")
	if format == "keys" {
		d.send(" ", "> Format: keys")
	}
	d.send("\t", map[bool]string{false: "> [ ] Include keys", true: "> Contains complete keys"}[format == "keys"])
	d.send("\t", "> Destination:")
	d.send(path, "> Destination:")
	d.send("\t", "> Review export")
}

func verifyResultsFailures(d resultsDriver, dir string) {
	t := d.t
	t.Helper()
	path := filepath.Join(t.TempDir(), "existing")
	require.NoError(t, os.WriteFile(path, []byte("keep existing contents"), 0600))
	d.exportForm(path, "json", false)
	d.send("\r", "The destination already exists.")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "keep existing contents", string(data))
	d.send("\x1b", "Rows 1 to 23 of 338")
	d.exportForm(filepath.Join(t.TempDir(), "empty-marks"), "json", true)
	d.send("\r", "No marked candidates.")
	d.send("\x1b", "Rows 1 to 23 of 338")
	d.send(" ", "1 marked")
	require.NoError(t, os.Rename(dir, dir+"-moved"))
	d.send("r", "Retained after refresh failure")
	frame := d.send("\r", "Detail history unavailable or not loaded.")
	require.NotContains(t, frame, "Sources 0")
	d.send("\x1b", "Rows 1 to 23 of 338")
	path = filepath.Join(t.TempDir(), "retained.json")
	d.exportForm(path, "json", false)
	d.send("\r", "Retained after refresh failure")
	d.send("\t", "> Create file")
	d.send("\r", "338 prepared of 338 selected")
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assertSummaryExport(t, data, 338)
	d.send("\r", "Rows 1 to 23 of 338")
}

func assertSummaryExport(t *testing.T, data []byte, count int) {
	t.Helper()
	var result struct {
		Items []struct {
			ID  int64   `json:"id"`
			Key *string `json:"key"`
		} `json:"items"`
		Returned  int    `json:"returned_count"`
		Truncated bool   `json:"truncated"`
		Limit     *int64 `json:"limit"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	require.Equal(t, count, result.Returned)
	require.Len(t, result.Items, count)
	require.False(t, result.Truncated)
	require.Nil(t, result.Limit)
	for i, item := range result.Items {
		require.Equal(t, int64(i+1), item.ID)
		require.Nil(t, item.Key)
	}
}
