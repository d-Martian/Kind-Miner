//go:build linux

package engine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEnginesDieWithKindMiner(t *testing.T) {
	if os.Getenv("KM_TEST_DYING_PARENT") == "1" {
		// The stand-in for kind-miner: start a child the way every engine
		// does, say which, and die without stopping it, as a crash would.
		cmd := exec.Command("sleep", "60")
		dieWithUs(cmd)
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println(cmd.Process.Pid)
		os.Exit(0)
	}

	parent := exec.Command(os.Args[0], "-test.run=^TestEnginesDieWithKindMiner$")
	parent.Env = append(os.Environ(), "KM_TEST_DYING_PARENT=1")
	out, err := parent.Output()
	if err != nil {
		t.Fatalf("stand-in parent: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("stand-in parent printed %q", out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !gone(pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatal("the engine outlived kind-miner")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// gone reports whether pid has exited: no longer there, or a zombie waiting
// for its new parent to reap it.
func gone(pid int) bool {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	// The state follows the command name, which is in parentheses and may
	// contain them itself.
	i := bytes.LastIndexByte(stat, ')')
	return i < 0 || i+2 >= len(stat) || stat[i+2] == 'Z'
}
