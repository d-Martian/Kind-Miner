//go:build !linux

package monitor

func readMemoryPSI() (float64, bool) { return 0, false }
