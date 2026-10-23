//go:build linux

package engine

import (
	"os/exec"
	"syscall"
)

// dieWithUs has the kernel kill cmd's process when kind-miner dies, however it
// dies. Shutdown stops every engine on a clean exit, but a crash, a kill -9 or
// the OOM killer runs no Go code at all, and an engine then lives on without
// us. A p2pool left like that keeps its ports and every later start dies on
// EADDRINUSE until someone finds it; this was a real observed failure. A
// suspended xmrig left like that keeps its 2.3 GB dataset indefinitely.
//
// SIGKILL because the miner may be stopped mid duty cycle when we die, and a
// stopped process acts on no other signal; it is also what every engine's Stop
// sends. The signal follows the thread that started the process, not the
// process. That thread can only go first if its goroutine exits while locked
// to it, and no goroutine that starts an engine does.
func dieWithUs(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
