package gui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// rates are the three hashrates the tray quotes. "now" is the headline figure
// the dashboard already shows; 30 minutes and 24 hours come from the ledger,
// which counts paused time as zero and survives a restart.
type rates struct {
	now, m30, d24       float64
	nowOK, m30OK, d24OK bool
}

func currentRates(history []stats.Sample, ledger *stats.Ledger, now time.Time) rates {
	var r rates
	r.now, r.nowOK = stats.Mean(history, hashrateWindow, stats.Hashrate)
	if ledger != nil {
		r.m30, r.m30OK = ledger.Rate(30*time.Minute, now)
		r.d24, r.d24OK = ledger.Rate(24*time.Hour, now)
	}
	return r
}

// longest returns the rate over the longest window that is known: the income
// estimate should move with the day, not with the moment.
func (r rates) longest() (float64, bool) {
	switch {
	case r.d24OK:
		return r.d24, true
	case r.m30OK:
		return r.m30, true
	}
	return r.now, r.nowOK
}

// formatRates renders "3.42 kH/s now · 3.10 30 min · 2.87 24 h". All three
// share the unit of the largest, written once, so the eye compares digits
// rather than parsing units; a rate the ledger cannot vouch for yet is "—".
func formatRates(r rates) string {
	max := 0.0
	for _, v := range []float64{r.now, r.m30, r.d24} {
		max = math.Max(max, v)
	}
	_, unit := hashrateParts(max)
	if unit == "" {
		unit = "H/s"
	}
	scale := map[string]float64{"H/s": 1, "kH/s": 1e3, "MH/s": 1e6}[unit]
	num := func(v float64, ok bool) string {
		if !ok {
			return emDash
		}
		if unit == "H/s" {
			return fmt.Sprintf("%.0f", v)
		}
		return fmt.Sprintf("%.2f", v/scale)
	}
	return fmt.Sprintf("%s %s now · %s 30 min · %s 24 h",
		num(r.now, r.nowOK), unit, num(r.m30, r.m30OK), num(r.d24, r.d24OK))
}

// formatMonthly renders "≈ 0.0102 · 0.0093 · 0.0086 XMR/month", one figure per
// rate. The "≈" is part of the claim: this is an expectation over a random
// process, and a month of real payouts arrives in lumps around it.
func formatMonthly(r rates, st engine.P2PoolStats) string {
	vals := make([]float64, 3)
	oks := []bool{r.nowOK, r.m30OK, r.d24OK}
	for i, h := range []float64{r.now, r.m30, r.d24} {
		if oks[i] {
			vals[i], oks[i] = st.EstimatedXMRPerMonth(h)
		}
	}
	decimals := monthlyDecimals(vals)
	parts := make([]string, 3)
	for i, v := range vals {
		if !oks[i] || v <= 0 {
			parts[i] = emDash
			continue
		}
		parts[i] = fmt.Sprintf("%.*f", decimals, v)
	}
	return "≈ " + strings.Join(parts, " · ") + " XMR/month"
}

// monthlyDecimals picks one precision for the line: enough that the largest
// figure shows three significant digits and no more, since the estimate is
// not good for a fourth. Shared, so the three columns line up.
func monthlyDecimals(vals []float64) int {
	max := 0.0
	for _, v := range vals {
		max = math.Max(max, v)
	}
	if max <= 0 {
		return 4
	}
	d := 2 - int(math.Floor(math.Log10(max)))
	if d < 2 {
		d = 2
	}
	if d > 8 {
		d = 8
	}
	return d
}

// nextShareLabel is "next in ≈ 6 h", from the 30-minute rate: the pace the
// miner has actually kept, throttling included, rather than p2pool's view of
// the hashes it happened to receive.
func nextShareLabel(r rates, st engine.P2PoolStats) string {
	h, ok := r.m30, r.m30OK
	if !ok {
		h, ok = r.now, r.nowOK
	}
	if !ok {
		return ""
	}
	d, ok := st.ShareIntervalAt(h)
	if !ok {
		return ""
	}
	return "next in ≈ " + formatETA(d)
}

// rateOrDash is one rate for a detail row, or "—" when the ledger cannot vouch
// for it yet.
func rateOrDash(v float64, ok bool) string {
	if !ok {
		return emDash
	}
	return formatHashrate(v)
}
