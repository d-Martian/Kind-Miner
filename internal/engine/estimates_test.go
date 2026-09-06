package engine

import (
	"math"
	"testing"
	"time"
)

// A miner deciding whether kind-miner is worth running reads these numbers, so
// they are checked against hand-computed values rather than against themselves.

func TestNetworkHashrate(t *testing.T) {
	// 4.8 G difficulty over 120s blocks is 40 MH/s.
	s := P2PoolStats{NetworkDifficulty: 4_800_000_000}
	got, ok := s.NetworkHashrate()
	if !ok {
		t.Fatal("NetworkHashrate reported nothing with a known difficulty")
	}
	if math.Abs(got-40_000_000) > 1 {
		t.Errorf("NetworkHashrate = %.0f, want 40000000", got)
	}

	if _, ok := (P2PoolStats{}).NetworkHashrate(); ok {
		t.Error("NetworkHashrate reported a value with no difficulty")
	}
}

func TestEstimatedXMRPerDay(t *testing.T) {
	// A machine doing 6000 H/s against a difficulty of 432e9 finds
	// 6000 × 86400 / 432e9 = 0.0012 blocks a day, each paying 0.6 XMR.
	s := P2PoolStats{NetworkDifficulty: 432_000_000_000, BlockReward: 600_000_000_000}

	got, ok := s.EstimatedXMRPerDay(6000)
	if !ok {
		t.Fatal("EstimatedXMRPerDay reported nothing with full inputs")
	}
	if math.Abs(got-0.00072) > 1e-9 {
		t.Errorf("EstimatedXMRPerDay = %.8f, want 0.00072", got)
	}

	// Every missing input must produce "unknown" rather than a zero presented
	// as an estimate of nothing.
	for name, bad := range map[string]struct {
		stats    P2PoolStats
		hashrate float64
	}{
		"no hashrate":   {s, 0},
		"no difficulty": {P2PoolStats{BlockReward: 600_000_000_000}, 6000},
		"no reward":     {P2PoolStats{NetworkDifficulty: 432_000_000_000}, 6000},
	} {
		if _, ok := bad.stats.EstimatedXMRPerDay(bad.hashrate); ok {
			t.Errorf("%s: EstimatedXMRPerDay reported a value", name)
		}
	}
}

func TestShareIntervalAndPayoutWindow(t *testing.T) {
	s := P2PoolStats{SidechainDifficulty: 12_000_000, MinerHashrate15m: 6000, PPLNSWindowSize: 2160}

	// 12M difficulty at 6 kH/s is a share every 2000 seconds.
	got, ok := s.ShareInterval()
	if !ok {
		t.Fatal("ShareInterval reported nothing")
	}
	if got != 2000*time.Second {
		t.Errorf("ShareInterval = %v, want 2000s", got)
	}

	// 2160 sidechain blocks at 10s each is six hours.
	window, ok := s.PayoutWindow()
	if !ok {
		t.Fatal("PayoutWindow reported nothing")
	}
	if window != 6*time.Hour {
		t.Errorf("PayoutWindow = %v, want 6h", window)
	}

	if _, ok := (P2PoolStats{}).ShareInterval(); ok {
		t.Error("ShareInterval reported a value with no hashrate")
	}
	if _, ok := (P2PoolStats{}).PayoutWindow(); ok {
		t.Error("PayoutWindow reported a value with no window size")
	}
}

// Occupancy is the fraction of pool blocks a miner is in line to be paid from.
// It saturates at 1: once shares arrive faster than the window empties, mining
// harder shortens the wait for a block rather than widening the share of them.
func TestWindowOccupancy(t *testing.T) {
	tests := []struct {
		name  string
		stats P2PoolStats
		want  float64
	}{
		{
			name:  "shares faster than the window are continuous presence",
			stats: P2PoolStats{SidechainDifficulty: 12_000_000, MinerHashrate15m: 60_000, PPLNSWindowSize: 2160},
			want:  1,
		},
		{
			// A share every 12h against a 6h window: present half the time.
			name:  "shares slower than the window leave gaps",
			stats: P2PoolStats{SidechainDifficulty: 259_200_000, MinerHashrate15m: 6000, PPLNSWindowSize: 2160},
			want:  0.5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.stats.WindowOccupancy()
			if !ok {
				t.Fatal("WindowOccupancy reported nothing")
			}
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("WindowOccupancy = %.4f, want %.4f", got, tt.want)
			}
		})
	}

	if _, ok := (P2PoolStats{}).WindowOccupancy(); ok {
		t.Error("WindowOccupancy reported a value with no inputs")
	}
}

// Being on a chain whose shares this machine cannot land inside the payout
// window earns almost nothing, and the symptom — no payouts — is
// indistinguishable from the miner being broken. Hence the advice.
func TestSuggestedChain(t *testing.T) {
	comfortable := P2PoolStats{SidechainDifficulty: 12_000_000, MinerHashrate15m: 60_000, PPLNSWindowSize: 2160}
	struggling := P2PoolStats{SidechainDifficulty: 2_000_000_000, MinerHashrate15m: 6000, PPLNSWindowSize: 2160}

	if _, change := comfortable.SuggestedChain("mini"); change {
		t.Error("suggested a move while shares already land inside the window")
	}

	got, change := struggling.SuggestedChain("main")
	if !change || got != "mini" {
		t.Errorf("SuggestedChain(main) = %q, %v; want mini, true", got, change)
	}
	got, change = struggling.SuggestedChain("mini")
	if !change || got != "nano" {
		t.Errorf("SuggestedChain(mini) = %q, %v; want nano, true", got, change)
	}
	// nano is the smallest chain there is; there is nowhere left to send them.
	if got, change := struggling.SuggestedChain("nano"); change || got != "nano" {
		t.Errorf("SuggestedChain(nano) = %q, %v; want nano, false", got, change)
	}
	// Without measurements, say nothing rather than guess.
	if _, change := (P2PoolStats{}).SuggestedChain("mini"); change {
		t.Error("suggested a move with no measurements to base it on")
	}
}

// Each sidechain has its own peer network, so they must never share a port, and
// the flag must match the chain the config asked for.
func TestChainFlagsAndPorts(t *testing.T) {
	cases := map[string]struct {
		flag string
		port int
	}{
		"main":    {"", 37889},
		"mini":    {"--mini", 37888},
		"nano":    {"--nano", 37890},
		"unknown": {"", 37889},
	}
	seen := map[int]string{}
	for chain, want := range cases {
		if got := chainFlag(chain); got != want.flag {
			t.Errorf("chainFlag(%q) = %q, want %q", chain, got, want.flag)
		}
		got := p2pPortForChain(chain)
		if got != want.port {
			t.Errorf("p2pPortForChain(%q) = %d, want %d", chain, got, want.port)
		}
		if chain == "unknown" {
			continue
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("chains %q and %q share p2p port %d", prev, chain, got)
		}
		seen[got] = chain
	}
}
