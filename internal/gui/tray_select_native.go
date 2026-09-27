//go:build windows || darwin

package gui

import "fyne.io/fyne/v2/driver/desktop"

// newTray uses Fyne's tray on macOS and Windows: there the tray hangs off the
// native event loop, which Fyne's driver owns, so systray cannot be run beside
// it.
func newTray(u *uiApp) trayRenderer {
	desk, ok := u.app.(desktop.App)
	if !ok {
		return nil
	}
	return newFyneTray(u, desk)
}
