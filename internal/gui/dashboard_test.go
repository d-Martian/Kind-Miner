package gui

import (
	"strings"
	"testing"

	"github.com/kind-miner/kind-miner/internal/kindness"
	"github.com/kind-miner/kind-miner/internal/scheduler"
)

// The header exists to answer one question — "am I mining?" — and a live run
// showed it failing: "Paused" beside a "Pause mining" button, with a green
// Connected tile, is three answers at once. These tests pin the contract that
// resolved it: "Paused" belongs to the user's own pause alone, an automatic
// stop says "Standing by" and promises to return, and the button never offers
// to pause a miner that is already stopped.

func noCountdown() (int, bool) { return 0, false }

func TestStatusSummaryAnswersAmIMining(t *testing.T) {
	polite := kindness.Get(kindness.Polite)

	tests := []struct {
		name       string
		state      scheduler.State
		reason     string
		override   scheduler.Override
		wantPrefix string
		wantWords  []string
	}{
		{
			name:       "manual pause is Paused, and says how it ends",
			state:      scheduler.StatePaused,
			reason:     "manual pause",
			override:   scheduler.OverridePause,
			wantPrefix: "Paused",
			wantWords:  []string{"resume"},
		},
		{
			name:       "an automatic stop is Standing by, never Paused",
			state:      scheduler.StatePaused,
			reason:     "waiting for the CPU to cool (88°C)",
			wantPrefix: "Standing by",
			wantWords:  []string{"cool", "resumes on its own"},
		},
		{
			name:       "battery standby carries its reason",
			state:      scheduler.StatePaused,
			reason:     "running on battery",
			wantPrefix: "Standing by",
			wantWords:  []string{"battery"},
		},
		{
			name:       "mining says so, with the preset",
			state:      scheduler.StateFull,
			wantPrefix: "Mining",
			wantWords:  []string{"Polite"},
		},
		{
			name:       "stepping aside is still mining",
			state:      scheduler.StateReduced,
			reason:     "system CPU 55%",
			wantPrefix: "Mining",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, _ := statusSummary(tt.state, tt.reason, tt.override, polite, noCountdown)
			if !strings.HasPrefix(text, tt.wantPrefix) {
				t.Fatalf("summary = %q, want prefix %q", text, tt.wantPrefix)
			}
			for _, w := range tt.wantWords {
				if !strings.Contains(text, w) {
					t.Errorf("summary = %q, want it to mention %q", text, w)
				}
			}
			// The word Paused is reserved: an automatic stop must never use
			// it, or the two states blur back together.
			if tt.override != scheduler.OverridePause && strings.Contains(text, "Paused") {
				t.Errorf("summary = %q uses \"Paused\" for a stop the user did not ask for", text)
			}
		})
	}
}

func TestPauseLabelNeverContradictsTheHeader(t *testing.T) {
	tests := []struct {
		name     string
		override scheduler.Override
		paused   bool
		want     string
	}{
		{"mining offers to pause", scheduler.OverrideNone, false, labelPause},
		{"manual pause offers to resume", scheduler.OverridePause, true, labelResume},
		// The live-run contradiction: standing by must not offer "Pause
		// mining" — the honest action is making the pause stick.
		{"standby offers to keep the pause", scheduler.OverrideNone, true, labelKeepPaused},
		{"mine-now while standing by, likewise", scheduler.OverrideMine, true, labelKeepPaused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pauseLabel(tt.override, tt.paused); got != tt.want {
				t.Errorf("pauseLabel(%v, %v) = %q, want %q", tt.override, tt.paused, got, tt.want)
			}
		})
	}
}
