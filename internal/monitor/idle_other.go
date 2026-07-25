//go:build !linux && !freebsd && !openbsd && !netbsd && !windows && !(darwin && cgo)

package monitor

import "log"

// probeIdleBackend has no implementation here. The headless (CGO_ENABLED=0)
// macOS build lands in this file too: idle detection needs the window server,
// which a headless build has no reason to talk to.
func probeIdleBackend() idleBackend {
	log.Printf("idle detection: not supported on this build; mining will not wait for you to go idle")
	return nil
}
