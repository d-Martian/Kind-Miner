package monitor

import (
	"sync"
	"time"
)

// Idle reports how long the user has been away from the keyboard and mouse.
//
// This is a different question from "is the CPU busy", which the CPU monitor
// answers. A machine can be completely idle while a background job pins every
// core, and it can be under a person's active attention while barely using any
// CPU. kind-miner uses both: CPU load decides how hard to throttle, user idle
// decides whether to go to full speed at all.
type Idle struct {
	mu      sync.Mutex
	backend idleBackend
	probed  bool
}

// idleBackend is one platform mechanism for reading the user's idle time.
type idleBackend interface {
	// idleTime returns the duration since the last keyboard/mouse input.
	idleTime() (time.Duration, error)
	name() string
}

// NewIdle returns an idle monitor. Backend selection is deferred to the first
// IdleTime call: on Linux it involves D-Bus round trips, and the session may not
// be ready when the app first starts.
func NewIdle() *Idle { return &Idle{} }

// IdleTime returns the time since the last user input. ok is false when no
// backend works on this system — an SSH session, a headless server, or a
// Wayland compositor with no supported idle interface. Callers must treat
// ok == false as "unknown", never as "idle".
func (i *Idle) IdleTime() (time.Duration, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if !i.probed {
		i.backend = probeIdleBackend()
		i.probed = true
	}
	if i.backend == nil {
		return 0, false
	}
	d, err := i.backend.idleTime()
	if err != nil {
		// A backend that worked once can stop working — the session bus goes
		// away on logout, the X display disappears on a session switch. Drop it
		// and re-probe next time rather than reporting a stale value.
		i.backend = nil
		i.probed = false
		return 0, false
	}
	return d, true
}

// Backend reports which mechanism is in use, for diagnostics. Empty when none
// is available.
func (i *Idle) Backend() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.backend == nil {
		return ""
	}
	return i.backend.name()
}
