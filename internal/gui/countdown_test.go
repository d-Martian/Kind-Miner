package gui

import (
	"testing"

	"github.com/kind-miner/kind-miner/internal/scheduler"
)

func TestCountdownLabel(t *testing.T) {
	tests := []struct {
		name string
		secs int
		ok   bool
		want string
	}{
		{"no countdown applies", 0, false, statusIdle},
		{"seconds when nearly there", 47, true, "Full speed in 47s"},
		{"at the fine-grained boundary", 90, true, "Full speed in 90s"},
		// Above the boundary the label changes once a minute, so re-applying the
		// tray menu (which closes an open menu) stays rare.
		{"coarse minutes above the boundary", 91, true, "Full speed in ~2m"},
		{"rounds minutes up", 240, true, "Full speed in ~4m"},
		{"exact minute", 300, true, "Full speed in ~5m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countdownLabel(tt.secs, tt.ok); got != tt.want {
				t.Errorf("countdownLabel(%d, %v) = %q, want %q", tt.secs, tt.ok, got, tt.want)
			}
		})
	}
}

func TestTrayStatusLine(t *testing.T) {
	counting := func() (int, bool) { return 42, true }
	noCountdown := func() (int, bool) { return 0, false }

	tests := []struct {
		name      string
		countdown func() (int, bool)
		override  scheduler.Override
		state     scheduler.State
		reason    string
		want      string
	}{
		{
			name:      "waiting for idle shows the countdown",
			countdown: counting,
			override:  scheduler.OverrideNone,
			state:     scheduler.StateReduced,
			reason:    scheduler.ReasonWaitingForIdle,
			want:      "Full speed in 42s",
		},
		{
			name:      "paused says so",
			countdown: counting,
			override:  scheduler.OverridePause,
			state:     scheduler.StatePaused,
			want:      statusPaused,
		},
		{
			// The whole point of the feature: mining on request is still polite,
			// and the user needs to see that rather than think the toggle broke.
			name:      "mine now while the machine is busy explains the backoff",
			countdown: noCountdown,
			override:  scheduler.OverrideMine,
			state:     scheduler.StateReduced,
			reason:    "system CPU 55%",
			want:      statusYielding,
		},
		{
			name:      "mine now at full speed",
			countdown: noCountdown,
			override:  scheduler.OverrideMine,
			state:     scheduler.StateFull,
			want:      statusMineNow,
		},
		{
			name:      "automatic backoff under load",
			countdown: noCountdown,
			override:  scheduler.OverrideNone,
			state:     scheduler.StateReduced,
			reason:    "system CPU 55%",
			want:      statusYielding,
		},
		{
			name:      "mining freely",
			countdown: noCountdown,
			override:  scheduler.OverrideNone,
			state:     scheduler.StateFull,
			want:      statusIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trayStatusLine(tt.countdown, tt.override, tt.state, tt.reason)
			if got != tt.want {
				t.Errorf("trayStatusLine() = %q, want %q", got, tt.want)
			}
		})
	}
}
