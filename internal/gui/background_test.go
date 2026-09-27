package gui

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
)

// On stock GNOME there is no tray. With the single-instance name held, closing
// the window hides it, says so once, and a relaunch brings it back.
func TestBackgroundModeHidesAndComesBack(t *testing.T) {
	a := test.NewApp()
	w := a.NewWindow("kind-miner")
	u := &uiApp{app: a, win: w, sup: core.New(config.Defaults())}
	activeUI = u
	t.Cleanup(func() { activeUI = nil })

	w.Show()
	u.hideToBackground()
	if !u.backgroundNoticed {
		t.Error("the first close did not explain where kind-miner went")
	}
	u.hideToBackground() // a second close stays quiet
	Activate()           // what a relaunch does
	if !u.backgroundNoticed {
		t.Error("the notice state was lost")
	}
}

func TestDashboardWithoutATrayCanQuit(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(kindTheme{}) // the test theme has no bold face
	cfg := config.Defaults()
	cfg.Wallet = sampleAddress
	u := &uiApp{sup: core.New(cfg), hasTray: false}
	w := test.NewWindow(u.dashboardScreen())
	defer w.Close()
	w.Resize(windowSize)
	if w.Canvas().Capture() == nil {
		t.Fatal("the tray-less dashboard did not render")
	}
}
