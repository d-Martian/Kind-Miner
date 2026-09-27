//go:build !windows && !darwin

package gui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/systray"

	"github.com/kind-miner/kind-miner/internal/kindness"
)

// systrayTray drives fyne.io/systray directly, on Linux and the BSDs.
//
// Fyne's tray cannot change one item: each change resets the menu and adds
// every item again, with new IDs, and a tray host shown a whole new menu
// closes the one that was open. Fyne 2.8 still does this. Here the items are
// created once and kept; an update sets one item's title or check mark in
// place, so the host re-reads a layout whose items are the same items, and a
// menu the user has open stays open while its numbers move.
//
// This is only possible where the tray is plain D-Bus, as it is here: systray
// needs no event loop of its own, so it runs alongside Fyne's window without
// either knowing about the other.
type systrayTray struct {
	u *uiApp

	status, hashrate, income, uptime, shares, reward *systray.MenuItem
	payoutRows                                       []*systray.MenuItem
	kinds                                            []*systray.MenuItem
	mineNow, pause, resume                           *systray.MenuItem

	last      trayView
	lastIcon  fyne.Resource
	statsAt   time.Time
	rendered  bool
	lastLines []string
}

// systrayStatsInterval is how often the numbers may move. Each change is a
// small D-Bus signal, not a rebuild, so this is about not chattering, not
// about protecting an open menu.
const systrayStatsInterval = 5 * time.Second

func newSystrayTray(u *uiApp) *systrayTray {
	t := &systrayTray{u: u}
	info := func(label string) *systray.MenuItem {
		item := systray.AddMenuItem(label, "")
		item.Disable()
		return item
	}
	onClick := func(item *systray.MenuItem, fn func()) {
		go func() {
			for range item.ClickedCh {
				fn()
			}
		}()
	}

	t.status = info(statusIdle)
	t.hashrate = info(formatRates(rates{}))
	t.income = info("≈ " + emDash + " XMR/month")
	t.uptime = info(uptimePrefix + emDash)
	t.shares = info(sharesPrefix + emDash)
	t.reward = info(rewardEstimating)

	payouts := systray.AddMenuItem(payoutsMenuLabel, "")
	header := payouts.AddSubMenuItem(payoutsSeenHeader, "")
	header.Disable()
	for i := 0; i < payoutsShown; i++ {
		row := payouts.AddSubMenuItem(payoutsNoneYet, "")
		row.Disable()
		if i > 0 {
			row.Hide()
		}
		t.payoutRows = append(t.payoutRows, row)
	}

	systray.AddSeparator()
	kind := systray.AddMenuItem("Kindness", "")
	current := u.sup.Config().Kindness
	for _, preset := range kindness.All() {
		level := preset.Level
		item := kind.AddSubMenuItemCheckbox(kindnessItemLabel(preset), "", level == current)
		onClick(item, func() { u.setKindness(level) })
		t.kinds = append(t.kinds, item)
	}
	t.mineNow = systray.AddMenuItem(labelMineNow, "")
	onClick(t.mineNow, u.onMineNowToggle)
	// Pause and Resume are two items, one hidden: an item cannot switch
	// between being a submenu and being an action.
	t.pause = systray.AddMenuItem(labelPauseMenu, "")
	for _, s := range u.snoozes() {
		until := s.until
		onClick(t.pause.AddSubMenuItem(s.label, ""), func() { u.snoozeUntil(until) })
	}
	t.resume = systray.AddMenuItem(labelResume, "")
	t.resume.Hide()
	onClick(t.resume, u.onToggle)

	systray.AddSeparator()
	onClick(systray.AddMenuItem("Open kind-miner", ""), u.showWindow)
	onClick(systray.AddMenuItem("Settings", ""), u.onSettings)
	systray.AddSeparator()
	onClick(systray.AddMenuItem("Quit", ""), func() { u.app.Quit() })

	systray.SetTitle("kind-miner")
	systray.SetTooltip("kind-miner")
	systray.SetIcon(resMining.Content())
	t.lastIcon = resMining
	systray.SetOnTapped(u.showWindow)

	// The items are all in place before the tray is published, so the host's
	// first look is the finished menu.
	start, _ := systray.RunWithExternalLoop(nil, nil)
	start()
	return t
}

func (t *systrayTray) render(v trayView) {
	if v.icon != t.lastIcon {
		t.lastIcon = v.icon
		systray.SetIcon(v.icon.Content())
	}

	// Controls at once: they answer "what did my click just do".
	set(t.status, t.last.status, v.status, t.rendered)
	set(t.mineNow, t.last.mineNowLabel, v.mineNowLabel, t.rendered)
	set(t.pause, t.last.pauseLabel, v.pauseLabel, t.rendered)
	if !t.rendered || v.paused != t.last.paused {
		if v.paused {
			t.pause.Hide()
			t.resume.Show()
		} else {
			t.resume.Hide()
			t.pause.Show()
		}
	}
	if !t.rendered || v.kindness != t.last.kindness {
		for i, item := range t.kinds {
			if i < len(kindness.Order) && kindness.Order[i] == v.kindness {
				item.Check()
			} else {
				item.Uncheck()
			}
		}
	}

	// Numbers at a gentler pace.
	now := time.Now()
	if !t.rendered || now.Sub(t.statsAt) >= systrayStatsInterval {
		t.statsAt = now
		set(t.hashrate, t.last.hashrate, v.hashrate, t.rendered)
		set(t.income, t.last.income, v.income, t.rendered)
		set(t.uptime, uptimePrefix+t.last.uptime, uptimePrefix+v.uptime, t.rendered)
		set(t.shares, sharesPrefix+t.last.shares, sharesPrefix+v.shares, t.rendered)
		set(t.reward, t.last.reward, v.reward, t.rendered)
		t.renderPayouts(v.payouts)
		t.last.hashrate, t.last.income, t.last.uptime = v.hashrate, v.income, v.uptime
		t.last.shares, t.last.reward = v.shares, v.reward
	}

	t.last.status, t.last.mineNowLabel, t.last.pauseLabel = v.status, v.mineNowLabel, v.pauseLabel
	t.last.paused, t.last.kindness = v.paused, v.kindness
	t.rendered = true
}

// renderPayouts fills the fixed rows, hiding the ones not needed.
func (t *systrayTray) renderPayouts(lines []string) {
	for i, row := range t.payoutRows {
		var was string
		if i < len(t.lastLines) {
			was = t.lastLines[i]
		}
		if i < len(lines) {
			if lines[i] != was {
				row.SetTitle(lines[i])
			}
			if i >= len(t.lastLines) {
				row.Show()
			}
		} else if i < len(t.lastLines) {
			row.Hide()
		}
	}
	t.lastLines = lines
}

// set changes an item's title only when it changed: every SetTitle is a
// signal to the tray host.
func set(item *systray.MenuItem, was, now string, rendered bool) {
	if !rendered || was != now {
		item.SetTitle(now)
	}
}

// stop leaves the tray to the end of the process. systray's own teardown
// closes the session bus connection it shares with every other D-Bus user in
// kind-miner; when the process exits, the bus drops the tray item anyway.
func (t *systrayTray) stop() {}

// newTray drives systray directly here: see systrayTray.
func newTray(u *uiApp) trayRenderer { return newSystrayTray(u) }
