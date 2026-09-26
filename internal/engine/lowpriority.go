package engine

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// The duty cycle decides how much CPU the miner gets. The kernel layer decides
// who wins in the moments between: a burst of work that arrives mid-slice, a
// page of memory both want, a disk both are reading. Nice 19 alone still takes
// a real share of a busy core from ordinary processes, so on Linux the miner
// is demoted three more ways:
//
//   - CPU: an idle-weighted systemd scope (cpu.idle=1), so any other task in
//     the same slice — the browser, the editor — preempts it outright. Where
//     no scope can be made (Flatpak, no user manager) every thread is put on
//     SCHED_IDLE instead, which is the same idea one level down.
//   - Memory: OOM score 1000, so if the machine runs out the miner is the
//     first thing the kernel kills rather than whatever the user is working in.
//     Raising one's own score needs no privilege.
//   - I/O: the idle class, served only when nothing else wants the disk.
//
// None of it changes the hashrate on an idle machine; it only decides who
// yields when the machine is not idle, which is the whole product promise.

// scopeProperties are tried in order: CPUWeight=idle sets cpu.idle on
// systemd 250 and later; older systemd rejects the value, and weight 1 is the
// nearest it can express.
var scopeProperties = []string{"CPUWeight=idle", "CPUWeight=1"}

// scopeSeq makes each scope's unit name unique. systemd refuses to start a
// scope whose name is still loaded, and a restarted miner can race the
// collection of the previous one.
var scopeSeq atomic.Uint64

// scopeArgs wraps a command in a transient user scope. systemd-run --scope
// registers the scope for its own PID and then execs the command in place, so
// the PID the caller sees is the miner's own — which the duty cycle, the
// affinity and the CPU accounting all rely on.
func scopeArgs(unit, property, bin string, args []string) (string, []string) {
	wrapped := []string{"--user", "--scope", "--collect", "--quiet",
		"--unit=" + unit, "-p", property, "--", bin}
	return "systemd-run", append(wrapped, args...)
}

func scopeUnitName(ownPID int) string {
	return fmt.Sprintf("kind-miner-xmrig-%d-%d", ownPID, scopeSeq.Add(1))
}

// unifiedCgroup returns the cgroup v2 path from /proc/<pid>/cgroup text: the
// "0::" line. Hybrid hosts list v1 controllers too (net_cls on Fedora), and
// those paths say nothing about CPU weight.
func unifiedCgroup(procCgroup string) (string, bool) {
	for _, line := range strings.Split(procCgroup, "\n") {
		if path, ok := strings.CutPrefix(line, "0::"); ok && path != "" {
			return path, true
		}
	}
	return "", false
}

// cgroupIsIdle reports whether a cgroup's cpu.idle / cpu.weight files show the
// demotion took. Either counts: cpu.idle on current systemd, weight 1 as the
// fallback on older.
func cgroupIsIdle(cpuIdle, cpuWeight string) bool {
	return strings.TrimSpace(cpuIdle) == "1" || strings.TrimSpace(cpuWeight) == "1"
}

// ioprioIdle is the I/O priority value for the idle class: IOPRIO_CLASS_IDLE
// (3) shifted into the class bits (IOPRIO_CLASS_SHIFT, 13). The idle class has
// no levels.
const ioprioIdle = 3 << 13

// oomScoreMiner is the highest OOM score adjustment: first to be killed.
const oomScoreMiner = 1000
