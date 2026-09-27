package gui

import (
	"fmt"
	"math"
	"time"

	"fyne.io/fyne/v2"

	"github.com/kind-miner/kind-miner/internal/stats"
)

// payoutsShown is how many payouts the tray lists.
const payoutsShown = 5

// coinbaseLock is how long a payout waits before it can be spent: a coinbase
// output unlocks 60 blocks after the block that created it, at Monero's
// two-minute target, so about two hours.
const coinbaseLock = 60 * 2 * time.Minute

// Menu labels for the payouts submenu. The header is the honest caveat: this
// is what kind-miner saw in p2pool's log while it ran, not the wallet's
// history.
const (
	payoutsMenuLabel  = "Recent payouts"
	payoutsSeenHeader = "Seen while running"
	payoutsNoneYet    = "None seen yet"
)

// payoutLines renders the submenu's rows, newest first.
func payoutLines(list []stats.Payout, now time.Time) []string {
	if len(list) == 0 {
		return []string{payoutsNoneYet}
	}
	if len(list) > payoutsShown {
		list = list[:payoutsShown]
	}
	lines := make([]string, 0, len(list))
	for _, p := range list {
		age := now.Sub(p.SeenAt)
		line := formatAtomic(p.Atomic) + " XMR · " + formatAge(age)
		if left := coinbaseLock - age; left > 0 {
			line += " · spendable in ≈ " + formatETA(left)
		}
		lines = append(lines, line)
	}
	return lines
}

// formatAtomic renders a payout to four significant digits: enough to tell
// payouts apart, without the twelve decimal places p2pool prints. The exact
// figure is in the wallet.
func formatAtomic(atomic uint64) string {
	v := float64(atomic) / 1e12
	if v <= 0 {
		return emDash
	}
	d := 3 - int(math.Floor(math.Log10(v)))
	if d < 4 {
		d = 4
	}
	if d > 12 {
		d = 12
	}
	return fmt.Sprintf("%.*f", d, v)
}

// formatAge renders how long ago a payout was seen, as coarsely as a menu
// that is rebuilt at most once a minute can honestly claim.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// payoutsMenu is the submenu: the "seen while running" caveat, then the rows.
// Every item is disabled — they are facts, not actions.
func payoutsMenu(lines []string) *fyne.Menu {
	items := []*fyne.MenuItem{disabledItem(payoutsSeenHeader), fyne.NewMenuItemSeparator()}
	for _, l := range lines {
		items = append(items, disabledItem(l))
	}
	return fyne.NewMenu("", items...)
}
