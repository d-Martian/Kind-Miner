package scheduler

import (
	"testing"
	"time"
)

// fakeIdle drives the idle gate without a desktop session.
type fakeIdle struct {
	dur time.Duration
	ok  bool
}

func (f fakeIdle) IdleTime() (time.Duration, bool) { return f.dur, f.ok }

// newTestScheduler builds a Scheduler with only the fields the idle gate reads,
// avoiding the monitors and the XMRig process New would otherwise wire up.
func newTestScheduler(after time.Duration, override Override, idle idleSource) *Scheduler {
	return &Scheduler{idleFullAfter: after, override: override, idle: idle}
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
