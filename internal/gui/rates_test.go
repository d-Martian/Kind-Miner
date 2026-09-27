package gui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/kind-miner/kind-miner/internal/engine"
)

func TestFormatRates(t *testing.T) {
	cases := []struct {
		name string
		r    rates
		want string
	}{
		{
			name: "the plan's example",
			r:    rates{now: 3420, m30: 3100, d24: 2870, nowOK: true, m30OK: true, d24OK: true},
			want: "3.42 kH/s now · 3.10 30 min · 2.87 24 h",
		},
		{
			// The unit follows the largest figure, so a dip below 1 kH/s is
			// still compared digit for digit, not read as a different scale.
			name: "one unit for the line",
			r:    rates{now: 950, m30: 1200, d24: 1100, nowOK: true, m30OK: true, d24OK: true},
			want: "0.95 kH/s now · 1.20 30 min · 1.10 24 h",
		},
		{
			name: "a fresh start cannot vouch for the long rates",
			r:    rates{now: 3420, nowOK: true},
			want: "3.42 kH/s now · — 30 min · — 24 h",
		},
		{
			name: "a slow machine stays in H/s",
			r:    rates{now: 480, m30: 450, nowOK: true, m30OK: true},
			want: "480 H/s now · 450 30 min · — 24 h",
		},
		{name: "nothing known yet", r: rates{}, want: "— H/s now · — 30 min · — 24 h"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatRates(c.r); got != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

// D = 2.6298e11 makes 1 kH/s find 0.01 blocks a month; at 0.6 XMR a block
// that is 0.006 XMR/month per kH/s, which keeps the arithmetic checkable.
var monthlyStats = engine.P2PoolStats{NetworkDifficulty: 262_980_000_000, BlockReward: 600_000_000_000,
	SidechainDifficulty: 43_200_000}

func TestFormatMonthly(t *testing.T) {
	r := rates{now: 1700, m30: 1550, d24: 1430, nowOK: true, m30OK: true, d24OK: true}
	// One precision for the line, set by the largest figure — the plan's
	// "≈ 0.0102 · 0.0093 · 0.0086", columns aligned.
	if got, want := formatMonthly(r, monthlyStats), "≈ 0.0102 · 0.0093 · 0.0086 XMR/month"; got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	fresh := rates{now: 1700, nowOK: true}
	if got := formatMonthly(fresh, monthlyStats); !strings.HasPrefix(got, "≈ 0.0102 · — · —") {
		t.Errorf("unknown rates must read as —, got %q", got)
	}
	if got := formatMonthly(r, engine.P2PoolStats{}); got != "≈ — · — · — XMR/month" {
		t.Errorf("no network stats yet, got %q", got)
	}
}

func TestNextShareUsesTheThirtyMinuteRate(t *testing.T) {
	// 43.2 M ÷ 2 kH/s = 21,600 s = 6 h: the plan's "next in ≈ 6 h".
	r := rates{now: 8000, m30: 2000, nowOK: true, m30OK: true}
	if got := nextShareLabel(r, monthlyStats); got != "next in ≈ 6h 0m" && got != "next in ≈ 6h" {
		t.Errorf("got %q, want six hours from the 30-minute rate", got)
	}
	if got := nextShareLabel(rates{}, monthlyStats); got != "" {
		t.Errorf("no rate known, got %q", got)
	}
}

func TestLongestRatePrefersTheDay(t *testing.T) {
	r := rates{now: 3, m30: 2, d24: 1, nowOK: true, m30OK: true, d24OK: true}
	if v, _ := r.longest(); v != 1 {
		t.Errorf("longest = %v, want the 24 h rate", v)
	}
	r.d24OK = false
	if v, _ := r.longest(); v != 2 {
		t.Errorf("longest = %v, want the 30 min rate", v)
	}
}

func TestDashboardShowsTheLedgerRows(t *testing.T) {
	// kvList.Set drops keys it was not built with, silently; a renamed row
	// would vanish from the dashboard without an error anywhere.
	test.NewApp()
	l := newKVList().Add("30 min", emDash).Add("24 h", emDash).Add("Est. per month", emDash)
	for _, key := range []string{"30 min", "24 h", "Est. per month"} {
		l.Set(key, "x")
		if l.values[key].Text != "x" {
			t.Errorf("row %q did not take a value", key)
		}
	}
}
