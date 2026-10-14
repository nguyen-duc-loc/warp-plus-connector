package terminal

import (
	"golang.org/x/sys/unix"
	"os"
	"sync"
)

func capture(in, out *os.File) (func(), error) {
	settings, err := unix.IoctlGetTermios(int(in.Fd()), unix.TCGETS)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(
			func() {
				_ = unix.IoctlSetTermios(int(in.Fd()), unix.TCSETS, settings)
				_, _ = out.WriteString("\x1b[?2026l\x1b[?2004l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?25h\x1b[?1049l")
			},
		)
	}, nil
}
