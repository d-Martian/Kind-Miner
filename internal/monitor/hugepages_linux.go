//go:build linux

package monitor

import "os"

// readHugePages parses the huge page fields out of /proc/meminfo.
//
// HugePages_Total and HugePages_Free are counts of pages; Hugepagesize is in
// kB, like every other size in that file. All three must be present for the
// answer to mean anything.
func readHugePages() (total, free, pageSize int64, ok bool) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, false
	}
	return parseHugePages(string(b))
}
