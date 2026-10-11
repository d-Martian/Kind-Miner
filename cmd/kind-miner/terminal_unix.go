//go:build linux || darwin

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// isTerminal reports whether stdin is a terminal, by asking the kernel for its
// terminal settings. Checking for a character device, as this once did, also
// says yes for /dev/null — and /dev/null is the stdin a desktop launcher,
// autostart and Flatpak all give an app, so every launch from the menu was
// taken for a terminal and ran in tray mode, with the text wizard on a first
// run.
func isTerminal() bool {
	return isTerminalFD(int(os.Stdin.Fd()))
}

func isTerminalFD(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	return err == nil
}
