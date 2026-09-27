package monitor

import (
	"sort"
	"strconv"
	"strings"
)

// CPUInfo is what the thread layout needs to know about one logical CPU.
type CPUInfo struct {
	ID int
	// Siblings are the logical CPUs sharing this one's physical core (SMT),
	// itself included. RandomX gains little from a second hyperthread on a
	// core, and the second one competes for the same L2, so layouts use one
	// thread per physical core.
	Siblings []int
	// Efficiency marks a hybrid part's efficiency cores (Intel's "atom"
	// cores). False on parts with one kind of core.
	Efficiency bool
	// HasL3 is false for cores outside the shared last-level cache — the
	// low-power E-cores on Meteor Lake, for one. A RandomX thread there has
	// no cache for its scratchpad and slows every other thread too.
	HasL3 bool
	// MaxFreq ranks cores within a kind, in kHz; 0 when unknown.
	MaxFreq int64
}

// Topology reads this machine's CPUs. ok is false where it cannot, in which
// case callers keep the thread layout they had before topology existed.
func Topology() ([]CPUInfo, bool) { return readTopology() }

// parseCPUList reads the kernel's CPU list format: "0-3,8,10-11".
func parseCPUList(s string) []int {
	var out []int
	for _, part := range strings.Split(strings.TrimSpace(s), ",") {
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			continue
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(hi); err != nil {
				continue
			}
		}
		for i := a; i <= b; i++ {
			out = append(out, i)
		}
	}
	sort.Ints(out)
	return out
}
