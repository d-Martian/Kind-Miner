package monitor

import (
	"strconv"
	"strings"
)

// MemorySample is one reading of how short the machine is of memory.
type MemorySample struct {
	// FullAvg10 is the share of the last ten seconds, in percent, in which
	// every runnable task was stalled waiting for memory — the kernel's own
	// measure of thrashing. PSIKnown is false where /proc/pressure is absent
	// (kernels without CONFIG_PSI, or booted with psi=0).
	FullAvg10 float64
	PSIKnown  bool
	// Total and Available are MemTotal and MemAvailable in bytes; MemKnown is
	// false where they cannot be read.
	Total, Available int64
	MemKnown         bool
}

// Memory pressure is read the way the plan's Fedora box needs it read: that
// machine hard-freezes under memory exhaustion instead of letting the OOM
// killer act, so by the time anything is killed it is too late. The pressure
// stall figure rises minutes earlier, while there is still time to give 2 GB
// back.

// SampleMemory reads memory pressure and headroom. Each half degrades on its
// own: a machine without PSI still reports MemAvailable, and vice versa.
func SampleMemory() MemorySample {
	var s MemorySample
	s.FullAvg10, s.PSIKnown = readMemoryPSI()
	s.Total, s.Available, s.MemKnown = Memory()
	return s
}

// psiAvg10 reads the avg10 figure of one line ("some" or "full") of a
// /proc/pressure file.
func psiAvg10(text, kind string) (float64, bool) {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != kind {
			continue
		}
		for _, f := range fields[1:] {
			if v, ok := strings.CutPrefix(f, "avg10="); ok {
				n, err := strconv.ParseFloat(v, 64)
				return n, err == nil
			}
		}
	}
	return 0, false
}
