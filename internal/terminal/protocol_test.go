package terminal

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Spec 0010 AC-12: reject malformed frames before method dispatch.
func TestRequestBoundary(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"id":"1","method":"overview","params":{}}` + "\n",
		`{"version":2,"id":"1","method":"hello","params":{},"extra":true}` + "\n",
		`{"version":2,"id":"1","id":"2","method":"hello","params":{}}` + "\n",
		`{"Version":2,"id":"1","method":"hello","params":{}}` + "\n",
		`{"version":2,"id":"01","method":"hello","params":{}}` + "\n",
		`{"version":2,"id":"1","method":"overview","params":null}` + "\n",
		`{"version":2,"id":"1","method":"hello","params":{}}`,
		strings.Repeat(" ", requestLimit) + "\n",
	} {
		_, err := readRequest(bufio.NewReaderSize(strings.NewReader(raw), requestLimit), new(big.Int))
		require.Error(t, err)
	}
	reader := bufio.NewReaderSize(
		strings.NewReader(
			`{"version":2,"id":"9007199254740993","method":"overview","params":{}}`+"\n"+
				`{"version":2,"id":"9007199254740993","method":"overview","params":{}}`+"\n",
		), requestLimit,
	)
	previous := new(big.Int)
	r, err := readRequest(reader, previous)
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", r.ID)
	_, err = readRequest(reader, previous)
	require.Error(t, err)
}

// Spec 0010 AC-12: the newline belongs to the frame budget and EOF is never a partial request.
func TestRequest_ExactFrameLimitAndSequence(t *testing.T) {
	base := `{"version":2,"id":"9007199254740993","method":"overview","params":{}}`
	for _, size := range []int{requestLimit, requestLimit + 1} {
		t.Run(big.NewInt(int64(size)).String(), func(t *testing.T) {
			raw := base + strings.Repeat(" ", size-len(base)-1) + "\n"
			previous := new(big.Int)
			reader := bufio.NewReaderSize(strings.NewReader(raw), requestLimit)
			got, err := readRequest(reader, previous)
			if size > requestLimit {
				require.EqualError(t, err, "ui_protocol_invalid")
				require.Zero(t, previous.Sign())
				return
			}
			require.NoError(t, err)
			require.Equal(t, "9007199254740993", got.ID)
			_, err = readRequest(reader, previous)
			require.ErrorIs(t, err, io.EOF)
		})
	}
	for _, id := range []string{"9007199254740994", "9007199254740992", "0", "-1", "1e3", strings.Repeat("9", 129)} {
		t.Run(id, func(t *testing.T) {
			previous, ok := new(big.Int).SetString("9007199254740993", 10)
			require.True(t, ok)
			raw := strings.Replace(base, "9007199254740993", id, 1) + "\n"
			_, err := readRequest(bufio.NewReaderSize(strings.NewReader(raw), requestLimit), previous)
			if id == "9007199254740994" {
				require.NoError(t, err)
				require.Equal(t, id, previous.String())
			} else {
				require.EqualError(t, err, "ui_protocol_invalid")
				require.Equal(t, "9007199254740993", previous.String())
			}
		})
	}
}

type replyWriter struct {
	calls int
	err   error
	short bool
}

func (w *replyWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.err != nil {
		return 0, w.err
	}
	if w.short {
		return len(p) - 1, nil
	}
	return len(p), nil
}

// Spec 0010 AC-12, AC-13: reply limits and failed writes expose only a safe code.
func TestReply_BoundsAndFailedWrites(t *testing.T) {
	var empty bytes.Buffer
	require.NoError(t, writeReply(&empty, "1", ""))
	payload := strings.Repeat("x", replyLimit-empty.Len())
	var output bytes.Buffer
	require.NoError(t, writeReply(&output, "1", payload))
	require.Equal(t, replyLimit, output.Len())
	require.Equal(t, byte('\n'), output.Bytes()[output.Len()-1])
	for _, tc := range []struct {
		name   string
		result any
		writer replyWriter
		calls  int
	}{
		{"too large", payload + "x", replyWriter{}, 0},
		{"cannot encode", make(chan int), replyWriter{}, 0},
		{"short write", "ok", replyWriter{short: true}, 1},
		{"private writer error", "ok", replyWriter{err: errors.New("private pipe details")}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.EqualError(t, writeReply(&tc.writer, "1", tc.result), "ui_protocol_invalid")
			require.Equal(t, tc.calls, tc.writer.calls)
		})
	}
}

// Spec 0010 AC-12: an absent development commit is an empty string, never JSON null.
func TestHelloRequiresStringCommit(t *testing.T) {
	var params struct {
		Release  string `json:"release"`
		Commit   string `json:"commit"`
		Protocol int    `json:"protocol"`
	}
	require.NoError(t, strictJSON([]byte(`{"release":"dev","commit":"","protocol":2}`), &params))
	require.Error(t, strictJSON([]byte(`{"release":"dev","commit":null,"protocol":2}`), &params))
}
