package store_test

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/cli"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/store"
	"github.com/stretchr/testify/require"
	"modernc.org/sqlite"
)

type exportCloseDriver struct{ closes *atomic.Int32 }

func (d exportCloseDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return exportCloseConn{Conn: c, closes: d.closes}, nil
}

type exportCloseConn struct {
	driver.Conn
	closes *atomic.Int32
}

func (c exportCloseConn) Close() error {
	c.closes.Add(1)
	return errors.Join(c.Conn.Close(), errors.New("private close failure payload"))
}

// Spec 0006 AC-8: even successful preparation cannot be delivered after reader closure fails.
func TestExportReaderCloseFailureDiscardsPreparedOutput(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(fmt.Sprint("named=", named), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			seedAcknowledgementCandidates(t, dir)
			var closes atomic.Int32
			name := fmt.Sprintf("export_close_%d", acknowledgementDriverID.Add(1))
			sql.Register(name, exportCloseDriver{closes: &closes})
			restore := store.SwapDatabaseOpenerForTest(func(_ string, dsn string) (*sql.DB, error) {
				return sql.Open(name, dsn)
			})
			defer restore()
			parent := t.TempDir()
			t.Setenv("TMPDIR", parent)
			config := filepath.Join(t.TempDir(), "config.json")
			require.NoError(t, os.WriteFile(config, []byte("{}"), 0600))
			args := []string{"export", "--config", config, "--state-dir", dir}
			if named {
				args = append(args, "--output", filepath.Join(parent, "result.json"))
			}
			cmd := cli.NewRootCommand("test", "")
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(args)
			err := cmd.ExecuteContext(t.Context())
			require.Equal(t, "state_error", cli.Diagnostic(err))
			require.Equal(t, int32(1), closes.Load(), "the close fault must actually be reached")
			require.Empty(t, stdout.String())
			require.NotContains(t, stderr.String(), "private close failure payload")
			entries, err := os.ReadDir(parent)
			require.NoError(t, err)
			require.Empty(t, entries, "neither a destination nor staging files may survive")
		})
	}
}
