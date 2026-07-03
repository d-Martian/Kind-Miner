//go:build windows || darwin

package gui

// systemTrayAvailable reports whether a system tray / menu-bar host exists.
// macOS (menu bar) and Windows (notification area) always have one.
func systemTrayAvailable() bool { return true }
