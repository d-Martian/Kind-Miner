package gui

import (
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/scheduler"
)

const appID = "io.github.kind_miner.KindMiner"

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

	// dashboard widgets (created when the dashboard screen is shown)
	status  *canvas.Text
	detail  *canvas.Text
	reward  *canvas.Text
	toggle  *widget.Button
	mineNow *widget.Button

	// system tray
	trayMenu   *fyne.Menu
	mToggle    *fyne.MenuItem
	mMineNow   *fyne.MenuItem
	mCountdown *fyne.MenuItem
	mReward    *fyne.MenuItem

	refreshStop chan struct{}
	stopOnce    sync.Once

	// lastMenuKey holds the rendered text of every dynamic menu item. Fyne 2.5.3
	// cannot refresh a single item, so changing a label means re-applying the
	// whole menu — which closes it if the user has it open. Re-applying only when
	// the rendered text actually changed keeps that rare.
	lastMenuKey string
}

// RunGraphical owns the full GUI lifecycle: optional first-run onboarding, an
// asynchronous startup with a progress screen, then the live dashboard. It
// blocks until the user quits. The caller owns the supervisor and must call
// Shutdown after this returns.
func RunGraphical(sup *core.Supervisor, firstRun bool) {
	u := newUIApp(sup)
	u.installTray()

	if firstRun {
		u.win.SetContent(u.onboardingScreen())
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
	u.win.SetContent(u.dashboardScreen())
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
	w.Resize(fyne.NewSize(360, 320))
	w.CenterOnScreen()

	u := &uiApp{app: a, win: w, sup: sup, refreshStop: make(chan struct{}), hasTray: systemTrayAvailable()}

	// Close behaviour depends on whether a system tray exists. With one, closing
	// tucks kind-miner away and it keeps mining. Without one (e.g. stock GNOME
	// with no AppIndicator extension), hiding would strand the app with no way
	// back — and orphan its StatusNotifierItem — so closing quits cleanly.
	if u.hasTray {
		w.SetCloseIntercept(func() { w.Hide() })
	} else {
		w.SetCloseIntercept(func() { a.Quit() })
	}

	return u
}

// startStartup swaps to the progress screen and runs the supervisor bring-up
// on a background goroutine.
func (u *uiApp) startStartup() {
	u.win.SetContent(u.progressScreen(core.StepInstallXMRig))
	go u.runStartup()
}

func (u *uiApp) runStartup() {
	err := u.sup.Start(func(st core.Step) {
		u.win.SetContent(u.progressScreen(st))
	})
	if err != nil {
		u.win.SetContent(u.errorScreen(err))
		u.showWindow()
		return
	}
	u.win.SetContent(u.dashboardScreen())
	u.startRefresh()
}

// ---- screens ----

// onboardingScreen starts the first-run flow: a welcome screen that hands off
// to wallet entry.
func (u *uiApp) onboardingScreen() fyne.CanvasObject {
	return welcomeScreen(func() {
		u.win.SetContent(u.walletEntryScreen())
	})
}

// walletEntryScreen collects the wallet address; on submit it saves the config
// and proceeds to startup.
func (u *uiApp) walletEntryScreen() fyne.CanvasObject {
	return walletScreen(u.win, func(addr string) {
		u.sup.Config().Wallet = addr
		if err := u.sup.Config().Save(); err != nil {
			u.win.SetContent(u.errorScreen(fmt.Errorf("saving config: %w", err)))
			return
		}
		u.startStartup()
	})
}

func (u *uiApp) progressScreen(step core.Step) fyne.CanvasObject {
	title := canvas.NewText(SetupTitle, colorForeground)
	title.TextSize = 20
	title.TextStyle = fyne.TextStyle{Bold: true}

	line := canvas.NewText(step.String()+"…", colorAccent)
	line.TextSize = 15

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
	u.status = canvas.NewText("Starting…", colorForeground)
	u.status.TextSize = 24
	u.status.TextStyle = fyne.TextStyle{Bold: true}

	u.detail = canvas.NewText("", colorMuted)
	u.detail.TextSize = 14

	u.reward = canvas.NewText("", colorMuted)
	u.reward.TextSize = 12

	u.toggle = widget.NewButton("Pause", u.onToggle)
	u.mineNow = widget.NewButton("Mine now", u.onMineNowToggle)
	settings := widget.NewButton("Settings", u.onSettings)
	quit := widget.NewButton("Quit", u.confirmQuit)

	buttons := container.NewHBox(u.toggle, u.mineNow, settings, widget.NewLabel(""), quit)
	body := container.NewVBox(u.status, u.detail, u.reward)

	u.updateDashboard()
	return container.NewBorder(nil, buttons, nil, nil, container.NewPadded(body))
}

func (u *uiApp) errorScreen(err error) fyne.CanvasObject {
	title := canvas.NewText("Something went wrong", colorError)
	title.TextSize = 20
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

// onSettings opens the in-app settings window.
func (u *uiApp) onSettings() {
	u.showSettings()
}

// confirmQuit guards against accidentally stopping mining. When a tray exists
// the window can simply be closed to keep mining in the background, so quitting
// — which stops mining — asks first. With no tray (quitting is the only exit)
// or before mining has started, it quits immediately.
func (u *uiApp) confirmQuit() {
	if !u.hasTray || u.sup.Scheduler() == nil {
		u.app.Quit()
		return
	}
	dialog.ShowConfirm("Quit kind-miner?",
		"This stops mining. To keep mining in the background, close this window instead.",
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

// updateDashboard reads the current mining state and refreshes the window
// labels, the toggle button, and the tray icon. Safe to call before startup
// completes (it no-ops while the scheduler is nil).
func (u *uiApp) updateDashboard() {
	s := u.sup.Scheduler()
	x := u.sup.XMRig()
	if s == nil || x == nil {
		return
	}
	state, reason := s.CurrentState()
	override := s.Override()
	paused := override == scheduler.OverridePause
	mineNow := override == scheduler.OverrideMine
	icon, statusText, detailText := visuals(state, reason, x.Hashrate())

	countdown := trayStatusLine(s.IdleCountdown, override, state, reason)
	// While the only thing holding mining back is the wait for idle, the detail
	// line should say how long is left rather than repeat the reason.
	if reason == scheduler.ReasonWaitingForIdle {
		detailText = countdown
	}

	if u.status != nil {
		u.status.Text = statusText
		u.status.Color = statusColor(state)
		u.status.Refresh()
		u.detail.Text = detailText
		u.detail.Refresh()
	}
	if u.reward != nil {
		u.reward.Text = ""
		if p := u.sup.P2Pool(); p != nil {
			if stats, ok := p.Stats(); ok {
				u.reward.Text = rewardDetail(stats)
			}
		}
		u.reward.Refresh()
	}
	if u.toggle != nil {
		if paused {
			u.toggle.SetText("Resume")
		} else {
			u.toggle.SetText("Pause")
		}
	}
	if u.mineNow != nil {
		if mineNow {
			u.mineNow.SetText("Wait for idle")
		} else {
			u.mineNow.SetText("Mine now")
		}
	}

	reward := rewardEstimating
	if p := u.sup.P2Pool(); p != nil {
		reward = rewardLabel(p.Stats())
	}
	u.refreshTray(icon, paused, mineNow, countdown, reward)
}

// ---- system tray ----

func (u *uiApp) installTray() {
	if !u.hasTray {
		return // no StatusNotifier host — registering an icon would orphan it
	}
	desk, ok := u.app.(desktop.App)
	if !ok {
		return // not a desktop driver (shouldn't happen on supported platforms)
	}
	u.mToggle = fyne.NewMenuItem("Pause", u.onToggle)
	u.mMineNow = fyne.NewMenuItem(labelMineNow, u.onMineNowToggle)
	// Status lines rather than actions: they show why mining is holding back and
	// what it is working towards.
	u.mCountdown = fyne.NewMenuItem(statusIdle, nil)
	u.mCountdown.Disabled = true
	u.mReward = fyne.NewMenuItem(rewardEstimating, nil)
	u.mReward.Disabled = true

	items := []*fyne.MenuItem{u.mCountdown}
	// In pool mode there is no p2pool sidechain, so there is nothing to estimate.
	if u.sup.Config() != nil && u.sup.Config().Mode != config.ModePool {
		items = append(items, u.mReward)
	} else {
		u.mReward = nil
	}
	items = append(items,
		fyne.NewMenuItem("Open kind-miner", u.showWindow),
		u.mMineNow,
		u.mToggle,
		fyne.NewMenuItem("Settings", u.onSettings),
	)
	u.trayMenu = fyne.NewMenu("kind-miner", items...)
	desk.SetSystemTrayMenu(u.trayMenu) // Fyne appends a Quit item automatically
	desk.SetSystemTrayIcon(resMining)
}

// refreshTray updates the tray icon and the dynamic menu labels, re-applying the
// menu only when the rendered text changed. See lastMenuKey.
func (u *uiApp) refreshTray(icon fyne.Resource, paused, mineNow bool, countdown, reward string) {
	desk, ok := u.app.(desktop.App)
	if !ok {
		return
	}
	desk.SetSystemTrayIcon(icon)

	toggleLabel := "Pause"
	if paused {
		toggleLabel = "Resume"
	}
	mineNowLabel := labelMineNow
	if mineNow {
		mineNowLabel = labelMineAuto
	}
	key := toggleLabel + "\x00" + mineNowLabel + "\x00" + countdown + "\x00" + reward
	if key == u.lastMenuKey {
		return
	}
	u.lastMenuKey = key

	if u.mToggle != nil {
		u.mToggle.Label = toggleLabel
	}
	if u.mMineNow != nil {
		u.mMineNow.Label = mineNowLabel
	}
	if u.mCountdown != nil {
		u.mCountdown.Label = countdown
	}
	if u.mReward != nil {
		u.mReward.Label = reward
	}
	desk.SetSystemTrayMenu(u.trayMenu) // re-apply to reflect the new labels
}

// ---- presentation helpers (ported from the old systray Tray) ----

// visuals maps a mining state to its tray icon, headline, and detail line.
func visuals(state scheduler.State, reason string, hr float64) (icon fyne.Resource, status, detail string) {
	switch state {
	case scheduler.StateFull:
		return resMining, "Mining", formatHashrate(hr)
	case scheduler.StateReduced:
		return resThrottle, "Throttled", formatHashrate(hr)
	case scheduler.StateMinimal:
		return resThrottle, "Minimal", formatHashrate(hr)
	case scheduler.StatePaused:
		if reason != "" {
			return resPaused, "Paused", reason
		}
		return resPaused, "Paused", ""
	}
	return resMining, "—", ""
}

func statusColor(s scheduler.State) color.Color {
	switch s {
	case scheduler.StateFull:
		return colorOK
	case scheduler.StateReduced, scheduler.StateMinimal:
		return colorAccent
	default:
		return colorMuted
	}
}

// formatHashrate formats a H/s value into a human-readable string.
func formatHashrate(hs float64) string {
	switch {
	case hs >= 1_000_000:
		return fmt.Sprintf("%.2f MH/s", hs/1_000_000)
	case hs >= 1_000:
		return fmt.Sprintf("%.2f kH/s", hs/1_000)
	case hs > 0:
		return fmt.Sprintf("%.2f H/s", hs)
	default:
		return "— H/s"
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
