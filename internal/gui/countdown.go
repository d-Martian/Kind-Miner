package gui

import (
	"fmt"

	"github.com/kind-miner/kind-miner/internal/scheduler"
)

// Tray status lines and the mine-now menu labels.
const (
	statusIdle     = "Mining at full speed when free"
	statusPaused   = "Mining paused"
	statusMineNow  = "Mining now — still yielding to your apps"
	statusYielding = "Backing off — your apps come first"

	labelMineNow  = "Mine at full speed now"
	labelMineAuto = "Return to automatic (wait for idle)"
)

// fineGrainedBelow is the point where the countdown switches from a coarse
// minute estimate to a live per-second one.
//
// The label only changes once a minute above this threshold, and Fyne 2.5.3 can
// only update a menu label by re-applying the whole menu — which closes it if
// the user is looking at it. A ticking seconds display is worth that cost only
// when the wait is nearly over and the user might actually be watching it.
const fineGrainedBelow = 90

// countdownLabel renders the remaining wait before mining ramps to full speed.
//
// secs is the seconds remaining; ok is false when no countdown applies, which
// covers a disabled gate, a system without idle detection, and the case where
// the user is already idle.
func countdownLabel(secs int, ok bool) string {
	if !ok {
		return statusIdle
	}
	if secs <= fineGrainedBelow {
		return fmt.Sprintf("Full speed in %ds", secs)
	}
	mins := (secs + 59) / 60
	return fmt.Sprintf("Full speed in ~%dm", mins)
}

// trayStatusLine renders the tray's first line: what the miner is doing and why.
//
// The "why" matters more than it looks. A user who can't tell why their machine
// feels different is liable to uninstall the miner rather than investigate, so
// the line always says whether mining is backing off for them.
func trayStatusLine(countdown func() (int, bool), override scheduler.Override, state scheduler.State, reason string) string {
	switch override {
	case scheduler.OverridePause:
		return statusPaused
	case scheduler.OverrideMine:
		// Mining on request still throttles under load — say so, otherwise the
		// throttling looks like the toggle failed.
		if state != scheduler.StateFull {
			return statusYielding
		}
		return statusMineNow
	}
	if state != scheduler.StateFull && reason != scheduler.ReasonWaitingForIdle && reason != "" {
		return statusYielding
	}
	return countdownLabel(countdown())
}
