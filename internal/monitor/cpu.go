// Package monitor provides lightweight system health samplers.
package monitor

import (
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/process"
)

// CPU samples CPU utilization excluding a specific PID so the scheduler can
// measure "other processes" load without counting the miner itself.
type CPU struct{}

// NewCPU returns a CPU monitor.
func NewCPU() *CPU { return &CPU{} }

// OtherUsage returns the fraction of CPU used by processes OTHER than the
// given PID. The sample window is 500ms. Returns a value in [0.0, 1.0].
// If pid is 0, it returns total CPU usage.
func (c *CPU) OtherUsage(pid int) (float64, error) {
	pcts, err := cpu.Percent(500*time.Millisecond, false)
	if err != nil || len(pcts) == 0 {
		return 0, err
	}
	total := pcts[0] / 100.0

	if pid == 0 {
		return total, nil
	}

	// Subtract the miner's own CPU share.
	minerPct, err := processCPUFraction(pid)
	if err != nil {
		// If we can't read the miner's CPU (e.g., it just restarted), return
		// total — it's a conservative over-estimate which is safe.
		return total, nil
	}
	other := total - minerPct
	if other < 0 {
		other = 0
	}
	return other, nil
}

func processCPUFraction(pid int) (float64, error) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return 0, err
	}
	pct, err := p.CPUPercent()
	if err != nil {
		return 0, err
	}
	// gopsutil CPUPercent returns a value like 350.0 for 3.5 cores on a
	// multi-core system (total across cores). Divide by 100 to normalise.
	numCPU, _ := cpu.Counts(true)
	if numCPU == 0 {
		numCPU = 1
	}
	return (pct / 100.0) / float64(numCPU), nil
}
