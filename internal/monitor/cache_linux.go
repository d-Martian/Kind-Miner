//go:build linux

package monitor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// cacheGlob matches every cache node the kernel exports, across all CPUs. Each
// physical cache appears once per CPU that can see it, so entries have to be
// de-duplicated before their sizes are summed.
const cacheGlob = "/sys/devices/system/cpu/cpu[0-9]*/cache/index[0-9]*"

// readL3CacheBytes sums the distinct level-3 caches on the machine.
//
// The de-duplication key is shared_cpu_map: a 24 MiB L3 shared by twenty cores
// is listed twenty times, and summing naively would report 480 MiB and uncap
// the thread count entirely — the exact failure this function exists to
// prevent. Summing across *distinct* maps is still right on the multi-socket
// and chiplet parts where there genuinely is more than one L3.
//
// Not every CPU has an L3 node: on hybrid parts the low-power E-cores sit
// outside the ring and export none, which is why this walks all CPUs rather
// than trusting cpu0 to describe the machine.
func readL3CacheBytes() (int64, bool) {
	nodes, err := filepath.Glob(cacheGlob)
	if err != nil || len(nodes) == 0 {
		return 0, false
	}
	var total int64
	seen := make(map[string]bool)
	for _, node := range nodes {
		if strings.TrimSpace(readFileString(node+"/level")) != "3" {
			continue
		}
		key := strings.TrimSpace(readFileString(node + "/shared_cpu_map"))
		// An unreadable map would collapse every cache onto one key and
		// under-count; skipping is the safer direction, since a low total only
		// costs threads while a high one costs hashrate.
		if key == "" || seen[key] {
			continue
		}
		size, ok := parseCacheSize(readFileString(node + "/size"))
		if !ok {
			continue
		}
		seen[key] = true
		total += size
	}
	if total <= 0 {
		return 0, false
	}
	return total, true
}

func readFileString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// parseCacheSize reads the kernel's cache size format — a decimal count with an
// optional binary unit suffix, e.g. "24576K".
func parseCacheSize(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	mult := int64(1)
	switch s[len(s)-1] {
	case 'K', 'k':
		mult, s = 1<<10, s[:len(s)-1]
	case 'M', 'm':
		mult, s = 1<<20, s[:len(s)-1]
	case 'G', 'g':
		mult, s = 1<<30, s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n * mult, true
}
