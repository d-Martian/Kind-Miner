package gui

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
)

// Closing a window makes Fyne 2.5.3 drop every never-shown window and quit if
// none is left — the hidden main window of a tray launch is one — so the
// settings window must only ever be hidden.
func TestSettingsWindowIsHiddenNeverClosed(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(kindTheme{})
	cfg := config.Defaults()
	cfg.Wallet = sampleAddress
	u := &uiApp{app: a, sup: core.New(cfg)}

	u.showSettings()
	first := u.settingsWin
	if first == nil || !u.settingsShown {
		t.Fatal("Settings did not open")
	}
	windows := len(a.Driver().AllWindows())

	// Save, Cancel and the window's ✕ (through its close intercept) all end
	// in hideSettings. The test driver cannot press the ✕ itself: Close from
	// code skips the intercept.
	u.hideSettings()
	if u.settingsShown {
		t.Error("Settings still counts as shown once put away")
	}
	if got := len(a.Driver().AllWindows()); got != windows {
		t.Errorf("once put away the app has %d windows, not %d: the window was closed", got, windows)
	}
	u.showSettings()
	if u.settingsWin != first {
		t.Error("reopening made a new window instead of reusing the hidden one")
	}
}
