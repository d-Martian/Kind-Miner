//go:build !linux

package engine

import "os/exec"

// Outside Linux there is no parent-death signal; engines are stopped by
// Shutdown alone. (Windows could get the same from a kill-on-close job object.)
func dieWithUs(*exec.Cmd) {}
