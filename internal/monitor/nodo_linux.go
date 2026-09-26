//go:build linux

package monitor

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func readMemory() (total, available int64, ok bool) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	return parseMemInfo(string(b))
}

// readCoreCapacities prefers cpu_capacity, the scheduler's own relative
// ranking on arm64. Where the kernel does not export it, the maximum frequency
// ranks the clusters the same way on every big.LITTLE part shipped so far.
func readCoreCapacities() (map[int]int64, bool) {
	dirs, err := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*")
	if err != nil || len(dirs) == 0 {
		return nil, false
	}
	caps := make(map[int]int64, len(dirs))
	for _, dir := range dirs {
		cpu, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(dir), "cpu"))
		if err != nil {
			continue
		}
		c, ok := readInt(dir + "/cpu_capacity")
		if !ok {
			c, ok = readInt(dir + "/cpufreq/cpuinfo_max_freq")
		}
		if !ok {
			// One unreadable core would make it look like a cluster of its own.
			return nil, false
		}
		caps[cpu] = c
	}
	return caps, len(caps) > 0
}

func readInt(path string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(readFileString(path)), 10, 64)
	return n, err == nil
}

// systemSlice is where systemd places system services on the cgroup v2
// hierarchy, which is what Nodo's Debian base mounts.
const systemSlice = "/sys/fs/cgroup/system.slice"

func (p *ServicePressure) sample(now time.Time) (pressured, known bool) {
	dir := filepath.Join(systemSlice, p.unit)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return false, true
	}
	// cpu.pressure exists but fails to read when the kernel booted with psi=0,
	// which is exactly when the cpu.stat fallback is needed.
	if b, err := os.ReadFile(dir + "/cpu.pressure"); err == nil {
		if avg, ok := psiSomeAvg10(string(b)); ok {
			return avg > pressureLimit, true
		}
	}
	b, err := os.ReadFile(dir + "/cpu.stat")
	if err != nil {
		return false, false
	}
	usage, ok := cpuStatUsage(string(b))
	if !ok {
		return false, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	prev, prevAt := p.lastUsage, p.lastAt
	p.lastUsage, p.lastAt = usage, now
	if prevAt.IsZero() {
		return false, false
	}
	return busyCores(prev, usage, now.Sub(prevAt)) > busyCoresLimit, true
}
