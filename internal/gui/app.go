package gui

import (
	"fmt"
	"log"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/kind-miner/kind-miner/internal/autostart"
	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/instance"
	"github.com/kind-miner/kind-miner/internal/kindness"
	"github.com/kind-miner/kind-miner/internal/scheduler"
)

const appID = instance.AppID

// AppID is the app's reverse-DNS identity: the desktop file, the Flatpak ID,
// and the single-instance bus name.
const AppID = appID

// windowSize is the dashboard's default geometry. It is wide enough for four
// stat tiles and a chart with a legible time axis, which is what the layout
// needs to say what it is for at a glance.
var windowSize = fyne.NewSize(760, 560)

// setupSize is the geometry for the screens that come before the dashboard —
// setup, startup progress, and errors. They are single columns of prose and
// look wrong stretched to the dashboard's width.
var setupSize = fyne.NewSize(560, 460)

// onboardingSize is taller than setupSize so the startup disclosure and
// "More options" sit on screen under the address field without scrolling.
var onboardingSize = fyne.NewSize(560, 600)

// currentApp holds the running Fyne app so Quit can be invoked from a signal
// handler in package main. Only one app exists per process.
var currentApp fyne.App

// Quit terminates the running GUI from any goroutine (e.g. an OS signal
// handler). It is a no-op if no GUI is running.
func Quit() {
	if currentApp != nil {
		currentApp.Quit()
	}
}

// uiApp is the Fyne front-end: one application, one window, and one system
// tray, all bound to a single core.Supervisor and driven from one event loop.
//
// Threading note: Fyne 2.5.3 has no fyne.Do helper, so background goroutines
// (startup, the refresh ticker) mutate widgets and call Refresh directly —
// the accepted pattern on this version. A future bump to Fyne 2.6 should route
// these through fyne.Do.
type uiApp struct {
	app fyne.App
	win fyne.Window
	sup *core.Supervisor

	// settingsWin holds the open settings window so repeat invocations focus the
	// existing one instead of spawning duplicates. nil when no window is open.
	settingsWin fyne.Window

	// hasTray records whether a system-tray / menu-bar host exists. It drives
	// close-to-tray vs close-to-quit, and whether a tray icon is registered.
	hasTray bool
	// tuckAfterStart is set by first-run setup: once mining starts, the window
	// hides into the tray and a notification says where it went.
	tuckAfterStart bool
	// backgroundNoticed records that the "still mining" notice has been shown,
	// so closing the window again stays quiet.
	backgroundNoticed bool

	// dash is the live dashboard, created when that screen is first shown.
	dash *dashboard

	// system tray
	// tray renders the system tray; nil without one. See tray.go.
	tray trayRenderer

	refreshStop chan struct{}
	stopOnce    sync.Once
}

// RunGraphical owns the full GUI lifecycle: optional first-run onboarding, an
// asynchronous startup with a progress screen, then the live dashboard. It
// blocks until the user quits. The caller owns the supervisor and must call
// Shutdown after this returns.
func RunGraphical(sup *core.Supervisor, firstRun bool) {
	u := newUIApp(sup)
	u.installTray()

	if firstRun {
		u.setScreen(u.onboardingScreen(), onboardingSize)
	} else {
		u.startStartup()
	}
	u.win.ShowAndRun()
	u.stopRefresh()
}

// RunTray presents an already-started supervisor as a system tray with an
// on-demand window, matching the historical terminal-launch behaviour. It
// blocks until the user quits; the caller owns Shutdown.
func RunTray(sup *core.Supervisor) {
	u := newUIApp(sup)
	u.setScreen(u.dashboardScreen(), windowSize)
	u.installTray()
	if !u.hasTray {
		// No tray to live in — keep a window on screen so there's a way back.
		u.win.Show()
	}
	u.startRefresh()
	// Terminal launch: live in the tray; the window opens from the tray menu.
	u.app.Run()
	u.stopRefresh()
}

func newUIApp(sup *core.Supervisor) *uiApp {
	a := fyneapp.NewWithID(appID)
	a.Settings().SetTheme(kindTheme{})
	currentApp = a

	w := a.NewWindow("kind-miner")
	w.Resize(windowSize)
	w.CenterOnScreen()

	u := &uiApp{app: a, win: w, sup: sup, refreshStop: make(chan struct{}), hasTray: systemTrayAvailable()}

	// Close behaviour depends on whether a system tray exists. With one, closing
	// tucks kind-miner away and it keeps mining. Without one (e.g. stock GNOME
	// with no AppIndicator extension), hiding would strand the app with no way
	// back — and orphan its StatusNotifierItem — so closing quits cleanly.
	switch {
	case u.hasTray:
		w.SetCloseIntercept(func() { w.Hide() })
	case background:
		// No tray, but launching kind-miner again brings this window back
		// (see EnableBackground), so closing it need not stop mining.
		w.SetCloseIntercept(u.hideToBackground)
	default:
		w.SetCloseIntercept(func() { a.Quit() })
	}
	activeUI = u

	return u
}

// setScreen swaps the window content and asserts the geometry that screen was
// designed for.
//
// The resize is not redundant. Fyne sizes a window to its content's minimum on
// SetContent, so the narrow setup screens shrink the window — and it stays
// shrunk when the far wider dashboard replaces them, which squeezes the chart
// into a column. Each screen therefore states its own size as it appears.
func (u *uiApp) setScreen(content fyne.CanvasObject, size fyne.Size) {
	u.win.SetContent(content)
	u.win.Resize(size)
}

// startStartup swaps to the progress screen and runs the supervisor bring-up
// on a background goroutine.
func (u *uiApp) startStartup() {
	u.setScreen(u.progressScreen(core.StepInstallXMRig), setupSize)
	go u.runStartup()
}

func (u *uiApp) runStartup() {
	err := u.sup.Start(func(st core.Step) {
		// Only the text changes between steps, so the window is left alone —
		// re-resizing on every step would undo a resize the user just made.
		u.win.SetContent(u.progressScreen(st))
	})
	if err != nil {
		u.setScreen(u.errorScreen(err), setupSize)
		u.showWindow()
		return
	}
	u.setScreen(u.dashboardScreen(), windowSize)
	u.startRefresh()
	u.tuckIntoTray()
}

// tuckIntoTray ends first-run setup the way the rest of kind-miner's life goes:
// out of sight in the tray. It waits for mining to have started rather than
// hiding the moment Start is pressed, because the first start downloads and
// syncs for a while, the progress screen is what explains that, and a failure
// needs a window to say so in. Without a tray there is nowhere to go, so the
// window stays.
func (u *uiApp) tuckIntoTray() {
	if !u.tuckAfterStart || !u.hasTray {
		return
	}
	u.tuckAfterStart = false
	u.win.Hide()
	u.app.SendNotification(fyne.NewNotification(TrayNoticeTitle, TrayNoticeBody))
}

// ---- screens ----

func (u *uiApp) progressScreen(step core.Step) fyne.CanvasObject {
	title := canvas.NewText(SetupTitle, colorForeground)
	title.TextSize = textSize(20)
	title.TextStyle = fyne.TextStyle{Bold: true}

	line := canvas.NewText(step.String()+"…", colorAccent)
	line.TextSize = textSize(15)

	// The step blurbs are full paragraphs, so they must wrap: a non-wrapping
	// canvas.Text would size this screen to the blurb's single-line width
	// (~1600px) and Fyne never shrinks the window back, leaving every later
	// screen stretched. RichText wraps within the window while keeping the
	// muted styling (ColorNamePlaceHolder maps to colorMuted in kindTheme).
	blurb := widget.NewRichText(&widget.TextSegment{
		Text: stepBlurb(step),
		Style: widget.RichTextStyle{
			ColorName: theme.ColorNamePlaceHolder,
			SizeName:  theme.SizeNameCaptionText,
		},
	})
	blurb.Wrapping = fyne.TextWrapWord

	bar := widget.NewProgressBarInfinite()

	body := container.NewVBox(title, widget.NewLabel(""), line, bar, widget.NewLabel(""), blurb)
	return container.NewPadded(body)
}

func (u *uiApp) dashboardScreen() fyne.CanvasObject {
	if u.dash == nil {
		u.dash = u.newDashboard()
	}
	u.dash.refresh()
	return u.dash.object
}

func (u *uiApp) errorScreen(err error) fyne.CanvasObject {
	title := canvas.NewText("Something went wrong", colorError)
	title.TextSize = textSize(20)
	title.TextStyle = fyne.TextStyle{Bold: true}

	msg := widget.NewLabel(err.Error())
	msg.Wrapping = fyne.TextWrapWord

	retry := widget.NewButton("Retry", func() { u.startStartup() })
	openCfg := widget.NewButton("Open config", func() { openInEditor(config.Path()) })
	quit := widget.NewButton("Quit", func() { u.app.Quit() })

	buttons := container.NewHBox(retry, openCfg, widget.NewLabel(""), quit)
	body := container.NewVBox(title, msg)
	return container.NewBorder(nil, buttons, nil, nil, container.NewPadded(body))
}

// ---- actions ----

func (u *uiApp) onToggle() {
	s := u.sup.Scheduler()
	if s == nil {
		return
	}
	if s.IsManuallyPaused() {
		s.SetOverride(scheduler.OverrideNone)
	} else {
		s.SetOverride(scheduler.OverridePause)
	}
	u.updateDashboard()
}

// onMineNowToggle switches between mining on request and waiting for idle.
//
// Turning it on only skips the wait for the user to step away; the CPU, battery,
// and temperature backoff all stay in force, so mining still gets out of the way
// of whatever else the machine is doing.
func (u *uiApp) onMineNowToggle() {
	s := u.sup.Scheduler()
	if s == nil {
		return
	}
	if s.Override() == scheduler.OverrideMine {
		s.SetOverride(scheduler.OverrideNone)
	} else {
		s.SetOverride(scheduler.OverrideMine)
	}
	u.updateDashboard()
}

// setKindness changes preset from anywhere in the UI. It takes effect on the
// running scheduler at once and is written to disk, because a preset that
// silently reverted on restart would be worse than no control at all.
func (u *uiApp) setKindness(level kindness.Level) {
	cfg := u.sup.Config()
	if cfg.Kindness == level {
		return
	}
	cfg.Kindness = level
	if s := u.sup.Scheduler(); s != nil {
		s.SetKindness(level)
	}
	if err := cfg.Save(); err != nil {
		dialog.ShowError(fmt.Errorf("saving kindness: %w", err), u.win)
	}
	u.updateDashboard()
}

// onSettings opens the in-app settings window.
func (u *uiApp) onSettings() {
	u.showSettings()
}

// confirmQuit guards against accidentally stopping mining. When a tray exists
// the window can simply be closed to keep mining in the background, so quitting
// — which stops mining — asks first. With no tray (quitting is the only exit)
// or before mining has started, it quits immediately.
func (u *uiApp) confirmQuit() {
	if (!u.hasTray && !background) || u.sup.Scheduler() == nil {
		u.app.Quit()
		return
	}
	dialog.ShowConfirm("Quit kind-miner?",
		quitWarning,
		func(ok bool) {
			if ok {
				u.app.Quit()
			}
		}, u.win)
}

func (u *uiApp) showWindow() {
	u.win.Show()
	u.win.RequestFocus()
}

// ---- live refresh ----

func (u *uiApp) startRefresh() {
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				u.updateDashboard()
			case <-u.refreshStop:
				return
			}
		}
	}()
}

func (u *uiApp) stopRefresh() {
	u.stopOnce.Do(func() { close(u.refreshStop) })
}

// updateDashboard refreshes the window and the tray from current state. Safe to
// call before startup completes (it no-ops while the scheduler is nil).
func (u *uiApp) updateDashboard() {
	s := u.sup.Scheduler()
	x := u.sup.XMRig()
	if s == nil || x == nil {
		return
	}
	if u.dash != nil {
		u.dash.refresh()
	}
	u.refreshTray()
}

// ---- system tray ----

func disabledItem(label string) *fyne.MenuItem {
	item := fyne.NewMenuItem(label, nil)
	item.Disabled = true
	return item
}

func setItem(item *fyne.MenuItem, label string) {
	if item != nil {
		item.Label = label
	}
}

// ---- presentation helpers ----

// trayIcon maps a mining state to its tray icon: mining, stepping aside,
// stopped — finer detail is unreadable at 22px — and held, the one stop that
// may need the user, which gets the amber badge.
func trayIcon(state scheduler.State, held bool) fyne.Resource {
	if held {
		return resWarning
	}
	switch state {
	case scheduler.StateFull:
		return resMining
	case scheduler.StateReduced, scheduler.StateMinimal:
		return resThrottle
	default:
		return resPaused
	}
}

// stepBlurb returns the friendly explanation shown under each startup step.
func stepBlurb(step core.Step) string {
	switch step {
	case core.StepInstallXMRig, core.StepStartXMRig:
		return XMRigBlurb
	case core.StepInstallP2Pool, core.StepStartP2Pool:
		return P2PoolBlurb
	case core.StepStartMonerod, core.StepStartTor, core.StepSelectNode:
		return NodeBlurb
	case core.StepReady:
		return MinerBlurb
	}
	return ""
}

// Background mode: stock GNOME has no system tray, so there is nowhere for a
// hidden window to be reopened from — except the app launcher. Once the
// process holds the single-instance name, launching kind-miner again reaches
// this one and shows its window (see internal/instance), which makes closing
// the window safe: it hides, and mining carries on.

// background is set by EnableBackground; activeUI is the window a relaunch
// brings forward.
var (
	background bool
	activeUI   *uiApp
)

// EnableBackground tells the GUI that relaunching reaches this process, so a
// window closed on a desktop without a tray can hide instead of quitting. Call
// it before RunGraphical.
func EnableBackground() { background = true }

// Activate shows the running window. The single-instance listener calls it
// when kind-miner is launched a second time.
func Activate() {
	if u := activeUI; u != nil {
		u.showWindow()
	}
}

const (
	backgroundNoticeTitle = "kind-miner is still mining"
	backgroundNoticeBody  = "Its window is closed, but mining carries on. Open kind-miner again from your apps to see it, or to quit."
	quitWarning           = "This stops mining. To keep mining in the background, close this window instead."
)

// hideToBackground closes the window without stopping mining. The first time,
// it says where kind-miner went — a window that vanishes with no tray icon to
// show for it otherwise reads as a crash — and, inside a Flatpak, asks the
// portal for permission to keep running with no window.
func (u *uiApp) hideToBackground() {
	u.win.Hide()
	if u.backgroundNoticed {
		return
	}
	u.backgroundNoticed = true
	u.app.SendNotification(fyne.NewNotification(backgroundNoticeTitle, backgroundNoticeBody))
	cfg := u.sup.Config()
	go func() {
		if err := autostart.RequestBackground(autostartOptions(), cfg.RunAtStartup); err != nil {
			log.Printf("background permission: %v", err)
		}
	}()
}

// installTray starts the tray this platform renders best (see newTray).
func (u *uiApp) installTray() {
	if !u.hasTray {
		return // no StatusNotifier host — registering an icon would orphan it
	}
	u.tray = newTray(u)
}

// refreshTray puts the current state on the tray.
func (u *uiApp) refreshTray() {
	if u.tray == nil {
		return
	}
	if v, ok := u.computeTrayView(time.Now()); ok {
		u.tray.render(v)
	}
}
