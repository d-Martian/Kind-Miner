package scheduler

import (
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/kindness"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// fakeIdle drives the idle gate without a desktop session.
type fakeIdle struct {
	dur time.Duration
	ok  bool
}

func (f fakeIdle) IdleTime() (time.Duration, bool) { return f.dur, f.ok }

// stubMiner records what the scheduler asked the engine to do, standing in for
// the real XMRig subprocess. The scheduler's whole control surface is the duty
// fraction now, so that is all the stub needs to capture.
type stubMiner struct {
	duty float64
}

func (m *stubMiner) SetDuty(d float64) { m.duty = d }
func (m *stubMiner) Stop()             {}
func (m *stubMiner) PID() int          { return 0 }

// newTestScheduler builds a Scheduler with the fields the state machine reads,
// avoiding the monitors and the mining subprocess New would otherwise wire up.
func newTestScheduler(after time.Duration, override Override, idle idleSource) *Scheduler {
	return &Scheduler{
		idleFullAfter: after,
		override:      override,
		idle:          idle,
		xmrig:         &stubMiner{},
		cores:         4,
		maxThreads:    4,
		preset:        kindness.Get(kindness.Default),
		Events:        make(chan StateChange, 16),
	}
}

func TestIdleGate(t *testing.T) {
	const after = 5 * time.Minute

	tests := []struct {
		name      string
		after     time.Duration
		override  Override
		idle      fakeIdle
		wantGated bool
	}{
		{
			name:      "user active holds mining to the kindest ceiling",
			after:     after,
			idle:      fakeIdle{dur: time.Minute, ok: true},
			wantGated: true,
		},
		{
			name:      "user idle long enough releases the gate",
			after:     after,
			idle:      fakeIdle{dur: 6 * time.Minute, ok: true},
			wantGated: false,
		},
		{
			name:      "exactly at the threshold releases the gate",
			after:     after,
			idle:      fakeIdle{dur: after, ok: true},
			wantGated: false,
		},
		{
			// Without idle detection the gate must not hold back forever.
			name:      "no idle backend falls through to the chosen kindness",
			after:     after,
			idle:      fakeIdle{ok: false},
			wantGated: false,
		},
		{
			name:      "gate disabled by config",
			after:     0,
			idle:      fakeIdle{dur: time.Second, ok: true},
			wantGated: false,
		},
		{
			name:      "mine-now override skips the wait",
			after:     after,
			override:  OverrideMine,
			idle:      fakeIdle{dur: time.Second, ok: true},
			wantGated: false,
		},
		{
			// Pausing is not the same as being present: the gate is about which
			// ceiling applies, and a paused miner has no allowance either way.
			name:      "pause does not suppress the gate",
			after:     after,
			override:  OverridePause,
			idle:      fakeIdle{dur: time.Second, ok: true},
			wantGated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestScheduler(tt.after, tt.override, tt.idle)
			if got := s.idleGated(tt.override); got != tt.wantGated {
				t.Errorf("idleGated() = %v, want %v", got, tt.wantGated)
			}
		})
	}
}

func TestIdleCountdown(t *testing.T) {
	const after = 5 * time.Minute

	tests := []struct {
		name     string
		after    time.Duration
		override Override
		idle     fakeIdle
		wantSecs int
		wantOK   bool
	}{
		{
			name:     "counts down the remaining wait",
			after:    after,
			idle:     fakeIdle{dur: 4 * time.Minute, ok: true},
			wantSecs: 60,
			wantOK:   true,
		},
		{
			// Rounding up keeps the label off "0s" while the wait is still on.
			name:     "partial second rounds up",
			after:    after,
			idle:     fakeIdle{dur: after - 1500*time.Millisecond, ok: true},
			wantSecs: 2,
			wantOK:   true,
		},
		{"already idle", after, OverrideNone, fakeIdle{dur: after, ok: true}, 0, false},
		{"no backend", after, OverrideNone, fakeIdle{ok: false}, 0, false},
		{"gate disabled", 0, OverrideNone, fakeIdle{dur: time.Second, ok: true}, 0, false},
		{"paused", after, OverridePause, fakeIdle{dur: time.Second, ok: true}, 0, false},
		{"mine now", after, OverrideMine, fakeIdle{dur: time.Second, ok: true}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestScheduler(tt.after, tt.override, tt.idle)
			secs, ok := s.IdleCountdown()
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && secs != tt.wantSecs {
				t.Errorf("secs = %d, want %d", secs, tt.wantSecs)
			}
		})
	}
}

// The overrides are mutually exclusive; setting one must clear the other.
func TestOverridesAreExclusive(t *testing.T) {
	s := newTestScheduler(time.Minute, OverrideNone, fakeIdle{ok: false})

	s.SetOverride(OverrideMine)
	if got := s.Override(); got != OverrideMine {
		t.Fatalf("Override() = %v, want OverrideMine", got)
	}
	if s.IsManuallyPaused() {
		t.Error("IsManuallyPaused() = true while mine-now is active")
	}

	s.SetOverride(OverrideNone)
	if got := s.Override(); got != OverrideNone {
		t.Errorf("Override() = %v, want OverrideNone", got)
	}
}

// The chart this feeds is the answer to "is the miner what's slowing my
// machine?", so the two CPU series must be recorded separately.
func TestHistoryRecordsBothCPUSeries(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{ok: false})
	s.history = stats.NewRing(4)

	s.record(conditions{otherCPU: 0.30, tempC: 55}, 0.10, 2, false)
	s.record(conditions{otherCPU: 0.55}, 0.05, 1, true)

	got := s.History()
	if len(got) != 2 {
		t.Fatalf("History() length = %d, want 2", len(got))
	}
	if got[0].OtherCPU != 0.30 || got[0].MinerCPU != 0.10 {
		t.Errorf("sample 0 = other %.2f miner %.2f, want 0.30 / 0.10", got[0].OtherCPU, got[0].MinerCPU)
	}
	if got[0].TempC != 55 {
		t.Errorf("sample 0 temp = %.1f, want 55", got[0].TempC)
	}
	if got[1].OtherCPU != 0.55 || got[1].MinerCPU != 0.05 {
		t.Errorf("sample 1 = other %.2f miner %.2f, want 0.55 / 0.05", got[1].OtherCPU, got[1].MinerCPU)
	}
	if got[0].BackedOff {
		t.Error("sample 0 marked as a backoff; it was not one")
	}
	if !got[1].BackedOff {
		t.Error("sample 1 not marked as a backoff; the chart would not show the miner stepping aside")
	}
	if got[0].At.IsZero() {
		t.Error("sample has no timestamp; a chart cannot place it on an axis")
	}
}

// The backoff count is the dashboard's evidence that the miner is the one
// getting out of the way, so it must only count events inside the window.
func TestBackoffCountWindow(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{ok: false})
	s.history = stats.NewRing(8)

	now := time.Now()
	s.history.Add(stats.Sample{At: now.Add(-5 * time.Minute), BackedOff: true})
	s.history.Add(stats.Sample{At: now.Add(-30 * time.Second), BackedOff: true})
	s.history.Add(stats.Sample{At: now.Add(-10 * time.Second), BackedOff: true})
	s.history.Add(stats.Sample{At: now.Add(-5 * time.Second), BackedOff: false})

	if got := s.BackoffCount(time.Minute); got != 2 {
		t.Errorf("BackoffCount(1m) = %d, want 2", got)
	}
}

// A scheduler built without a history (as the test helper does) must not panic.
func TestHistoryAbsentIsSafe(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{ok: false})
	s.record(conditions{otherCPU: 0.5}, 0.1, 1, false)
	if got := s.History(); got != nil {
		t.Errorf("History() = %v, want nil", got)
	}
	if got := s.BackoffCount(time.Minute); got != 0 {
		t.Errorf("BackoffCount() = %d, want 0", got)
	}
}

// hashrateStub reports a fixed hashrate, standing in for xmrig's cached API
// reading — which freezes at its last value when the process is suspended.
type hashrateStub struct {
	stubMiner
	hr float64
}

func (m *hashrateStub) Hashrate() float64 { return m.hr }

// While the miner is suspended it does zero work, but its API is frozen on the
// last pre-pause figure. Recording that figure would put phantom hashrate into
// every average taken while paused — and into the earnings estimate.
func TestPausedSamplesRecordZeroHashrate(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{ok: false})
	s.xmrig = &hashrateStub{hr: 5940}
	s.history = stats.NewRing(4)

	s.record(conditions{}, 0.3, 2, false) // running: the reading is real
	s.record(conditions{}, 0, 0, false)   // suspended: the reading is stale

	got := s.History()
	if len(got) != 2 {
		t.Fatalf("History() length = %d, want 2", len(got))
	}
	if got[0].Hashrate != 5940 {
		t.Errorf("running sample hashrate = %.0f, want 5940", got[0].Hashrate)
	}
	if got[1].Hashrate != 0 {
		t.Errorf("suspended sample hashrate = %.0f, want 0 — the stale API figure leaked in", got[1].Hashrate)
	}
}
