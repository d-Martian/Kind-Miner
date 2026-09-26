//go:build linux

package engine

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func startSleeper(t *testing.T, name string, args []string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

func TestDemoteLandsOnEveryCount(t *testing.T) {
	cmd := startSleeper(t, "sleep", []string{"30"})
	pid := cmd.Process.Pid
	demote(pid, true)

	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/oom_score_adj")
	if err != nil || strings.TrimSpace(string(b)) != "1000" {
		t.Errorf("oom_score_adj = %q (%v), want 1000", b, err)
	}
	prio, _, errno := unix.Syscall(unix.SYS_IOPRIO_GET, ioprioWhoProcess, uintptr(pid), 0)
	if errno != 0 || prio != ioprioIdle {
		t.Errorf("ioprio = %#x (%v), want the idle class %#x", prio, errno, ioprioIdle)
	}
	attr, err := unix.SchedGetAttr(pid, 0)
	if err != nil || attr.Policy != unix.SCHED_IDLE {
		t.Errorf("scheduling policy = %+v (%v), want SCHED_IDLE", attr, err)
	}
}

func TestDemoteLeavesTheSchedulerToAScope(t *testing.T) {
	cmd := startSleeper(t, "sleep", []string{"30"})
	demote(cmd.Process.Pid, false)
	if attr, err := unix.SchedGetAttr(cmd.Process.Pid, 0); err != nil || attr.Policy == unix.SCHED_IDLE {
		t.Errorf("policy = %+v (%v); with an idle scope doing the job, the threads keep theirs", attr, err)
	}
}

func TestDemoteOfAGoneProcessIsQuiet(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	demote(cmd.Process.Pid, true) // must not panic or hang
}

// This one needs a real user session with systemd; CI and containers skip it.
func TestLaunchInAnIdleScopeKeepsThePID(t *testing.T) {
	if scopeSupport() == "" {
		t.Skip("no systemd user manager that can make an idle scope here")
	}
	name, args, scoped := launchCommand("sleep", []string{"30"})
	if !scoped || name != "systemd-run" {
		t.Fatalf("launchCommand = %q scoped=%v, want a systemd-run scope", name, scoped)
	}
	cmd := startSleeper(t, name, args)
	pid := cmd.Process.Pid

	// systemd-run execs sleep in place once the scope is registered.
	deadline := time.Now().Add(5 * time.Second)
	for {
		comm, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
		if strings.TrimSpace(string(comm)) == "sleep" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("PID %d never became the miner (comm %q); SIGSTOP and affinity would hit systemd-run", pid, comm)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !scopeIsIdle(pid) {
		t.Error("the scope exists but its cgroup is not idle-weighted")
	}
}

func TestAwaitExecFollowsSystemdRunIntoTheMiner(t *testing.T) {
	if scopeSupport() == "" {
		t.Skip("no systemd user manager that can make an idle scope here")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	name, args, _ := launchCommand(sleep, []string{"30"})
	cmd := startSleeper(t, name, args)
	if err := awaitExec(cmd.Process.Pid, sleep, 5*time.Second); err != nil {
		t.Fatalf("awaitExec: %v", err)
	}
	// Only now is it safe to SIGSTOP: stopping systemd-run before this froze
	// it mid-request, and the miner never started.
	if comm, _ := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/comm"); strings.TrimSpace(string(comm)) != "sleep" {
		t.Errorf("awaitExec returned while the PID was still %q", comm)
	}
}

func TestAwaitExecGivesUpWhenTheLauncherExits(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	// Stands in for a systemd-run that failed: it exits without ever
	// becoming the miner.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	start := time.Now()
	if err := awaitExec(cmd.Process.Pid, sleep, 5*time.Second); err == nil {
		t.Fatal("awaitExec succeeded for a process that exited")
	}
	if time.Since(start) > time.Second {
		t.Errorf("took %s to notice the launcher had exited; want no wait for the timeout", time.Since(start))
	}
}
