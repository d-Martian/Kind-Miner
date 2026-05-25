package scheduler

import (
	"testing"

	"github.com/kind-miner/kind-miner/internal/config"
)

// thresholdDecision mimics the CPU-branch of tick() without touching monitors or engine.
func thresholdDecision(profile config.ThresholdProfile, usage float64) State {
	switch {
	case usage >= profile.PauseAt:
		return StatePaused
	case usage >= profile.ReduceAt:
		return StateReduced
	default:
		return StateFull
	}
}

func TestThresholdDecisions(t *testing.T) {
	tests := []struct {
		name      string
		sensitive config.Sensitivity
		usage     float64
		want      State
	}{
		// medium profile: reduce at 40%, pause at 60%
		{"medium low load", config.SensitivityMedium, 0.20, StateFull},
		{"medium at reduce threshold", config.SensitivityMedium, 0.40, StateReduced},
		{"medium between thresholds", config.SensitivityMedium, 0.50, StateReduced},
		{"medium at pause threshold", config.SensitivityMedium, 0.60, StatePaused},
		{"medium high load", config.SensitivityMedium, 0.90, StatePaused},

		// low profile: reduce at 70%, pause at 80%
		{"low below reduce", config.SensitivityLow, 0.60, StateFull},
		{"low at reduce", config.SensitivityLow, 0.70, StateReduced},
		{"low between", config.SensitivityLow, 0.75, StateReduced},
		{"low at pause", config.SensitivityLow, 0.80, StatePaused},

		// high profile: reduce at 20%, pause at 40%
		{"high idle", config.SensitivityHigh, 0.10, StateFull},
		{"high at reduce", config.SensitivityHigh, 0.20, StateReduced},
		{"high at pause", config.SensitivityHigh, 0.40, StatePaused},
		{"high busy", config.SensitivityHigh, 0.95, StatePaused},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := config.SensitivityProfiles[tt.sensitive]
			got := thresholdDecision(profile, tt.usage)
			if got != tt.want {
				t.Errorf("usage=%.2f sensitivity=%s: state=%v, want %v", tt.usage, tt.sensitive, got, tt.want)
			}
		})
	}
}

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
