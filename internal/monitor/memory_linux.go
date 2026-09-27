//go:build linux

package monitor

import "os"

func readMemoryPSI() (float64, bool) {
	b, err := os.ReadFile("/proc/pressure/memory")
	if err != nil {
		return 0, false
	}
	return psiAvg10(string(b), "full")
}
