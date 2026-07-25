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
	other, _, err := c.Usage(pid)
	return other, err
}

// Usage returns the CPU split between the given PID and everything else, both
// as fractions in [0.0, 1.0] of total capacity. The sample window is 500ms.
//
// The miner's own share is what makes the two series comparable on a chart: it
// is the difference between "the machine is busy" and "the miner is what's
// making it busy".
func (c *CPU) Usage(pid int) (other, miner float64, err error) {
	pcts, err := cpu.Percent(500*time.Millisecond, false)
	if err != nil || len(pcts) == 0 {
		return 0, 0, err
	}
	total := pcts[0] / 100.0

	if pid == 0 {
		return total, 0, nil
	}

	minerPct, err := processCPUFraction(pid)
	if err != nil {
		// If we can't read the miner's CPU (e.g., it just restarted), report
		// the total as "other" — a conservative over-estimate, which is safe
		// because it only makes the scheduler back off sooner.
		return total, 0, nil
	}
	other = total - minerPct
	if other < 0 {
		other = 0
	}
	return other, minerPct, nil
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
