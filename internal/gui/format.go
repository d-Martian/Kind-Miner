package gui

import (
	"fmt"
	"runtime"
	"time"
)

// cpuCount is the number of logical cores, the denominator for "6 of 8 threads".
func cpuCount() int { return runtime.NumCPU() }

// Formatting for everything the dashboard, tray and settings display.
//
// The house rule: never show more precision than the number deserves. A
// hashrate averaged over five seconds gets two decimals; an income estimate
// drawn from a random process gets a "~" and coarse units. Displaying an
// estimate to six figures reads as a promise the miner cannot keep.

// emDash is what every field shows before it has a real value. It is used
// deliberately in place of a zero: "0 W" claims a measurement, "—" admits there
// isn't one.
const emDash = "—"

// formatHashrate renders a H/s value with a unit that keeps it readable.
func formatHashrate(hs float64) string {
	value, unit := hashrateParts(hs)
	if unit == "" {
		return emDash
	}
	return value + " " + unit
}

// hashrateParts splits a hashrate into its number and unit so the dashboard can
// typeset them at different sizes. An unavailable reading returns ("—", "").
func hashrateParts(hs float64) (value, unit string) {
	switch {
	case hs >= 1_000_000:
		return fmt.Sprintf("%.2f", hs/1_000_000), "MH/s"
	case hs >= 1_000:
		return fmt.Sprintf("%.2f", hs/1_000), "kH/s"
	case hs > 0:
		return fmt.Sprintf("%.0f", hs), "H/s"
	default:
		return emDash, ""
	}
}

// formatXMR renders an amount of Monero. Daily estimates for a desktop are
// small enough that five decimals is the first place a digit appears.
func formatXMR(v float64, decimals int) string {
	if v <= 0 {
		return emDash
	}
	return fmt.Sprintf("%.*f", decimals, v)
}

// formatWatts renders a power reading, or "—" where the machine's energy
// counters are not readable. See monitor.Power for why that is common.
func formatWatts(w float64, known bool) string {
	if !known || w <= 0 {
		return emDash
	}
	return fmt.Sprintf("%.0f", w)
}

// formatEfficiency renders hashes per watt — the number that says whether the
// electricity is buying anything.
func formatEfficiency(hashrate, watts float64, known bool) string {
	if !known || watts <= 1 || hashrate <= 0 {
		return emDash
	}
	return fmt.Sprintf("%.0f H/W", hashrate/watts)
}

// formatTemp renders a CPU temperature, or "—" where no sensor answered.
func formatTemp(c float64) string {
	if c <= 0 {
		return emDash
	}
	return fmt.Sprintf("%.0f °C", c)
}

// formatPercent renders a 0–1 fraction as a whole percentage.
func formatPercent(f float64) string {
	return fmt.Sprintf("%.0f%%", f*100)
}

// formatUptime renders how long mining has been running, in the compact form
// the tray and title bar use.
func formatUptime(d time.Duration) string {
	if d <= 0 {
		return emDash
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %02dm", h, m)
}

// formatBackoff renders how often the miner stepped aside in the last minute.
// It is phrased as an achievement rather than an error, because that is what it
// is: every count is a moment the user got their machine back.
func formatBackoff(n int, paused bool) string {
	switch {
	case paused:
		return "paused"
	case n == 0:
		return "no interruptions this minute"
	case n == 1:
		return "stepped aside once this minute"
	default:
		return fmt.Sprintf("stepped aside %d× this minute", n)
	}
}

// valueWidth is the longest a detail value may be before it is elided.
//
// It exists because canvas.Text does not wrap, so its width becomes a minimum
// width for everything containing it — and Fyne sizes a window to its content's
// minimum. One un-elided 75-character .onion address in the details panel
// stretched the whole window past 2300px on a live run.
const valueWidth = 34

// shortenMiddle elides the middle of an over-long value, keeping both ends.
//
// The ends are what identify a value at a glance — the first characters of an
// onion address and its port — so cutting from the middle preserves more than
// truncating the tail would.
func shortenMiddle(s string, max int) string {
	if max < 5 || len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)
	keep := max - 1 // one rune goes to the ellipsis
	head := (keep + 1) / 2
	tail := keep - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// formatShareInterval renders how often this machine lands a p2pool share.
func formatShareInterval(d time.Duration, ok bool) string {
	if !ok {
		return emDash
	}
	return "every " + formatETA(d)
}
