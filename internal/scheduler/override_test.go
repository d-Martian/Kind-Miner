package scheduler

import (
	"math"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/kindness"
)

// The point of "mine now" is that it is still kind: it skips only the wait for
// the user to step away, and never the backoff that keeps other work smooth.
func TestOverrideInteractionsWithLoad(t *testing.T) {
	const after = 5 * time.Minute

	active := fakeIdle{dur: time.Second, ok: true}
	away := fakeIdle{dur: 10 * time.Minute, ok: true}

	// Polite: ceiling 0.78. Ghost's 0.52 is what the idle gate holds it to.
	tests := []struct {
		name       string
		override   Override
		idle       fakeIdle
		otherCPU   float64
		wantTarget float64
	}{
		{"idle user, quiet machine, takes the full ceiling", OverrideNone, away, 0.10, 0.68},
		{"active user, quiet machine, held to the ghost ceiling", OverrideNone, active, 0.10, 0.42},
		{"active user, busy machine, gets what is left of the ghost ceiling", OverrideNone, active, 0.40, 0.12},
		{"idle user, heavy load, stops", OverrideNone, away, 0.80, 0},

		// Mine-now skips the idle wait; the load backoff is untouched.
		{"mine now, active user, quiet machine", OverrideMine, active, 0.10, 0.68},
		{"mine now yields to a busy machine", OverrideMine, active, 0.60, 0.18},
		{"mine now still stops under heavy load", OverrideMine, active, 0.90, 0},

		// Pause wins over everything, including mine-now having been set before.
		{"pause overrides a quiet machine", OverridePause, away, 0.10, 0},
		{"pause overrides an active user", OverridePause, active, 0.10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestScheduler(after, tt.override, tt.idle)
			p := policy{preset: s.preset}
			c := conditions{otherCPU: tt.otherCPU, idleGated: s.idleGated(tt.override)}

			target, _, _ := decide(p, c, tt.override)
			if diff := target - tt.wantTarget; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("override=%v other=%.2f idle=%v: target = %.4f, want %.4f",
					tt.override, tt.otherCPU, tt.idle.dur, target, tt.wantTarget)
			}
		})
	}
}

// The allowance is only a number until it reaches the process. What protects
// the user's machine is the thread count and the suspend.
// The allowance reaches the miner as a duty fraction, mapped by how much of
// the machine the miner covers at full tilt. No thread thrash, no restart — the
// whole point of the rewrite, since restarting stalled the miner in RandomX
// init and it never produced a hash.
func TestAllowanceReachesTheEngine(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{ok: false})
	// A half-machine miner: full share 0.5, so an allowance maps to twice the
	// duty.
	s.cores = 8
	s.maxThreads = 4
	m := s.xmrig.(*stubMiner)

	s.apply(0.5, "")
	if m.duty != 1.0 {
		t.Errorf("full allowance: duty = %.2f, want 1.0", m.duty)
	}
	if state, _ := s.CurrentState(); state != StateFull {
		t.Errorf("state = %v, want StateFull", state)
	}

	// Falling is immediate — that is the entire promise. 0.25/0.5 = 0.5 duty.
	s.apply(0.25, "busy")
	if math.Abs(m.duty-0.5) > 1e-9 {
		t.Errorf("stepping aside: duty = %.2f, want 0.5", m.duty)
	}
	if state, _ := s.CurrentState(); state != StateReduced {
		t.Errorf("state = %v, want StateReduced", state)
	}

	// No allowance suspends the miner (duty 0) and records the reason.
	s.apply(0, "system busy · other apps at 95%")
	if m.duty != 0 {
		t.Errorf("no allowance: duty = %.2f, want 0", m.duty)
	}
	if state, reason := s.CurrentState(); state != StatePaused || reason != "system busy · other apps at 95%" {
		t.Errorf("state = %v reason = %q, want StatePaused / the busy reason", state, reason)
	}
}

// Duty control has no restart cost, so both directions apply on the very tick
// they are computed — the fall-fast/rise-slow shape lives entirely in the
// smoothed allowance, not in the actuator.
func TestDutyAppliesImmediatelyBothWays(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{ok: false})
	s.cores = 8
	s.maxThreads = 8 // full share 1.0, duty == allowance
	m := s.xmrig.(*stubMiner)

	s.apply(0.9, "")
	if math.Abs(m.duty-0.9) > 1e-9 {
		t.Fatalf("duty = %.2f, want 0.9", m.duty)
	}
	s.apply(0.2, "busy")
	if math.Abs(m.duty-0.2) > 1e-9 {
		t.Errorf("fall: duty = %.2f, want 0.2 immediately", m.duty)
	}
	s.apply(0.8, "")
	if math.Abs(m.duty-0.8) > 1e-9 {
		t.Errorf("rise: duty = %.2f, want 0.8 immediately", m.duty)
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

// Changing preset must take effect without restarting anything, because the
// tray offers it as a one-click change.
func TestSetKindnessTakesEffect(t *testing.T) {
	s := newTestScheduler(0, OverrideNone, fakeIdle{ok: false})

	s.SetKindness(kindness.Full)
	if got := s.Preset().Level; got != kindness.Full {
		t.Fatalf("Preset() = %v, want full", got)
	}

	// An unknown level must fall back rather than leave the scheduler with a
	// zero-valued preset, which would have a ceiling of 0 and never mine.
	s.SetKindness("nonsense")
	if got := s.Preset(); got.Ceiling <= 0 {
		t.Errorf("Preset() after an unknown level has ceiling %.2f, want the default's", got.Ceiling)
	}
}
