package scheduler

import (
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/stats"
)

// fakeIdle drives the idle gate without a desktop session.
type fakeIdle struct {
	dur time.Duration
	ok  bool
}

func (f fakeIdle) IdleTime() (time.Duration, bool) { return f.dur, f.ok }

// stubMiner records what the scheduler asked the engine to do, standing in for
// the real XMRig subprocess.
type stubMiner struct {
	paused  bool
	threads int
}

func (m *stubMiner) Pause() error  { m.paused = true; return nil }
func (m *stubMiner) Resume() error { m.paused = false; return nil }
func (m *stubMiner) SetThreads(n int) error {
	m.threads = n
	return nil
}
func (m *stubMiner) Stop()    {}
func (m *stubMiner) PID() int { return 0 }

// newTestScheduler builds a Scheduler with the fields the state machine reads,
// avoiding the monitors and the mining subprocess New would otherwise wire up.
func newTestScheduler(after time.Duration, override Override, idle idleSource) *Scheduler {
	return &Scheduler{
		idleFullAfter: after,
		override:      override,
		idle:          idle,
		xmrig:         &stubMiner{},
		maxThreads:    4,
		Events:        make(chan StateChange, 16),
	}
}

func TestIdleGate(t *testing.T) {
	const after = 5 * time.Minute

	tests := []struct {
		name       string
		after      time.Duration
		override   Override
		idle       fakeIdle
		wantGated  bool
		wantState  State
		wantReason string
	}{
		{
			name:       "user active holds mining at reduced speed",
			after:      after,
			idle:       fakeIdle{dur: time.Minute, ok: true},
			wantGated:  true,
			wantState:  StateReduced,
			wantReason: ReasonWaitingForIdle,
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
			// Without idle detection the gate must not throttle forever.
			name:      "no idle backend falls through to full speed",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestScheduler(tt.after, tt.override, tt.idle)
			state, reason, gated := s.idleGate()
			if gated != tt.wantGated {
				t.Fatalf("gated = %v, want %v", gated, tt.wantGated)
			}
			if !gated {
				return
			}
			if state != tt.wantState {
				t.Errorf("state = %v, want %v", state, tt.wantState)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
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

	s.record(0.30, 0.10)
	s.record(0.55, 0.05)

	got := s.History()
	if len(got) != 2 {
		t.Fatalf("History() length = %d, want 2", len(got))
	}
	if got[0].OtherCPU != 0.30 || got[0].MinerCPU != 0.10 {
		t.Errorf("sample 0 = other %.2f miner %.2f, want 0.30 / 0.10", got[0].OtherCPU, got[0].MinerCPU)
	}
	if got[1].OtherCPU != 0.55 || got[1].MinerCPU != 0.05 {
		t.Errorf("sample 1 = other %.2f miner %.2f, want 0.55 / 0.05", got[1].OtherCPU, got[1].MinerCPU)
	}
	if got[0].At.IsZero() {
		t.Error("sample has no timestamp; a chart cannot place it on an axis")
	}
}

// A scheduler built without a history (as the test helper does) must not panic.
func TestHistoryAbsentIsSafe(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{ok: false})
	s.record(0.5, 0.1)
	if got := s.History(); got != nil {
		t.Errorf("History() = %v, want nil", got)
	}
}
