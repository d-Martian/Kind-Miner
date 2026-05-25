//go:build !windows

package engine

import (
	"os"
	"syscall"
)

func setPriority(p *os.Process) error {
	return syscall.Setpriority(syscall.PRIO_PROCESS, p.Pid, 19)
}

func suspendProcess(p *os.Process) error {
	return p.Signal(syscall.SIGSTOP)
}

func resumeProcess(p *os.Process) error {
	return p.Signal(syscall.SIGCONT)
}
