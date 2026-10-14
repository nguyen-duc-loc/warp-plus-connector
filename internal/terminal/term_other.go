//go:build !linux

package terminal

import "os"

func capture(_, _ *os.File) (func(), error) { return nil, failure("ui_startup_failed") }
