package scheduler

import (
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/kindness"
	"github.com/kind-miner/kind-miner/internal/monitor"
)

const gib = int64(1) << 30

// ram describes a 16 GiB machine with avail free and a full-stall figure.
func ram(avail int64, stall float64) monitor.MemorySample {
	return monitor.MemorySample{Total: 16 * gib, Available: avail, MemKnown: true, FullAvg10: stall, PSIKnown: true}
}

func TestMemoryGuardStops(t *testing.T) {
	now := time.Now()
	t.Run("sustained thrashing stops the miner", func(t *testing.T) {
		g := memoryGuard{footprint: 2300 << 20}
		for i := 1; i < memoryStallTicks; i++ {
			if g.step(ram(6*gib, 12), now) {
				t.Fatalf("stopped after %d stalled ticks, want %d", i, memoryStallTicks)
			}
		}
		if !g.step(ram(6*gib, 12), now) {
			t.Error("did not stop after sustained stalls")
		}
	})
	t.Run("a single burst does not", func(t *testing.T) {
		g := memoryGuard{}
		g.step(ram(6*gib, 40), now)
		g.step(ram(6*gib, 0), now)
		if g.step(ram(6*gib, 40), now) {
			t.Error("stopped on two separate bursts")
		}
	})
	t.Run("running out of headroom stops at once", func(t *testing.T) {
		g := memoryGuard{}
		// 5% of 16 GiB is 819 MiB, above the 512 MiB floor.
		if !g.step(ram(700<<20, 0), now) {
			t.Error("did not stop with 700 MiB free on a 16 GiB machine")
		}
	})
	t.Run("knowing nothing changes nothing", func(t *testing.T) {
		g := memoryGuard{}
		for i := 0; i < 10; i++ {
			if g.step(monitor.MemorySample{}, now) {
				t.Fatal("stopped without any reading")
			}
		}
	})
}

func TestMemoryGuardRestartsOnlyAfterALongCalmWithRoom(t *testing.T) {
	start := time.Now()
	footprint := int64(2300 << 20)
	stopped := func() *memoryGuard {
		g := &memoryGuard{footprint: footprint}
		g.step(ram(300<<20, 30), start)
		return g
	}

	g := stopped()
	if !g.step(ram(8*gib, 0), start.Add(time.Minute)) {
		t.Fatal("restarted the moment pressure eased")
	}
	if g.step(ram(8*gib, 0), start.Add(time.Minute+memoryQuiet)) {
		t.Error("still stopped after a full quiet period with room")
	}

	g = stopped()
	g.step(ram(8*gib, 0), start.Add(time.Minute))
	g.step(ram(8*gib, 3), start.Add(6*time.Minute)) // a relapse resets the wait
	if !g.step(ram(8*gib, 0), start.Add(12*time.Minute)) {
		t.Error("restarted though the calm was broken halfway")
	}

	// Calm, but not enough free for the miner's 2.3 GB on top of the margin:
	// restarting would put the machine straight back under pressure.
	g = stopped()
	tight := margin(16*gib) + footprint - 100<<20
	g.step(ram(tight, 0), start.Add(time.Minute))
	if !g.step(ram(tight, 0), start.Add(time.Hour)) {
		t.Error("restarted without room for its own dataset")
	}
}

// restartableMiner records Stop and Start, standing in for XMRig.
type restartableMiner struct {
	stubMiner
	stops, starts int
}

func (m *restartableMiner) Stop()        { m.stops++ }
func (m *restartableMiner) Start() error { m.starts++; return nil }

func TestMemoryStopAndRestartTheProcessOnce(t *testing.T) {
	m := &restartableMiner{}
	s := &Scheduler{xmrig: m}
	s.stopOrRestartForMemory(true)
	s.stopOrRestartForMemory(true)
	if m.stops != 1 {
		t.Errorf("stopped %d times while short, want once", m.stops)
	}
	s.stopOrRestartForMemory(false)
	s.stopOrRestartForMemory(false)
	if m.starts != 1 {
		t.Errorf("started %d times after, want once", m.starts)
	}
}

func TestDecideForGamesAndMemory(t *testing.T) {
	p := policy{preset: kindness.Get(kindness.Full), hold: "p2pool has no peers"}
	if _, reason, hard := decide(p, conditions{memoryShort: true}, OverrideNone); reason != ReasonMemoryShort || !hard {
		t.Errorf("memory short under a hold reads %q; the stopped process is the thing to explain", reason)
	}
	p.hold = ""
	if target, reason, hard := decide(p, conditions{gameActive: true}, OverrideNone); target != 0 || reason != ReasonGame || !hard {
		t.Errorf("a game: target %v reason %q hard %v", target, reason, hard)
	}
	if _, reason, _ := decide(p, conditions{gameActive: true}, OverridePause); reason != "manual pause" {
		t.Errorf("the user's own pause reads %q under a game", reason)
	}
}
