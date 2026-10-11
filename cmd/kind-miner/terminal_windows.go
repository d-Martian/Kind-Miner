package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// isTerminal reports whether stdin is a console. NUL, like /dev/null, is a
// character device, so the device type alone cannot say.
func isTerminal() bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &mode) == nil
}
