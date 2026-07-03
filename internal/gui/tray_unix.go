//go:build !windows && !darwin

package gui

import "github.com/godbus/dbus/v5"

// systemTrayAvailable reports whether a StatusNotifier host (system tray) is
// currently registered on the session bus. On Linux/BSD a tray only exists if
// something owns the watcher name — a panel, or a GNOME extension such as
// AppIndicator. When nothing does, registering a tray icon creates an orphan
// the app can never surface (and which makes the desktop's tray host log
// "ServiceUnknown: not activatable" once the app exits), so callers fall back
// to a plain window with close-to-quit instead of close-to-tray.
func systemTrayAvailable() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	for _, name := range []string{
		"org.kde.StatusNotifierWatcher",        // de-facto standard; GNOME AppIndicator provides it
		"org.freedesktop.StatusNotifierWatcher", // spec name
	} {
		var hasOwner bool
		call := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name)
		if call.Err == nil && call.Store(&hasOwner) == nil && hasOwner {
			return true
		}
	}
	return false
}
