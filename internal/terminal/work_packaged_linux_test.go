package terminal_test

import (
	"os"
	"testing"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
)

// Spec 0012 AC-1, AC-5, AC-10, AC-11 and AC-13: use real packaged Ink
// and a competing writer to prove reviewed admission without external work.
func verifyPackagedControls(t *testing.T, dir string, master *os.File, output *terminalOutput) {
	t.Helper()
	w, err := store.OpenWriter(t.Context(), dir, time.Now)
	require.NoError(t, err)
	defer func() { require.NoError(t, w.Close()) }()
	d := resultsDriver{t: t, master: master, output: output}
	d.send("c", "Select configured sources")
	d.send("\t", "> Common page limit")
	d.send("\t", "> Back")
	d.send("\t", "> Review")
	d.send("\r", "> Back |")
	d.send("\t", "> Start crawl")
	d.send("\r", "Outcome available")
	d.send("a", "Overview  |")
	d.send("\t", "> Recent work")
	d.send("\r", "Open run ID")
	d.send("\r", "Fixed snapshot")
	d.send("\x1b", "Overview  |")
	_, err = master.WriteString("q")
	require.NoError(t, err)
}

// A local CONNECT fixture holds retrieval without contacting Telegram. The real
// executable persists request intent, handles Ctrl+X and exposes the saved report.
func verifyPackagedCancellation(t *testing.T, master *os.File, output *terminalOutput) {
	t.Helper()
	d := resultsDriver{t: t, master: master, output: output}
	d.send("c", "Select configured sources")
	d.send("\t", "> Common page limit")
	d.send("\t", "> Back")
	d.send("\t", "> Review")
	d.send("\r", "> Back |")
	d.send("\t", "> Start crawl")
	d.send("\r", "request intents")
	d.send(string([]byte{24}), "cancelled")
	d.send("r", "Fixed snapshot")
	d.send("\t", "Section sources")
	d.send("\r", "Section pages")
	_, err := master.WriteString("q")
	require.NoError(t, err)
}
