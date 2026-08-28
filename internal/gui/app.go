package gui

import (
	"fmt"
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
	"github.com/kind-miner/kind-miner/internal/kindness"
	"github.com/kind-miner/kind-miner/internal/scheduler"
	"github.com/kind-miner/kind-miner/internal/stats"
)

const appID = "io.github.kind_miner.KindMiner"

// windowSize is the dashboard's default geometry. It is wide enough for four
// stat tiles and a chart with a legible time axis, which is what the layout
// needs to say what it is for at a glance.
var windowSize = fyne.NewSize(760, 560)

// setupSize is the geometry for the screens that come before the dashboard —
// setup, startup progress, and errors. They are single columns of prose and
// look wrong stretched to the dashboard's width.
var setupSize = fyne.NewSize(560, 460)

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

	// dash is the live dashboard, created when that screen is first shown.
	dash *dashboard

	// system tray
	trayMenu   *fyne.Menu
	mStatus    *fyne.MenuItem
	mHashrate  *fyne.MenuItem
	mUptime    *fyne.MenuItem
	mShares    *fyne.MenuItem
	mReward    *fyne.MenuItem
	mKindness  *fyne.MenuItem
	mKindItems []*fyne.MenuItem
	mToggle    *fyne.MenuItem
	mMineNow   *fyne.MenuItem

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
		u.setScreen(u.onboardingScreen(), setupSize)
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
	if u.hasTray {
		w.SetCloseIntercept(func() { w.Hide() })
	} else {
		w.SetCloseIntercept(func() { a.Quit() })
	}

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
}

// ---- screens ----

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
	if u.dash == nil {
		u.dash = u.newDashboard()
	}
	u.dash.refresh()
	return u.dash.object
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

func (u *uiApp) installTray() {
	if !u.hasTray {
		return // no StatusNotifier host — registering an icon would orphan it
	}
	desk, ok := u.app.(desktop.App)
	if !ok {
		return // not a desktop driver (shouldn't happen on supported platforms)
	}

	// Status lines rather than actions: they say what the miner is doing, what
	// it has done, and what it is working towards, without opening the window.
	u.mStatus = disabledItem(statusIdle)
	u.mHashrate = disabledItem("Hashrate " + emDash)
	u.mUptime = disabledItem("Uptime " + emDash)
	u.mShares = disabledItem("Shares " + emDash)
	u.mReward = disabledItem(rewardEstimating)

	u.mToggle = fyne.NewMenuItem("Pause mining", u.onToggle)
	u.mMineNow = fyne.NewMenuItem(labelMineNow, u.onMineNowToggle)
	u.mKindness = u.buildKindnessMenu()

	items := []*fyne.MenuItem{
		u.mStatus,
		u.mHashrate,
		u.mUptime,
	}
	// In pool mode there is no p2pool sidechain, so there are no shares to
	// count and nothing to estimate.
	if u.sup.Config() != nil && u.sup.Config().Mode != config.ModePool {
		items = append(items, u.mShares, u.mReward)
	} else {
		u.mShares, u.mReward = nil, nil
	}
	items = append(items,
		fyne.NewMenuItemSeparator(),
		u.mKindness,
		u.mMineNow,
		u.mToggle,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Open kind-miner", u.showWindow),
		fyne.NewMenuItem("Settings", u.onSettings),
	)
	u.trayMenu = fyne.NewMenu("kind-miner", items...)
	desk.SetSystemTrayMenu(u.trayMenu) // Fyne appends a Quit item automatically
	desk.SetSystemTrayIcon(resMining)
}

func disabledItem(label string) *fyne.MenuItem {
	item := fyne.NewMenuItem(label, nil)
	item.Disabled = true
	return item
}

// buildKindnessMenu makes the presets reachable without opening a window, which
// is where the design puts them: changing how much of the machine the miner may
// take should be as easy as noticing you want it back.
func (u *uiApp) buildKindnessMenu() *fyne.MenuItem {
	current := u.sup.Config().Kindness
	items := make([]*fyne.MenuItem, 0, len(kindness.Order))
	u.mKindItems = nil
	for _, preset := range kindness.All() {
		level := preset.Level
		item := fyne.NewMenuItem(
			fmt.Sprintf("%s — up to %d%% CPU", preset.Label, preset.CeilingPercent()),
			func() { u.setKindness(level) },
		)
		item.Checked = level == current
		items = append(items, item)
		u.mKindItems = append(u.mKindItems, item)
	}
	parent := fyne.NewMenuItem("Kindness", nil)
	parent.ChildMenu = fyne.NewMenu("", items...)
	return parent
}

// refreshTray updates the tray icon and the dynamic menu labels, re-applying the
// menu only when the rendered text changed. See lastMenuKey.
func (u *uiApp) refreshTray() {
	desk, ok := u.app.(desktop.App)
	if !ok || !u.hasTray {
		return
	}
	s := u.sup.Scheduler()
	if s == nil {
		return
	}
	state, reason := s.CurrentState()
	override := s.Override()
	preset := s.Preset()
	history := s.History()

	desk.SetSystemTrayIcon(trayIcon(state))

	status := trayStatusLine(s.IdleCountdown, override, state, reason)
	hashrate := emDash
	if mean, ok := stats.Mean(history, hashrateWindow, stats.Hashrate); ok {
		hashrate = formatHashrate(mean)
	}
	uptime := emDash
	if up, ok := u.sup.Uptime(); ok {
		uptime = formatUptime(up)
	}
	shares, reward := emDash, rewardEstimating
	if p := u.sup.P2Pool(); p != nil {
		if st, ok := p.Stats(); ok {
			shares = fmt.Sprintf("%d found", st.SharesFound)
			if occupancy, ok := st.WindowOccupancy(); ok {
				shares += fmt.Sprintf(" · %s of the payout window", formatPercent(occupancy))
			}
			reward = rewardLabel(st, true)
		}
	}
	toggleLabel := pauseLabel(override, state == scheduler.StatePaused)
	mineNowLabel := labelMineNow
	if override == scheduler.OverrideMine {
		mineNowLabel = labelMineAuto
	}

	key := status + "\x00" + hashrate + "\x00" + uptime + "\x00" + shares + "\x00" +
		reward + "\x00" + toggleLabel + "\x00" + mineNowLabel + "\x00" + string(preset.Level)
	if key == u.lastMenuKey {
		return
	}
	u.lastMenuKey = key

	setItem(u.mStatus, status)
	setItem(u.mHashrate, "Hashrate  "+hashrate)
	setItem(u.mUptime, "Uptime  "+uptime)
	setItem(u.mShares, "Shares  "+shares)
	setItem(u.mReward, reward)
	setItem(u.mToggle, toggleLabel)
	setItem(u.mMineNow, mineNowLabel)
	for i, item := range u.mKindItems {
		if i < len(kindness.Order) {
			item.Checked = kindness.Order[i] == preset.Level
		}
	}
	desk.SetSystemTrayMenu(u.trayMenu) // re-apply to reflect the new labels
}

func setItem(item *fyne.MenuItem, label string) {
	if item != nil {
		item.Label = label
	}
}

// ---- presentation helpers ----

// trayIcon maps a mining state to its tray icon. Only three are drawn: mining,
// stepping aside, and stopped — finer detail is unreadable at 22px.
func trayIcon(state scheduler.State) fyne.Resource {
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
