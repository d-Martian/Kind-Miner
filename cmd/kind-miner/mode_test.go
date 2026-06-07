package main

import "testing"

func TestDecideMode(t *testing.T) {
	tests := []struct {
		name          string
		forceHeadless bool
		forceGUI      bool
		isTTY         bool
		hasDisplay    bool
		want          uiMode
	}{
		// Default desktop launches.
		{"terminal on desktop → tray (as-is)", false, false, true, true, modeTray},
		{"double-click on desktop → gui", false, false, false, true, modeGUI},

		// No display: never a window or tray.
		{"terminal over ssh, no display → headless", false, false, true, false, modeHeadless},
		{"cron, no display → headless", false, false, false, false, modeHeadless},

		// Explicit --gui.
		{"--gui from terminal → gui", false, true, true, true, modeGUI},
		{"--gui double-click → gui", false, true, false, true, modeGUI},
		{"--gui without display → headless fallback", false, true, true, false, modeHeadless},

		// Explicit --no-tray/--headless wins over everything.
		{"--no-tray on desktop → headless", true, false, true, true, modeHeadless},
		{"--no-tray beats --gui", true, true, false, true, modeHeadless},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideMode(tt.forceHeadless, tt.forceGUI, tt.isTTY, tt.hasDisplay)
			if got != tt.want {
				t.Errorf("decideMode(headless=%v, gui=%v, tty=%v, display=%v) = %v, want %v",
					tt.forceHeadless, tt.forceGUI, tt.isTTY, tt.hasDisplay, got, tt.want)
			}
		})
	}
}
