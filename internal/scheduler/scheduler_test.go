package scheduler

import (
	"math"
	"testing"

	"github.com/kind-miner/kind-miner/internal/kindness"
)

func TestStateString(t *testing.T) {
	cases := map[State]string{
		StateFull:    "mining",
		StateReduced: "throttled",
		StateMinimal: "minimal",
		StatePaused:  "paused",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("State(%d).String() = %q, want %q", s, got, want)
		}
	}
}

// The allowance is the whole policy: whatever the machine has spare under the
// kindness ceiling, and nothing when a hard condition says stop.
func TestDecide(t *testing.T) {
	polite := kindness.Get(kindness.Polite) // ceiling 0.78
	full := kindness.Get(kindness.Full)     // ceiling 1.00

	tests := []struct {
		name       string
		policy     policy
		conditions conditions
		override   Override
		wantTarget float64
		wantReason string
	}{
		{
			name:       "quiet machine gets the headroom under the ceiling",
			policy:     policy{preset: polite},
			conditions: conditions{otherCPU: 0.08},
			wantTarget: 0.70,
		},
		{
			name:       "a busy machine leaves less",
			policy:     policy{preset: polite},
			conditions: conditions{otherCPU: 0.60},
			wantTarget: 0.18,
		},
		{
			// The ceiling is the promise. Past it there is nothing left to take,
			// regardless of how much of the machine is technically idle.
			name:       "load above the ceiling stops mining",
			policy:     policy{preset: polite},
			conditions: conditions{otherCPU: 0.85},
			wantTarget: 0,
			wantReason: "system busy · other apps at 85%",
		},
		{
			name:       "full still yields to a fully busy machine",
			policy:     policy{preset: full},
			conditions: conditions{otherCPU: 1.0},
			wantTarget: 0,
			wantReason: "system busy · other apps at 100%",
		},
		{
			name:       "manual pause beats everything",
			policy:     policy{preset: full},
			conditions: conditions{otherCPU: 0},
			override:   OverridePause,
			wantTarget: 0,
			wantReason: "manual pause",
		},
		{
			name:       "battery pauses when asked to",
			policy:     policy{preset: polite, pauseOnBattery: true},
			conditions: conditions{onBattery: true},
			wantTarget: 0,
			wantReason: "running on battery",
		},
		{
			name:       "battery is ignored when not asked to",
			policy:     policy{preset: polite},
			conditions: conditions{onBattery: true, otherCPU: 0.08},
			wantTarget: 0.70,
		},
		{
			name:       "temperature limit pauses",
			policy:     policy{preset: polite, tempLimit: 90},
			conditions: conditions{tempC: 91, otherCPU: 0.08},
			wantTarget: 0,
			wantReason: "cooling down · CPU at 91°C",
		},
		{
			name:       "a limit of zero disables the temperature guard",
			policy:     policy{preset: polite},
			conditions: conditions{tempC: 120, otherCPU: 0.08},
			wantTarget: 0.70,
		},
		{
			// The crux of the redesign: heat governs by degree, not by switch.
			// A machine that idles at 80°C (some do) must keep mining, not
			// hard-stop — the live-run bug that "paused for 24 minutes" on an
			// 80°C idle machine. Well below the soft threshold (limit 95, band
			// 8 → soft 87), the governor leaves the allowance alone.
			name:       "warm but below the soft threshold mines freely",
			policy:     policy{preset: polite, tempLimit: 95, thermalGovernor: true},
			conditions: conditions{tempC: 80, otherCPU: 0.08},
			wantTarget: 0.70,
		},
		{
			// Inside the band it eases off proportionally rather than stopping.
			// At 91°C, factor = (95-91)/(95-87) = 0.5, so 0.70 → 0.35.
			name:       "inside the thermal band it eases off, not stops",
			policy:     policy{preset: polite, tempLimit: 95, thermalGovernor: true},
			conditions: conditions{tempC: 91, otherCPU: 0.08},
			wantTarget: 0.35,
			wantReason: "easing off the heat · CPU at 91°C",
		},
		{
			// At the hard limit it stops, as a last-resort safety.
			name:       "the hard limit still stops mining",
			policy:     policy{preset: polite, tempLimit: 95, thermalGovernor: true},
			conditions: conditions{tempC: 95, otherCPU: 0.08},
			wantTarget: 0,
			wantReason: "cooling down · CPU at 95°C",
		},
		{
			// No readable sensor reports 0°C, which must read as "unknown" —
			// the governor leaves mining alone rather than easing on a guess.
			name:       "no temperature sensor leaves mining untouched",
			policy:     policy{preset: polite, tempLimit: 95, thermalGovernor: true},
			conditions: conditions{tempC: 0, otherCPU: 0.08},
			wantTarget: 0.70,
		},
		{
			// With the governor switched off, the top of the band no longer
			// eases — only the hard limit still applies.
			name:       "governor off means no easing below the limit",
			policy:     policy{preset: polite, tempLimit: 95, thermalGovernor: false},
			conditions: conditions{tempC: 91, otherCPU: 0.08},
			wantTarget: 0.70,
		},
		{
			name:       "an unlocked screen pauses when mining is restricted to locked",
			policy:     policy{preset: polite, onlyWhenLocked: true},
			conditions: conditions{locked: false, lockKnown: true},
			wantTarget: 0,
			wantReason: "screen is unlocked",
		},
		{
			name:       "a locked screen mines",
			policy:     policy{preset: polite, onlyWhenLocked: true},
			conditions: conditions{locked: true, lockKnown: true, otherCPU: 0.08},
			wantTarget: 0.70,
		},
		{
			// Enforcing a rule the machine cannot evaluate would stop mining
			// forever with no way for the user to tell why.
			name:       "an unknowable lock state does not stop mining",
			policy:     policy{preset: polite, onlyWhenLocked: true},
			conditions: conditions{lockKnown: false, otherCPU: 0.08},
			wantTarget: 0.70,
		},
		{
			// The user is still here, so even Greedy is held to Ghost's ceiling.
			name:       "the idle gate holds a hungry preset to the ghost ceiling",
			policy:     policy{preset: full},
			conditions: conditions{otherCPU: 0.10, idleGated: true},
			wantTarget: 0.42,
			wantReason: ReasonWaitingForIdle,
		},
		{
			// Ghost is already only-idle-cycles, so the gate changes nothing and
			// must not claim it is holding anything back.
			name:       "the idle gate is a no-op for ghost",
			policy:     policy{preset: kindness.Get(kindness.Ghost)},
			conditions: conditions{otherCPU: 0.10, idleGated: true},
			wantTarget: 0.42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, reason, _ := decide(tt.policy, tt.conditions, tt.override)
			if math.Abs(target-tt.wantTarget) > 1e-9 {
				t.Errorf("target = %.4f, want %.4f", target, tt.wantTarget)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

// The hard/soft split is what keeps the miner alive on a busy-ish desktop: a
// transient loss of headroom must be soft (eased away, not zeroed), while a
// genuine stop must be hard (zeroed at once). Getting this backwards was the
// "never accumulates any hashrate" bug.
func TestDecideHardness(t *testing.T) {
	polite := kindness.Get(kindness.Polite)
	tests := []struct {
		name       string
		policy     policy
		conditions conditions
		override   Override
		wantHard   bool
	}{
		{"a busy moment is soft", policy{preset: polite}, conditions{otherCPU: 0.9}, OverrideNone, false},
		{"normal mining is soft", policy{preset: polite}, conditions{otherCPU: 0.1}, OverrideNone, false},
		{"the idle gate is soft", policy{preset: polite}, conditions{otherCPU: 0.1, idleGated: true}, OverrideNone, false},
		{"easing off heat is soft", policy{preset: polite, tempLimit: 95, thermalGovernor: true}, conditions{otherCPU: 0.1, tempC: 91}, OverrideNone, false},
		{"manual pause is hard", policy{preset: polite}, conditions{}, OverridePause, true},
		{"battery is hard", policy{preset: polite, pauseOnBattery: true}, conditions{onBattery: true}, OverrideNone, true},
		{"the temperature limit is hard", policy{preset: polite, tempLimit: 95}, conditions{tempC: 96}, OverrideNone, true},
		{"an unlocked screen is hard", policy{preset: polite, onlyWhenLocked: true}, conditions{lockKnown: true, locked: false}, OverrideNone, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, hard := decide(tt.policy, tt.conditions, tt.override); hard != tt.wantHard {
				t.Errorf("hard = %v, want %v", hard, tt.wantHard)
			}
		})
	}
}

// A one-tick spike must dent the allowance, not wipe it — the difference
// between a miner that recovers in seconds and one that never climbs off zero.
func TestTransientSpikeDoesNotCollapseAllowance(t *testing.T) {
	polite := kindness.Get(kindness.Polite)
	allowed := 0.6

	// A single busy tick: soft target 0, eased at the fall rate.
	target, _, hard := decide(policy{preset: polite}, conditions{otherCPU: 0.95}, OverrideNone)
	if hard {
		t.Fatal("a transient busy moment was treated as a hard stop")
	}
	allowed = approach(allowed, target, polite)
	if allowed <= 0 {
		t.Fatalf("one spike collapsed the allowance to %.3f", allowed)
	}
	if allowed >= 0.6 {
		t.Fatalf("the spike did not reduce the allowance at all: %.3f", allowed)
	}

	// A hard stop, by contrast, is zeroed at once by the tick (mirrored here).
	_, _, hard = decide(policy{preset: polite}, conditions{}, OverridePause)
	if !hard {
		t.Fatal("manual pause should be a hard stop")
	}
}

// Falling fast and rising slowly is the whole reason the miner goes unnoticed.
// If these ever converge, kind-miner stops being kind.
func TestApproachIsAsymmetric(t *testing.T) {
	for _, preset := range kindness.All() {
		t.Run(preset.Label, func(t *testing.T) {
			if preset.Fall <= preset.Rise {
				t.Fatalf("Fall (%.3f) must exceed Rise (%.3f)", preset.Fall, preset.Rise)
			}

			// From the same distance, one tick down must cover more ground than
			// one tick up.
			const start, low, high = 0.5, 0.1, 0.9
			downMoved := start - approach(start, low, preset)
			upMoved := approach(start, high, preset) - start
			if downMoved <= upMoved {
				t.Errorf("one tick moved %.4f down but %.4f up; backing off must be faster", downMoved, upMoved)
			}
		})
	}
}

// Approaching must converge on the target and never overshoot it, or the miner
// would oscillate around the ceiling instead of settling under it.
func TestApproachConverges(t *testing.T) {
	preset := kindness.Get(kindness.Polite)
	const target = 0.42

	allowed := 0.0
	for i := 0; i < 500; i++ {
		allowed = approach(allowed, target, preset)
		if allowed > target+1e-9 {
			t.Fatalf("tick %d overshot: allowed = %.6f > target %.4f", i, allowed, target)
		}
	}
	if math.Abs(allowed-target) > 1e-3 {
		t.Errorf("after 500 ticks allowed = %.6f, want ≈ %.4f", allowed, target)
	}
}

// The rates are quoted per 250ms; rescaling must preserve "0 stays 0, 1 stays
// 1" and keep everything in between a valid fraction, or a retuned tick would
// silently change every preset.
func TestScaleRate(t *testing.T) {
	if got := scaleRate(0); got != 0 {
		t.Errorf("scaleRate(0) = %v, want 0", got)
	}
	if got := scaleRate(1); got != 1 {
		t.Errorf("scaleRate(1) = %v, want 1", got)
	}
	for _, preset := range kindness.All() {
		for _, r := range []float64{preset.Fall, preset.Rise} {
			got := scaleRate(r)
			if got <= 0 || got > 1 {
				t.Errorf("scaleRate(%.3f) = %v, want in (0,1]", r, got)
			}
			// A longer tick can only close more of the gap, never less.
			if got < r {
				t.Errorf("scaleRate(%.3f) = %v, want ≥ the per-reference-tick rate", r, got)
			}
		}
	}
}

// dutyFor turns a machine-wide allowance into the run fraction that hits it,
// scaling by how much of the machine the miner covers at full tilt.
func TestDutyFor(t *testing.T) {
	tests := []struct {
		name      string
		allowed   float64
		fullShare float64
		want      float64
	}{
		{"miner covers the whole machine, so duty tracks allowance", 0.7, 1.0, 0.7},
		{"a half-machine miner runs twice as hard for the same share", 0.4, 0.5, 0.8},
		{"never more than flat out", 0.9, 0.5, 1.0},
		{"no allowance, no duty", 0, 1.0, 0},
		{"a stopped miner cannot be scheduled", 0.5, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dutyFor(tt.allowed, tt.fullShare); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("dutyFor(%.2f, %.2f) = %.4f, want %.4f", tt.allowed, tt.fullShare, got, tt.want)
			}
		})
	}
}

func TestStateForDuty(t *testing.T) {
	cases := []struct {
		duty float64
		want State
	}{
		{0, StatePaused},
		{0.1, StateMinimal},
		{0.5, StateReduced},
		{0.95, StateFull},
		{1, StateFull},
	}
	for _, tt := range cases {
		if got := stateForDuty(tt.duty); got != tt.want {
			t.Errorf("stateForDuty(%.2f) = %v, want %v", tt.duty, got, tt.want)
		}
	}
}

// The thermal governor is a linear taper across the band, clamped at both ends.
func TestThermalFactor(t *testing.T) {
	const limit = 95 // band 8 → soft threshold 87
	cases := []struct {
		tempC float64
		want  float64
	}{
		{70, 1},   // well below the band
		{87, 1},   // at the soft threshold
		{91, 0.5}, // halfway across
		{95, 0},   // at the hard limit
		{99, 0},   // clamped past it
	}
	for _, tt := range cases {
		if got := thermalFactor(tt.tempC, limit); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("thermalFactor(%.0f, %d) = %.3f, want %.3f", tt.tempC, limit, got, tt.want)
		}
	}
}

func TestThreadCap(t *testing.T) {
	if got := threadCap(3); got != 3 {
		t.Errorf("threadCap(3) = %d, want 3", got)
	}
	// 0 means "the whole machine" now — duty and the ceiling do the limiting —
	// and must never resolve to zero even on a single-core host.
	if got := threadCap(0); got < 1 {
		t.Errorf("threadCap(0) = %d, want at least 1", got)
	}
}
