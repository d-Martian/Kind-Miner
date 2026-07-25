package scheduler

import (
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
)

// decide mirrors tick()'s ordering — pause override, then CPU thresholds, then
// the idle gate — without the monitors and subprocess tick() would otherwise
// need. Keep it in step with tick().
func decide(s *Scheduler, profile config.ThresholdProfile, usage float64) State {
	if s.Override() == OverridePause {
		return StatePaused
	}
	switch {
	case usage >= profile.PauseAt:
		return StatePaused
	case usage >= profile.ReduceAt:
		return StateReduced
	default:
		if state, _, gated := s.idleGate(); gated {
			return state
		}
		return StateFull
	}
}

// The point of "mine now" is that it is still kind: it skips only the wait for
// the user to step away, and never the backoff that keeps other work smooth.
func TestOverrideInteractionsWithLoad(t *testing.T) {
	const after = 5 * time.Minute
	profile := config.SensitivityProfiles[config.SensitivityMedium] // reduce 40%, pause 60%

	active := fakeIdle{dur: time.Second, ok: true}
	away := fakeIdle{dur: 10 * time.Minute, ok: true}

	tests := []struct {
		name     string
		override Override
		idle     fakeIdle
		usage    float64
		want     State
	}{
		{"idle user, quiet machine, mines fully", OverrideNone, away, 0.10, StateFull},
		{"active user, quiet machine, holds back", OverrideNone, active, 0.10, StateReduced},
		{"active user, busy machine, still throttles", OverrideNone, active, 0.50, StateReduced},
		{"idle user, heavy load, pauses", OverrideNone, away, 0.80, StatePaused},

		// Mine-now cases: the idle wait is skipped, the load backoff is not.
		{"mine now, active user, quiet machine", OverrideMine, active, 0.10, StateFull},
		{"mine now yields to a busy machine", OverrideMine, active, 0.50, StateReduced},
		{"mine now still pauses under heavy load", OverrideMine, active, 0.80, StatePaused},

		// Pause wins over everything, including mine-now having been set before.
		{"pause overrides a quiet machine", OverridePause, away, 0.10, StatePaused},
		{"pause overrides an active user", OverridePause, active, 0.10, StatePaused},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestScheduler(after, tt.override, tt.idle)
			if got := decide(s, profile, tt.usage); got != tt.want {
				t.Errorf("override=%v usage=%.2f idle=%v: state = %v, want %v",
					tt.override, tt.usage, tt.idle.dur, got, tt.want)
			}
		})
	}
}

// The state enum is only a label; what protects the user's machine is the
// thread count and suspend actually reaching the miner process.
func TestKindnessReachesTheEngine(t *testing.T) {
	s := newTestScheduler(5*time.Minute, OverrideMine, fakeIdle{dur: time.Second, ok: true})
	m := s.xmrig.(*stubMiner)

	s.applyState(StateFull, "")
	if m.paused || m.threads != 4 {
		t.Errorf("full speed: paused=%v threads=%d, want false, 4", m.paused, m.threads)
	}

	// Busy machine while mine-now is on: threads must drop, not stay at max.
	s.applyState(StateReduced, "system CPU 55%")
	if m.paused || m.threads != 2 {
		t.Errorf("reduced: paused=%v threads=%d, want false, 2", m.paused, m.threads)
	}

	s.applyState(StateMinimal, "system CPU 70%")
	if m.threads != 1 {
		t.Errorf("minimal: threads=%d, want 1", m.threads)
	}

	s.applyState(StatePaused, "system CPU 90%")
	if !m.paused {
		t.Error("paused: miner was not suspended")
	}
}

// Switching between the two overrides must never leave both set.
func TestOverrideSwitching(t *testing.T) {
	s := newTestScheduler(time.Minute, OverrideNone, fakeIdle{ok: false})

	s.SetOverride(OverrideMine)
	if s.IsManuallyPaused() {
		t.Error("mine-now left the scheduler manually paused")
	}

	// Pausing from mine-now must take effect, not be ignored as a no-op.
	s.SetOverride(OverridePause)
	if !s.IsManuallyPaused() {
		t.Error("pause did not replace the mine-now override")
	}
	if got := s.Override(); got != OverridePause {
		t.Errorf("Override() = %v, want OverridePause", got)
	}

	s.SetOverride(OverrideMine)
	if got := s.Override(); got != OverrideMine {
		t.Errorf("Override() = %v, want OverrideMine", got)
	}
}
