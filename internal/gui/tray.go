package gui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"

	"github.com/kind-miner/kind-miner/internal/kindness"
	"github.com/kind-miner/kind-miner/internal/scheduler"
)

// The tray is computed, then rendered. trayView is everything it shows,
// worked out from the miner's state in one place; a trayRenderer puts it on
// screen. There are two renderers because the right way to update a tray
// differs by platform (see tray_systray.go and tray_fyne.go), and keeping what
// to show apart from how to show it means both show the same thing.

// trayView is one moment of the tray menu.
type trayView struct {
	icon fyne.Resource

	status, hashrate, income, uptime, shares, reward string
	payouts                                          []string

	// paused is the user's own pause: the pause item becomes a single Resume
	// instead of the snooze submenu.
	paused bool
	// pauseLabel names the snooze submenu when not paused: "Pause", or "Keep
	// paused" when the scheduler has already stood the miner down.
	pauseLabel   string
	mineNowLabel string
	kindness     kindness.Level
}

// trayRenderer puts a trayView on screen.
type trayRenderer interface {
	render(trayView)
	// stop takes the tray down at shutdown.
	stop()
}

// computeTrayView gathers the tray's state. ok is false before mining starts.
func (u *uiApp) computeTrayView(now time.Time) (trayView, bool) {
	s := u.sup.Scheduler()
	if s == nil {
		return trayView{}, false
	}
	state, reason := s.CurrentState()
	override := s.Override()
	_, held := s.Held()

	v := trayView{
		icon:         trayIcon(state, held),
		status:       trayStatusLine(s.IdleCountdown, override, state, reason),
		income:       "≈ " + emDash + " XMR/month",
		uptime:       emDash,
		shares:       emDash,
		reward:       rewardEstimating,
		paused:       override == scheduler.OverridePause,
		pauseLabel:   labelPauseMenu,
		mineNowLabel: labelMineNow,
		kindness:     s.Preset().Level,
	}
	if until, paused := s.PausedUntil(); paused {
		v.status = pausedStatus(until, now)
	}
	r := currentRates(s.History(), s.Ledger(), now)
	v.hashrate = formatRates(r)
	if up, ok := u.sup.Uptime(); ok {
		v.uptime = formatUptime(up)
	}
	if p := u.sup.P2Pool(); p != nil {
		if st, ok := p.Stats(); ok {
			v.shares = fmt.Sprintf("%d found", st.SharesFound)
			if occupancy, ok := st.WindowOccupancy(); ok {
				v.shares += fmt.Sprintf(" · %s of the payout window", formatPercent(occupancy))
			}
			if next := nextShareLabel(r, st); next != "" {
				v.shares += " · " + next
			}
			v.income = formatMonthly(r, st)
			v.reward = rewardLabel(st, true)
		}
	}
	v.payouts = payoutLines(u.sup.Payouts().Recent(payoutsShown), now)
	if !v.paused && state == scheduler.StatePaused {
		v.pauseLabel = labelKeepPaused
	}
	if override == scheduler.OverrideMine {
		v.mineNowLabel = labelMineAuto
	}
	return v, true
}

// The fixed labels of the tray's status lines.
const (
	uptimePrefix = "Uptime  "
	sharesPrefix = "Shares  "
)

// kindnessItemLabel is a preset's line in the Kindness submenu.
func kindnessItemLabel(p kindness.Preset) string {
	return fmt.Sprintf("%s — up to %d%% CPU", p.Label, p.CeilingPercent())
}

// snoozes are the Pause submenu's choices, shared by both renderers.
func (u *uiApp) snoozes() []struct {
	label string
	until func() time.Time
} {
	return []struct {
		label string
		until func() time.Time
	}{
		{snoozeHour, func() time.Time { return time.Now().Add(time.Hour) }},
		{snoozeTomorrow, func() time.Time { return nextMorning(time.Now()) }},
		{snoozeResume, func() time.Time { return time.Time{} }},
	}
}

// snoozeUntil pauses until the chosen time.
func (u *uiApp) snoozeUntil(until func() time.Time) {
	if s := u.sup.Scheduler(); s != nil {
		s.PauseUntil(until())
		u.updateDashboard()
	}
}
