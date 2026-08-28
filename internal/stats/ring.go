// Package stats keeps a short rolling history of what the miner and the rest of
// the machine were doing.
//
// This exists to make kind-miner's restraint visible. A user who notices their
// machine behaving oddly has no way to tell whether the miner is responsible,
// and the safe assumption — uninstall it — is the one that costs them the
// earnings they installed it for. A chart of miner CPU against everything
// else's CPU shows the miner stepping back as other work arrives, which is an
// answer no status line can give.
package stats

import (
	"sync"
	"time"
)

// Sample is one observation of the machine at a moment in time.
type Sample struct {
	At time.Time
	// MinerCPU and OtherCPU are fractions in [0,1] of total CPU capacity.
	MinerCPU float64
	OtherCPU float64
	// Hashrate is the miner's hashrate in H/s at that moment.
	Hashrate float64
	// TempC is the hottest CPU sensor in Celsius, or 0 where none was readable.
	TempC float64
	// Watts is whole-package power draw. WattsKnown is false on the many
	// machines whose energy counters are root-only, where showing a number
	// would mean inventing one.
	Watts      float64
	WattsKnown bool
	// BackedOff marks the moment the miner gave a noticeable slice of CPU back
	// to other work. The chart ticks these so restraint is visible.
	BackedOff bool
}

// Total returns the whole machine's CPU usage at this sample.
func (s Sample) Total() float64 { return s.MinerCPU + s.OtherCPU }

// Ring is a fixed-size circular buffer of samples. It is safe for concurrent
// use: the scheduler appends from its tick loop while the GUI reads.
//
// A ring rather than a growing slice because this runs for weeks at a time —
// the history is bounded by design, and old samples are not worth the memory.
type Ring struct {
	mu   sync.Mutex
	buf  []Sample
	next int  // where the next sample goes
	full bool // whether the buffer has wrapped at least once
}

// NewRing creates a ring holding at most capacity samples. A capacity below 1
// is treated as 1.
func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{buf: make([]Sample, capacity)}
}

// Add appends a sample, discarding the oldest one when full.
func (r *Ring) Add(s Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = s
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
}

// Snapshot returns a copy of the samples, oldest first. The copy means a caller
// can render it without holding a lock or racing the writer.
func (r *Ring) Snapshot() []Sample {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.full {
		out := make([]Sample, r.next)
		copy(out, r.buf[:r.next])
		return out
	}
	out := make([]Sample, 0, len(r.buf))
	out = append(out, r.buf[r.next:]...)
	out = append(out, r.buf[:r.next]...)
	return out
}

// Len returns how many samples are currently held.
func (r *Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return len(r.buf)
	}
	return r.next
}
