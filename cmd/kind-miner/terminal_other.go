//go:build !linux && !darwin && !windows

package main

import "os"

// isTerminal falls back to the device type where there is no termios to ask:
// it may take /dev/null for a terminal, which only costs the tray mode a
// window launch should have had.
func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
