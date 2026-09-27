//go:build !linux

package monitor

func readTopology() ([]CPUInfo, bool) { return nil, false }
