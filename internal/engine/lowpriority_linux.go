//go:build linux

package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var (
	scopeOnce     sync.Once
	scopeProperty string // "" when no scope can be made
)

// scopeSupport probes once whether this session can make an idle-weighted
// user scope, and with which property. It runs a real scope around `true`
// rather than inspecting versions: that answers every way it can fail — no
// systemd-run, no user manager (a system service, an SSH session without
// lingering), a systemd too old for the value — in one step, and costs one
// short process for the life of the app.
func scopeSupport() string {
	scopeOnce.Do(func() {
		// Flatpak can reach systemd only through a sandbox hole Flathub may
		// refuse; there the fallback is SCHED_IDLE, by design.
		if _, err := os.Stat("/.flatpak-info"); err == nil {
			return
		}
		if _, err := exec.LookPath("systemd-run"); err != nil {
			return
		}
		for _, prop := range scopeProperties {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			name, args := scopeArgs(scopeUnitName(os.Getpid()), prop, "true", nil)
			err := exec.CommandContext(ctx, name, args...).Run()
			cancel()
			if err == nil {
				scopeProperty = prop
				return
			}
		}
	})
	return scopeProperty
}

// launchCommand returns the command that starts bin at the lowest priority
// this session offers, and whether it runs in an idle scope.
func launchCommand(bin string, args []string) (string, []string, bool) {
	prop := scopeSupport()
	if prop == "" {
		return bin, args, false
	}
	name, wrapped := scopeArgs(scopeUnitName(os.Getpid()), prop, bin, args)
	return name, wrapped, true
}

// demote applies the per-process and per-thread demotions to pid. It is
// called straight after launch and again once the miner has spawned its
// hashing threads; every setting it makes is idempotent. schedIdle is set
// when no idle scope is doing that job.
//
// Each failure is logged, never returned: an unprivileged setting that does
// not take leaves the miner exactly as it was before this existed, which is
// no reason to refuse to mine.
func demote(pid int, schedIdle bool) {
	if err := os.WriteFile(fmt.Sprintf("/proc/%d/oom_score_adj", pid),
		[]byte(strconv.Itoa(oomScoreMiner)), 0); err != nil {
		log.Printf("xmrig: could not raise its OOM score: %v", err)
	}
	tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return // already exited
	}
	var ioErr, schedErr error
	for _, t := range tasks {
		tid, err := strconv.Atoi(t.Name())
		if err != nil {
			continue
		}
		// ioprio and scheduling policy are per thread. Threads created later
		// inherit both from the thread that creates them, so demoting the
		// ones that exist covers their descendants too.
		if _, _, errno := unix.Syscall(unix.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(tid), ioprioIdle); errno != 0 && errno != unix.ESRCH {
			ioErr = errno
		}
		if schedIdle {
			attr := &unix.SchedAttr{Size: unix.SizeofSchedAttr, Policy: unix.SCHED_IDLE}
			if err := unix.SchedSetAttr(tid, attr, 0); err != nil && !errors.Is(err, unix.ESRCH) {
				schedErr = err
			}
		}
	}
	if ioErr != nil {
		log.Printf("xmrig: could not set idle I/O priority: %v", ioErr)
	}
	if schedErr != nil {
		log.Printf("xmrig: could not set SCHED_IDLE: %v", schedErr)
	}
}

// awaitExec waits until pid is running bin — systemd-run having registered
// its scope and exec'd the miner in place — or reports why it never did.
func awaitExec(pid int, bin string, timeout time.Duration) error {
	want, err := os.Stat(bin)
	if err != nil {
		return err
	}
	exe := fmt.Sprintf("/proc/%d/exe", pid)
	deadline := time.Now().Add(timeout)
	for {
		got, err := os.Stat(exe)
		if err != nil {
			// Gone, or a zombie: systemd-run gave up, and its reason is on the
			// miner's output, which readOutput never gets to see.
			return errors.New("systemd-run exited before starting the miner")
		}
		if os.SameFile(got, want) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("systemd-run did not start the miner within %s", timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ioprioWhoProcess is IOPRIO_WHO_PROCESS: the target is one thread by ID.
const ioprioWhoProcess = 1

// scopeIsIdle reports whether pid's cgroup actually carries the idle weight.
// systemd-run succeeding is not proof: without the cpu controller delegated
// to the user manager the property is accepted and has no effect.
func scopeIsIdle(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return false
	}
	path, ok := unifiedCgroup(string(b))
	if !ok {
		return false
	}
	dir := filepath.Join("/sys/fs/cgroup", path)
	idle, _ := os.ReadFile(filepath.Join(dir, "cpu.idle"))
	weight, _ := os.ReadFile(filepath.Join(dir, "cpu.weight"))
	return cgroupIsIdle(string(idle), string(weight))
}

// settleAfter is how long after launch the second demotion pass runs: long
// enough for xmrig to have allocated its RandomX dataset and started its
// hashing threads.
const settleAfter = 15 * time.Second

// settle runs the second pass: demote again, and confirm the scope took. A
// scope that did not is replaced by SCHED_IDLE, so the miner is never left
// less demoted than a session without systemd would make it.
func settle(pid int, scoped bool, stop <-chan struct{}) {
	select {
	case <-stop:
		return
	case <-time.After(settleAfter):
	}
	schedIdle := !scoped
	if scoped && !scopeIsIdle(pid) {
		log.Printf("xmrig: its systemd scope has no idle CPU weight (is the cpu controller delegated?); using SCHED_IDLE")
		schedIdle = true
	}
	demote(pid, schedIdle)
}

// describeLowPriority is the one log line saying how the miner was demoted.
func describeLowPriority(scoped bool) string {
	if scoped {
		return "idle CPU scope (" + scopeProperty + "), OOM score 1000, idle I/O"
	}
	return "SCHED_IDLE, OOM score 1000, idle I/O"
}
