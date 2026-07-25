package gui

import "fmt"

// statusIdle is the tray status line when nothing is holding mining back.
const statusIdle = "Mining at full speed when free"

// fineGrainedBelow is the point where the countdown switches from a coarse
// minute estimate to a live per-second one.
//
// The label only changes once a minute above this threshold, and Fyne 2.5.3 can
// only update a menu label by re-applying the whole menu — which closes it if
// the user is looking at it. A ticking seconds display is worth that cost only
// when the wait is nearly over and the user might actually be watching it.
const fineGrainedBelow = 90

// countdownLabel renders the tray status line.
//
// secs is the seconds remaining before mining ramps to full speed; ok is false
// when no countdown applies, which covers a disabled gate, a system without idle
// detection, and the case where the user is already idle.
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
