package main

import (
	"os"
	"runtime"
)

// uiMode is the presentation layer chosen for a launch.
type uiMode int

const (
	modeGUI      uiMode = iota // full graphical app: window + system tray
	modeTray                   // terminal launch: stdout startup logs + system tray
	modeHeadless               // no GUI at all: stdout logs, wait for a signal
)

func (m uiMode) String() string {
	switch m {
	case modeGUI:
		return "gui"
	case modeTray:
		return "tray"
	case modeHeadless:
		return "headless"
	}
	return "unknown"
}

// decideMode picks the presentation layer from the parsed flags and the
// environment. It is pure so the policy can be unit-tested.
//
// Rules, in order:
//   - forceHeadless (--no-tray/--headless) always wins.
//   - With no usable display we can't open a window or a tray, so fall back to
//     headless even if --gui was requested.
//   - forceGUI (--gui) then opens the full window.
//   - Otherwise a terminal (TTY) launch keeps the historical default of a
//     system tray, while a non-terminal launch (double-click, .desktop with
//     Terminal=false) gets the full GUI.
func decideMode(forceHeadless, forceGUI, isTTY, hasDisplay bool) uiMode {
	switch {
	case forceHeadless:
		return modeHeadless
	case !hasDisplay:
		return modeHeadless
	case forceGUI:
		return modeGUI
	case isTTY:
		return modeTray
	default:
		return modeGUI
	}
}

// displayAvailable reports whether a graphical session is reachable. On macOS
// and Windows a display is always assumed; on Linux/BSD it depends on an X11 or
// Wayland session being present in the environment.
func displayAvailable() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	default:
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	}
}
