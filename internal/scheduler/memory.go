package scheduler

import (
	"log"
	"time"

	"github.com/kind-miner/kind-miner/internal/monitor"
)

// Memory is the one resource the duty cycle cannot give back. A suspended
// miner still holds its RandomX dataset — about 2.3 GB — so when the machine
// runs short, SIGSTOP frees nothing, and on a desktop that thrashes rather than
// letting the OOM killer act, the next few minutes are a frozen screen. So
// memory pressure is the one signal that stops the miner outright: the process
// ends and its memory goes back. It is the exception to "throttle by duty,
// never by restart", and restarting is kept rare to match — a long quiet period
// first, and only if the machine has room for the miner again, or the restart
// would put the machine straight back where it was.

const (
	// memoryStallLimit is the "full" PSI figure, in percent of the last ten
	// seconds with every task stalled on memory, above which the machine is
	// thrashing rather than merely busy.
	memoryStallLimit = 5.0
	// memoryStallTicks is how many ticks in a row it must hold: six seconds,
	// long enough to skip a burst from a large allocation, short enough to act
	// before a desktop that hard-freezes has frozen.
	memoryStallTicks = 3
	// memoryFloor is the smallest MemAvailable the guard accepts; the margin
	// is this or memoryFloorShare of RAM, whichever is larger.
	memoryFloor      = 512 << 20
	memoryFloorShare = 0.05
	// memoryQuiet is how long the machine must stay calm before the miner
	// comes back. Restarting costs RandomX its dataset again and takes 2 GB
	// in one go; doing that the moment pressure dips would be the oscillation
	// the rest of the scheduler exists to avoid.
	memoryQuiet = 10 * time.Minute
	// memoryQuietStall is the stall figure that counts as calm.
	memoryQuietStall = 1.0
)

// memoryGuard decides when to stop the miner for memory and when it may come
// back. It is a value with a step function so the policy is testable without
// a machine to exhaust.
type memoryGuard struct {
	stopped    bool
	stallTicks int
	calmSince  time.Time
	// footprint is what the miner takes when it starts: its RandomX dataset
	// and scratchpads.
	footprint int64
}

// margin is the MemAvailable the guard keeps free.
func margin(total int64) int64 {
	m := int64(float64(total) * memoryFloorShare)
	if m < memoryFloor {
		m = memoryFloor
	}
	return m
}

// step takes one reading and returns whether the miner should be stopped now.
// A reading that knows nothing changes nothing: without PSI and MemAvailable
// there is no evidence either way.
func (g *memoryGuard) step(m monitor.MemorySample, now time.Time) bool {
	short := m.MemKnown && m.Available < margin(m.Total)
	stalled := m.PSIKnown && m.FullAvg10 >= memoryStallLimit
	if stalled {
		g.stallTicks++
	} else {
		g.stallTicks = 0
	}

	if !g.stopped {
		if short || g.stallTicks >= memoryStallTicks {
			g.stopped = true
			g.calmSince = time.Time{}
		}
		return g.stopped
	}

	// Stopped: wait for a long calm with room to spare for the miner itself.
	calm := !stalled && (!m.PSIKnown || m.FullAvg10 < memoryQuietStall)
	roomy := !m.MemKnown || m.Available >= margin(m.Total)+g.footprint
	if !calm || !roomy {
		g.calmSince = time.Time{}
		return true
	}
	if g.calmSince.IsZero() {
		g.calmSince = now
	}
	if now.Sub(g.calmSince) >= memoryQuiet {
		g.stopped = false
		g.calmSince = time.Time{}
	}
	return g.stopped
}

// starter is implemented by engines that can be started again after Stop. It
// is deliberately separate from miner: the scheduler restarts the miner only
// after a memory stop, never to throttle it.
type starter interface {
	Start() error
}

// stopOrRestartForMemory acts on the guard's decision when it changes: ends
// the miner's process when memory runs short, and starts it again once the
// guard allows. Between the two the decide step keeps the allowance at zero.
func (s *Scheduler) stopOrRestartForMemory(short bool) {
	switch {
	case short && !s.memStopped:
		s.memStopped = true
		log.Printf("Memory is short: stopping the miner to give back its RandomX dataset")
		s.xmrig.Stop()
	case !short && s.memStopped:
		s.memStopped = false
		st, ok := s.xmrig.(starter)
		if !ok {
			return
		}
		log.Printf("Memory has been calm for %s with room to spare: starting the miner again", memoryQuiet)
		if err := st.Start(); err != nil {
			log.Printf("could not restart the miner: %v", err)
		}
	}
}
