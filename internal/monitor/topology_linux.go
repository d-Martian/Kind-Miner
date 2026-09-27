//go:build linux

package monitor

import (
	"path/filepath"
	"strconv"
	"strings"
)

func readTopology() ([]CPUInfo, bool) {
	dirs, err := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*")
	if err != nil || len(dirs) == 0 {
		return nil, false
	}
	// Hybrid Intel parts list their efficiency cores here; elsewhere the file
	// does not exist and every core is the same kind.
	atoms := map[int]bool{}
	for _, c := range parseCPUList(readFileString("/sys/devices/cpu_atom/cpus")) {
		atoms[c] = true
	}
	var cpus []CPUInfo
	for _, dir := range dirs {
		id, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(dir), "cpu"))
		if err != nil {
			continue
		}
		siblings := parseCPUList(readFileString(dir + "/topology/thread_siblings_list"))
		if len(siblings) == 0 {
			// Offline CPUs have no topology; they cannot run a thread anyway.
			continue
		}
		freq, _ := strconv.ParseInt(strings.TrimSpace(readFileString(dir+"/cpufreq/cpuinfo_max_freq")), 10, 64)
		cpus = append(cpus, CPUInfo{
			ID:         id,
			Siblings:   siblings,
			Efficiency: atoms[id],
			HasL3:      hasL3(dir),
			MaxFreq:    freq,
		})
	}
	return cpus, len(cpus) > 0
}

// hasL3 reports whether a level-3 cache lists this CPU among its users.
func hasL3(dir string) bool {
	nodes, _ := filepath.Glob(dir + "/cache/index[0-9]*")
	for _, n := range nodes {
		if strings.TrimSpace(readFileString(n+"/level")) == "3" {
			return strings.TrimSpace(readFileString(n+"/shared_cpu_list")) != ""
		}
	}
	return false
}
