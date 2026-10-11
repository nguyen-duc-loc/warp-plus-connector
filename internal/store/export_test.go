package store

import "database/sql"

// SwapDatabaseOpenerForTest installs a driver fault boundary for external package tests.
// Callers serialize installation and wait for every handle to close before restoring it.
func SwapDatabaseOpenerForTest(open func(string, string) (*sql.DB, error)) func() {
	previous := openDatabase
	openDatabase = open
	return func() { openDatabase = previous }
}
