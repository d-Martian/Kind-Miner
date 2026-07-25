package gui

import "testing"

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
