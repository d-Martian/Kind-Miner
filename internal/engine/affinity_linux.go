//go:build linux

package engine

import (
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// setAffinity applies the CPU set to every task of pid, not just the process.
// sched_setaffinity acts on one thread, and xmrig pins its workers itself at
// start-up, so setting the process alone would leave every hashing thread
// where it was.
func setAffinity(pid int, cpus []int) error {
	var set unix.CPUSet
	for _, c := range cpus {
		set.Set(c)
	}
	tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return err
	}
	var firstErr error
	for _, t := range tasks {
		tid, err := strconv.Atoi(t.Name())
		if err != nil {
			continue
		}
		// A thread can exit between the listing and the call; that is not a
		// failure worth reporting.
		if err := unix.SchedSetaffinity(tid, &set); err != nil && err != unix.ESRCH && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
