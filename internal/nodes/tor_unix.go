//go:build linux || darwin

package nodes

import (
	"fmt"
	"os/exec"
	"runtime"
)

// tryStartTor attempts to start the Tor service using the platform service
// manager. It does not block; the caller polls for SOCKS5 availability.
func tryStartTor() bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		// Try systemctl first (systemd), then service (SysV/OpenRC).
		if path, err := exec.LookPath("systemctl"); err == nil {
			cmd = exec.Command(path, "start", "tor")
		} else if path, err := exec.LookPath("service"); err == nil {
			cmd = exec.Command(path, "tor", "start")
		} else {
			return false
		}
	case "darwin":
		// Homebrew installs Tor as a brew service.
		if path, err := exec.LookPath("brew"); err == nil {
			cmd = exec.Command(path, "services", "start", "tor")
		} else {
			return false
		}
	default:
		return false
	}

	if err := cmd.Run(); err != nil {
		// Non-fatal: might just need sudo. Log and let caller handle.
		fmt.Printf("kind-miner: could not auto-start Tor (%v) — start it manually\n", err)
		return false
	}
	fmt.Println("kind-miner: started Tor service automatically.")
	return true
}
