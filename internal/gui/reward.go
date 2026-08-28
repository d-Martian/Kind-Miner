package gui

import (
	"fmt"
	"time"

	"github.com/kind-miner/kind-miner/internal/engine"
)

const (
	rewardEstimating = "Next reward: estimating…"
	rewardPrefix     = "Next reward: "
)

// rewardLabel renders the tray's reward line.
//
// The estimate is an average over a random process, not a countdown, so it is
// shown coarsely and with a "~": a precise-looking figure would imply a promise
// mining cannot make. Coarse units also mean the label changes at most once a
// minute, which keeps the tray menu from being rebuilt (and closed) often.
func rewardLabel(stats engine.P2PoolStats, ok bool) string {
	if !ok {
		return rewardEstimating
	}
	eta, ok := stats.RewardETA()
	if !ok {
		return rewardEstimating
	}
	return rewardPrefix + "~" + formatETA(eta)
}

// rewardValue renders just the estimate, for places that already carry a
// "Next reward" label of their own — repeating it inside the value reads as a
// mistake.
func rewardValue(stats engine.P2PoolStats, ok bool) string {
	if !ok {
		return "estimating…"
	}
	eta, ok := stats.RewardETA()
	if !ok {
		return "estimating…"
	}
	return "~" + formatETA(eta)
}

// rewardDetail renders the dashboard's reward line, which has room for the
// share of the next block this miner has earned so far.
func rewardDetail(stats engine.P2PoolStats) string {
	eta, ok := stats.RewardETA()
	if !ok {
		return rewardEstimating
	}
	line := rewardPrefix + "~" + formatETA(eta)
	if stats.RewardSharePercent > 0 {
		line += fmt.Sprintf("  ·  your share of the next block: %.2f%%", stats.RewardSharePercent)
	}
	return line
}

// formatETA renders a duration at a granularity that matches how confident the
// estimate is: minutes for short waits, hours then days beyond that.
func formatETA(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < 90*time.Minute:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute).Minutes()))
	case d < 48*time.Hour:
		d = d.Round(time.Minute)
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		d = d.Round(time.Hour)
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		if h == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd %dh", days, h)
	}
}
