package monitor

import (
	"sync"
	"time"
)

// Power reports whole-package CPU power draw in watts.
//
// It reads the platform's energy counter and differentiates it, which means the
// first call only establishes a baseline. Where no counter is readable — which
// is most desktops, since Linux made the RAPL counters root-only after they
// were shown to leak information about other processes' work — Watts reports
// "unknown" rather than modelling a number. A made-up wattage next to a real
// hashrate would be indistinguishable from a measured one.
type Power struct {
	mu sync.Mutex
	// last is the previous cumulative energy reading in microjoules, taken at
	// lastAt. started guards the first call, which has no interval to divide by.
	last    uint64
	lastAt  time.Time
	started bool
	ok      bool
	// wrapAt is the counter's range; the register is narrow enough to wrap
	// every minute or two under load, so the rollover must be handled.
	wrapAt uint64
}

// minSampleInterval is the shortest gap that yields a stable reading. Below it
// the counter's granularity dominates and the watt figure jumps around.
const minSampleInterval = 500 * time.Millisecond

// NewPower returns a power monitor, or one that always reports unknown when the
// platform has no readable energy counter.
func NewPower() *Power {
	energy, wrap, ok := readEnergyMicrojoules()
	p := &Power{ok: ok, wrapAt: wrap}
	if ok {
		p.last = energy
		p.lastAt = time.Now()
		p.started = true
	}
	return p
}

// Watts returns the average package power since the previous call. ok is false
// until a second reading is available, and on machines with no readable
// counter.
func (p *Power) Watts() (float64, bool) {
	if p == nil || !p.ok {
		return 0, false
	}
	energy, _, ok := readEnergyMicrojoules()
	if !ok {
		return 0, false
	}
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		p.last, p.lastAt, p.started = energy, now, true
		return 0, false
	}
	elapsed := now.Sub(p.lastAt)
	if elapsed < minSampleInterval {
		return 0, false
	}

	delta := energy - p.last
	if energy < p.last {
		// The counter wrapped. Without a known range the interval is
		// unrecoverable, so skip it and re-baseline.
		if p.wrapAt == 0 {
			p.last, p.lastAt = energy, now
			return 0, false
		}
		delta = p.wrapAt - p.last + energy
	}
	p.last, p.lastAt = energy, now

	// microjoules / microseconds = watts
	return float64(delta) / float64(elapsed.Microseconds()), true
}

// Available reports whether this machine exposes a readable energy counter.
func (p *Power) Available() bool { return p != nil && p.ok }

// AttributeToMiner splits a whole-package reading into the share attributable
// to mining, given the two CPU series.
//
// Power scales with more than utilisation — clock and voltage both rise with
// load — so this is a proportional approximation, not a measurement of the
// miner alone. It is the same approximation every per-process power tool makes,
// and it is honest at the extremes: all of the package power when only the
// miner is running, none of it when the miner is stopped.
func AttributeToMiner(packageWatts, minerCPU, otherCPU float64) (float64, bool) {
	total := minerCPU + otherCPU
	if packageWatts <= 0 || total <= 0 {
		return 0, false
	}
	return packageWatts * (minerCPU / total), true
}
