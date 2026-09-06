package gui

import (
	"testing"
	"time"
)

// TestShouldReapplyMenu pins the guard that keeps the tray from being rebuilt
// every second.
//
// This is not a micro-optimisation. Re-applying a Fyne 2.5.3 tray menu tears
// down and rebuilds every item over D-Bus on the GLFW main thread — the same
// thread that dispatches window input — and leaks a goroutine per item each
// time. The original guard hashed the live hashrate into the same key as the
// button labels, so it never matched twice and the menu was re-applied about
// once a second for the life of the process.
func TestShouldReapplyMenu(t *testing.T) {
	base := trayMenuKey{controls: "Mining · Balanced\x00Pause mining\x00Mine now\x00balanced", stats: "4.21 kH/s\x001m"}

	tests := []struct {
		name  string
		cur   trayMenuKey
		prev  trayMenuKey
		since time.Duration
		want  bool
	}{
		{
			name:  "nothing changed",
			cur:   base,
			prev:  base,
			since: time.Hour,
			want:  false,
		},
		{
			// The whole reason the app churned: a duty-cycled miner's hashrate
			// moves every tick, and on its own it must not drag the menu along.
			name:  "the hashrate moving alone does not rebuild",
			cur:   trayMenuKey{controls: base.controls, stats: "4.19 kH/s\x001m"},
			prev:  base,
			since: time.Second,
			want:  false,
		},
		{
			name:  "the numbers do get through eventually",
			cur:   trayMenuKey{controls: base.controls, stats: "4.19 kH/s\x001m"},
			prev:  base,
			since: trayStatsInterval,
			want:  true,
		},
		{
			// A user who clicks Pause must see the label change now, not in
			// half a minute — the click is how they know it worked.
			name:  "a control the user just acted on never waits",
			cur:   trayMenuKey{controls: "Mining paused\x00Resume mining\x00Mine now\x00balanced", stats: base.stats},
			prev:  base,
			since: 0,
			want:  true,
		},
		{
			name:  "changing kindness never waits",
			cur:   trayMenuKey{controls: "Mining · Ghost\x00Pause mining\x00Mine now\x00ghost", stats: base.stats},
			prev:  base,
			since: 0,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldReapplyMenu(tt.cur, tt.prev, tt.since); got != tt.want {
				t.Errorf("shouldReapplyMenu() = %v, want %v", got, tt.want)
			}
		})
	}
}
