package terminal

import (
	"bufio"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Spec 0010 AC-12: reject malformed frames before method dispatch.
func TestRequestBoundary(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"id":"1","method":"hello","params":{},"extra":true}` + "\n",
		`{"version":1,"id":"1","id":"2","method":"hello","params":{}}` + "\n",
		`{"Version":1,"id":"1","method":"hello","params":{}}` + "\n",
		`{"version":1,"id":"01","method":"hello","params":{}}` + "\n",
		`{"version":1,"id":"1","method":"overview","params":null}` + "\n",
		`{"version":1,"id":"1","method":"hello","params":{}}`,
		strings.Repeat(" ", requestLimit) + "\n",
	} {
		_, err := readRequest(bufio.NewReaderSize(strings.NewReader(raw), requestLimit), new(big.Int))
		require.Error(t, err)
	}
	reader := bufio.NewReaderSize(
		strings.NewReader(
			`{"version":1,"id":"9007199254740993","method":"overview","params":{}}`+"\n"+
				`{"version":1,"id":"9007199254740993","method":"overview","params":{}}`+"\n",
		), requestLimit,
	)
	previous := new(big.Int)
	r, err := readRequest(reader, previous)
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", r.ID)
	_, err = readRequest(reader, previous)
	require.Error(t, err)
}

// Spec 0010 AC-12: an absent development commit is an empty string, never JSON null.
func TestHelloRequiresStringCommit(t *testing.T) {
	var params struct {
		Release  string `json:"release"`
		Commit   string `json:"commit"`
		Protocol int    `json:"protocol"`
	}
	require.NoError(t, strictJSON([]byte(`{"release":"dev","commit":"","protocol":1}`), &params))
	require.Error(t, strictJSON([]byte(`{"release":"dev","commit":null,"protocol":1}`), &params))
}
