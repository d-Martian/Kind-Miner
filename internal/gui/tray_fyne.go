package gui

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/kind-miner/kind-miner/internal/kindness"
)

// fyneTray renders through Fyne's own tray, used where kind-miner cannot run
// systray itself (macOS and Windows, whose trays hang off the native event
// loop Fyne owns). Fyne cannot change one item: every change re-applies the
// whole menu — rebuilt item by item, and closed if it was open — and that is
// still true of Fyne 2.8. So the numbers are rate-limited and the controls
// are not; see shouldReapplyMenu.
type fyneTray struct {
	u    *uiApp
	desk desktop.App
	menu *fyne.Menu

	status, hashrate, income, uptime, shares, reward *fyne.MenuItem
	payouts, pause, mineNow                          *fyne.MenuItem
	kindItems                                        []*fyne.MenuItem

	lastKey  trayMenuKey
	lastAt   time.Time
	lastIcon fyne.Resource
}

func newFyneTray(u *uiApp, desk desktop.App) *fyneTray {
	t := &fyneTray{u: u, desk: desk}
	t.status = disabledItem(statusIdle)
	t.hashrate = disabledItem(formatRates(rates{}))
	t.income = disabledItem("≈ " + emDash + " XMR/month")
	t.uptime = disabledItem(uptimePrefix + emDash)
	t.shares = disabledItem(sharesPrefix + emDash)
	t.reward = disabledItem(rewardEstimating)
	t.payouts = fyne.NewMenuItem(payoutsMenuLabel, nil)
	t.payouts.ChildMenu = payoutsMenu(payoutLines(nil, time.Now()))
	t.pause = fyne.NewMenuItem(labelPauseMenu, nil)
	t.pause.ChildMenu = t.snoozeMenu()
	t.mineNow = fyne.NewMenuItem(labelMineNow, u.onMineNowToggle)

	current := u.sup.Config().Kindness
	var kinds []*fyne.MenuItem
	for _, preset := range kindness.All() {
		level := preset.Level
		item := fyne.NewMenuItem(kindnessItemLabel(preset), func() { u.setKindness(level) })
		item.Checked = level == current
		kinds = append(kinds, item)
	}
	t.kindItems = kinds
	kind := fyne.NewMenuItem("Kindness", nil)
	kind.ChildMenu = fyne.NewMenu("", kinds...)

	t.menu = fyne.NewMenu("kind-miner",
		t.status, t.hashrate, t.income, t.uptime, t.shares, t.reward, t.payouts,
		fyne.NewMenuItemSeparator(),
		kind, t.mineNow, t.pause,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Open kind-miner", u.showWindow),
		fyne.NewMenuItem("Settings", u.onSettings),
	)
	desk.SetSystemTrayMenu(t.menu) // Fyne appends a Quit item automatically
	desk.SetSystemTrayIcon(resMining)
	t.lastIcon, t.lastAt = resMining, time.Now()
	return t
}

func (t *fyneTray) snoozeMenu() *fyne.Menu {
	var items []*fyne.MenuItem
	for _, s := range t.u.snoozes() {
		until := s.until
		items = append(items, fyne.NewMenuItem(s.label, func() { t.u.snoozeUntil(until) }))
	}
	return fyne.NewMenu("", items...)
}

func (t *fyneTray) render(v trayView) {
	// Setting the icon re-encodes it and hands it to the main thread, so an
	// unchanged icon is not re-sent.
	if v.icon != t.lastIcon {
		t.lastIcon = v.icon
		t.desk.SetSystemTrayIcon(v.icon)
	}
	toggle := v.pauseLabel
	if v.paused {
		toggle = labelResume
	}
	key := trayMenuKey{
		controls: v.status + "\x00" + toggle + "\x00" + v.mineNowLabel + "\x00" + string(v.kindness),
		stats: v.hashrate + "\x00" + v.income + "\x00" + v.uptime + "\x00" + v.shares + "\x00" + v.reward +
			"\x00" + strings.Join(v.payouts, "\x00"),
	}
	now := time.Now()
	if !shouldReapplyMenu(key, t.lastKey, now.Sub(t.lastAt)) {
		return
	}
	t.lastKey, t.lastAt = key, now

	setItem(t.status, v.status)
	setItem(t.hashrate, v.hashrate)
	setItem(t.income, v.income)
	setItem(t.uptime, uptimePrefix+v.uptime)
	setItem(t.shares, sharesPrefix+v.shares)
	setItem(t.reward, v.reward)
	t.payouts.ChildMenu = payoutsMenu(v.payouts)
	t.setToggle(v)
	setItem(t.mineNow, v.mineNowLabel)
	for i, item := range t.kindItems {
		if i < len(kindness.Order) {
			item.Checked = kindness.Order[i] == v.kindness
		}
	}
	t.desk.SetSystemTrayMenu(t.menu) // re-apply to reflect the new labels
}

// setToggle shapes the pause item: a submenu of snoozes while mining, a
// single "Resume mining" while the user has paused.
func (t *fyneTray) setToggle(v trayView) {
	if v.paused {
		t.pause.Label = labelResume
		t.pause.ChildMenu = nil
		t.pause.Action = t.u.onToggle
		return
	}
	t.pause.Label = v.pauseLabel
	t.pause.Action = nil
	if t.pause.ChildMenu == nil {
		t.pause.ChildMenu = t.snoozeMenu()
	}
}

func (t *fyneTray) stop() {}

// trayStatsInterval is how often the Fyne tray may re-apply for the numbers
// alone.
const trayStatsInterval = 30 * time.Second

// trayMenuKey is the rendered text of the dynamic tray items, split by how
// urgently a change must reach the screen.
type trayMenuKey struct {
	// controls is the text that answers "what did my click just do" — the
	// status line and the labels of the items that act. It must never lag.
	controls string
	// stats is the live readout beside them. It moves on its own every tick
	// whether or not anyone is looking at the menu.
	stats string
}

// shouldReapplyMenu decides whether the Fyne tray re-applies its menu. A
// re-apply rebuilds every item over D-Bus on the GLFW main thread that also
// dispatches window input, and the hashrate alone changed every second — so
// the numbers are rate-limited and the controls are not. A user who clicks
// "Pause" sees the label change immediately, while the hashrate ticking
// underneath cannot drag the whole menu along with it.
func shouldReapplyMenu(cur, prev trayMenuKey, since time.Duration) bool {
	if cur.controls != prev.controls {
		return true
	}
	if cur.stats == prev.stats {
		return false
	}
	return since >= trayStatsInterval
}
