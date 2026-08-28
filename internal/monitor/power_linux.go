//go:build linux

package monitor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// raplGlob matches the top-level powercap zones — one per CPU package. The
// nested zones (intel-rapl:0:0 and friends) break the same energy down by
// domain and would double-count if summed alongside their parent, so the
// pattern deliberately stops at one colon.
const raplGlob = "/sys/class/powercap/intel-rapl:[0-9]*"

// readEnergyMicrojoules sums the cumulative energy counters across CPU
// packages, and returns the combined wrap point.
//
// ok is false whenever no zone can be read. That is the common case on a
// desktop: since CVE-2020-8694 the kernel ships energy_uj as root-only, because
// the counter is precise enough to infer what other processes are computing.
// kind-miner does not ask for privileges to read it.
func readEnergyMicrojoules() (energy, wrapAt uint64, ok bool) {
	zones, err := filepath.Glob(raplGlob)
	if err != nil || len(zones) == 0 {
		return 0, 0, false
	}
	for _, zone := range zones {
		e, err := readUint(filepath.Join(zone, "energy_uj"))
		if err != nil {
			continue
		}
		energy += e
		ok = true
		// A missing range is survivable — Watts re-baselines across a wrap
		// instead of reporting a nonsense spike.
		if max, err := readUint(filepath.Join(zone, "max_energy_range_uj")); err == nil {
			wrapAt += max
		}
	}
	return energy, wrapAt, ok
}

func readUint(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}
