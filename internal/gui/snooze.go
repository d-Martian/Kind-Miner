package gui

import (
	"time"

	"fyne.io/fyne/v2"

	"github.com/kind-miner/kind-miner/internal/scheduler"
)

// The tray's pause is a snooze, because most pauses have an end the user
// already knows — a call, a game, the rest of the evening — and a pause that
// has to be remembered and undone tends to become an uninstall.

// morningHour is when "until tomorrow" ends. Midnight would bring the miner
// back half an hour after a 23:30 pause; six in the morning is after a night
// and before a working day.
const morningHour = 6

const (
	snoozeHour     = "1 hour"
	snoozeTomorrow = "Until tomorrow, 06:00"
	snoozeResume   = "Until I resume"
	labelPauseMenu = "Pause"
)

// nextMorning is the end of "until tomorrow": the next 06:00, local time,
// strictly after now.
func nextMorning(now time.Time) time.Time {
	m := time.Date(now.Year(), now.Month(), now.Day(), morningHour, 0, 0, 0, now.Location())
	if !m.After(now) {
		m = m.AddDate(0, 0, 1)
	}
	return m
}

// pausedStatus says how long the user's pause lasts, in the words a person
// would use: a time today, "tomorrow", or a weekday.
func pausedStatus(until, now time.Time) string {
	if until.IsZero() {
		return statusPaused
	}
	until = until.In(now.Location())
	y1, m1, d1 := now.Date()
	y2, m2, d2 := until.Date()
	today := time.Date(y1, m1, d1, 0, 0, 0, 0, now.Location())
	day := time.Date(y2, m2, d2, 0, 0, 0, 0, now.Location())
	switch days := int(day.Sub(today).Hours() / 24); {
	case days <= 0:
		return "Paused until " + until.Format("15:04")
	case days == 1:
		return "Paused until tomorrow " + until.Format("15:04")
	default:
		return "Paused until " + until.Format("Mon 15:04")
	}
}

// snoozeMenu is the "Pause ▸" submenu.
func (u *uiApp) snoozeMenu() *fyne.Menu {
	pause := func(until func() time.Time) func() {
		return func() {
			if s := u.sup.Scheduler(); s != nil {
				s.PauseUntil(until())
				u.updateDashboard()
			}
		}
	}
	return fyne.NewMenu("",
		fyne.NewMenuItem(snoozeHour, pause(func() time.Time { return time.Now().Add(time.Hour) })),
		fyne.NewMenuItem(snoozeTomorrow, pause(func() time.Time { return nextMorning(time.Now()) })),
		fyne.NewMenuItem(snoozeResume, pause(func() time.Time { return time.Time{} })),
	)
}

// setToggle shapes the tray's pause item for the current state: a submenu of
// snoozes while mining, a single "Resume mining" while the user has paused.
func (u *uiApp) setToggle(override scheduler.Override, label string) {
	if u.mToggle == nil {
		return
	}
	if override == scheduler.OverridePause {
		u.mToggle.Label = labelResume
		u.mToggle.ChildMenu = nil
		u.mToggle.Action = u.onToggle
		return
	}
	u.mToggle.Label = label
	u.mToggle.Action = nil
	if u.mToggle.ChildMenu == nil {
		u.mToggle.ChildMenu = u.snoozeMenu()
	}
}
