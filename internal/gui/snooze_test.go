package gui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/scheduler"
)

func TestNextMorning(t *testing.T) {
	loc := time.FixedZone("here", 2*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 9, 27, h, m, 0, 0, loc) }
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		// Not midnight: that would bring mining back half an hour later.
		{"late evening waits for the morning", at(23, 30), time.Date(2026, 9, 28, 6, 0, 0, 0, loc)},
		{"after six it is tomorrow's six", at(9, 15), time.Date(2026, 9, 28, 6, 0, 0, 0, loc)},
		{"at exactly six it is tomorrow's six", at(6, 0), time.Date(2026, 9, 28, 6, 0, 0, 0, loc)},
		{"small hours end this morning", at(3, 0), at(6, 0)},
	}
	for _, c := range cases {
		if got := nextMorning(c.now); !got.Equal(c.want) {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestPausedStatus(t *testing.T) {
	now := time.Date(2026, 9, 27, 14, 0, 0, 0, time.UTC) // a Sunday
	cases := []struct {
		until time.Time
		want  string
	}{
		{time.Time{}, statusPaused},
		{now.Add(time.Hour), "Paused until 15:00"},
		{time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC), "Paused until tomorrow 06:00"},
		{time.Date(2026, 9, 30, 6, 0, 0, 0, time.UTC), "Paused until Wed 06:00"},
	}
	for _, c := range cases {
		if got := pausedStatus(c.until, now); got != c.want {
			t.Errorf("until %v: got %q, want %q", c.until, got, c.want)
		}
	}
}

func TestTrayPauseIsASnoozeMenuUntilPaused(t *testing.T) {
	test.NewApp()
	u := &uiApp{sup: core.New(config.Defaults())}
	u.mToggle = fyne.NewMenuItem(labelPauseMenu, nil)
	u.setToggle(scheduler.OverrideNone, labelPauseMenu)
	if u.mToggle.ChildMenu == nil || len(u.mToggle.ChildMenu.Items) != 3 || u.mToggle.Action != nil {
		t.Fatalf("while mining: %+v", u.mToggle)
	}
	for i, want := range []string{snoozeHour, snoozeTomorrow, snoozeResume} {
		if got := u.mToggle.ChildMenu.Items[i].Label; got != want {
			t.Errorf("snooze %d = %q, want %q", i, got, want)
		}
	}
	u.setToggle(scheduler.OverridePause, labelResume)
	if u.mToggle.ChildMenu != nil || u.mToggle.Action == nil || u.mToggle.Label != labelResume {
		t.Errorf("while paused: %+v, want a single Resume action", u.mToggle)
	}
}
