package scheduler

import (
	"testing"
	"time"
)

// TestStopStopsTheMiner pins the fix for an orphaned miner.
//
// Stop used to only close stopCh and return, leaving the tick loop to call
// xmrig.Stop() whenever it next ran. Its caller — Supervisor.Shutdown, then
// main — exits the process immediately afterwards, so on the losing side of
// that race the goroutine never ran and xmrig outlived the app that launched
// it: still hashing, still holding the huge pages, with no window or tray icon
// left to stop it from.
func TestStopStopsTheMiner(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{})
	miner := s.xmrig.(*stubMiner)

	s.Stop()

	if !miner.stopped {
		t.Error("Stop returned without stopping the miner; it would be orphaned when the process exits")
	}
}

// Shutdown is reachable twice on some exit paths, and closing a closed channel
// panics.
func TestStopIsIdempotent(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{})
	s.Stop()
	s.Stop()
}

// The tick loop must still stop the miner on its way out, so a caller that only
// signals — rather than calling Stop — does not orphan it either.
func TestStartLoopStopsTheMinerOnStopCh(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{})
	miner := s.xmrig.(*stubMiner)

	done := make(chan struct{})
	go func() { s.Start(); close(done) }()

	close(s.stopCh)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after stopCh closed")
	}
	if !miner.stopped {
		t.Error("the tick loop returned without stopping the miner")
	}
}
