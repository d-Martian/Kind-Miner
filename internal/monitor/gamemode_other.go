//go:build !linux && !freebsd && !openbsd && !netbsd

package monitor

// GameMode is a Linux (and BSD) daemon; elsewhere there is nothing to ask.
func newGameModeBackend() gameModeBackend { return nil }
