package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/engine"
)

func TestFormatETA(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"seconds", 30 * time.Second, "under a minute"},
		{"minutes", 42 * time.Minute, "42m"},
		{"just under the hour switch", 89 * time.Minute, "89m"},
		{"hours and minutes", 3*time.Hour + 25*time.Minute, "3h 25m"},
		{"whole hours", 5 * time.Hour, "5h"},
		{"days and hours", 52 * time.Hour, "2d 4h"},
		{"whole days", 72 * time.Hour, "3d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatETA(tt.d); got != tt.want {
				t.Errorf("formatETA(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestRewardLabel(t *testing.T) {
	complete := engine.P2PoolStats{
		NetworkDifficulty:   410000000000,
		PoolHashrate:        9500000,
		SidechainDifficulty: 37000000,
		PPLNSWindowSize:     2160,
		MinerHashrate15m:    2500,
	}

	// Before p2pool has synced there is nothing to estimate from, and the label
	// must say so rather than show a fabricated number.
	if got := rewardLabel(engine.P2PoolStats{}, false); got != rewardEstimating {
		t.Errorf("rewardLabel(zero, false) = %q, want %q", got, rewardEstimating)
	}
	if got := rewardLabel(engine.P2PoolStats{}, true); got != rewardEstimating {
		t.Errorf("rewardLabel(zero, true) = %q, want %q", got, rewardEstimating)
	}

	got := rewardLabel(complete, true)
	if !strings.HasPrefix(got, rewardPrefix) {
		t.Errorf("rewardLabel() = %q, want the %q prefix", got, rewardPrefix)
	}
	// The tilde matters: this is an average of a random process, not a promise.
	if !strings.Contains(got, "~") {
		t.Errorf("rewardLabel() = %q, want an approximation marker", got)
	}
	if got != "Next reward: ~11h 59m" {
		t.Errorf("rewardLabel() = %q, want %q", got, "Next reward: ~11h 59m")
	}
}

func TestRewardDetailIncludesShare(t *testing.T) {
	stats := engine.P2PoolStats{
		NetworkDifficulty:   410000000000,
		PoolHashrate:        9500000,
		SidechainDifficulty: 37000000,
		PPLNSWindowSize:     2160,
		MinerHashrate15m:    2500,
		RewardSharePercent:  0.83,
	}
	got := rewardDetail(stats)
	if !strings.Contains(got, "0.83%") {
		t.Errorf("rewardDetail() = %q, want it to include the reward share", got)
	}

	// With no share yet, the line should not claim 0.00%.
	stats.RewardSharePercent = 0
	if got := rewardDetail(stats); strings.Contains(got, "%") {
		t.Errorf("rewardDetail() = %q, want no share figure when none has been earned", got)
	}
}
