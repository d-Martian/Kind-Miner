package core

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

// healthy is a desktop on p2pool-mini: a sliver of a long, busy sidechain.
func healthy(at time.Time, height uint64) islandSample {
	return islandSample{
		At: at, Peers: 10, PeersKnown: true,
		Height: height, Difficulty: 37_000_000,
		PoolHashrate: 9_500_000, OwnHashrate: 2_500,
	}
}

// genesis is the island from the plan: p2pool alone on its own chain, at
// height 20 and the minimum difficulty, the pool being this machine.
func genesis(at time.Time, height uint64) islandSample {
	return islandSample{
		At: at, Peers: 0, PeersKnown: true,
		Height: height, Difficulty: sidechainMinDifficulty,
		PoolHashrate: 2_600, OwnHashrate: 2_500,
	}
}

func TestIslandFault(t *testing.T) {
	cases := []struct {
		name        string
		sample      islandSample
		lastAdvance time.Time
		own         uint64
		want        string // substring; "" means healthy
	}{
		{name: "a healthy sidechain passes", sample: healthy(t0, 8_000_000), lastAdvance: t0, own: 2_500},
		{
			name:        "no peers",
			sample:      func() islandSample { s := healthy(t0, 8_000_000); s.Peers = 0; return s }(),
			lastAdvance: t0, own: 2_500, want: "no peers",
		},
		{
			// Unknown is not zero: p2pool writes local/p2p on its own timer.
			name:        "an unread peer count is not a fault",
			sample:      func() islandSample { s := healthy(t0, 8_000_000); s.Peers, s.PeersKnown = 0, false; return s }(),
			lastAdvance: t0, own: 2_500,
		},
		{
			name:        "a sidechain not synced yet",
			sample:      func() islandSample { s := healthy(t0, 0); return s }(),
			lastAdvance: t0, own: 2_500, want: "not synced",
		},
		{
			name:        "height 20 at the starting difficulty is a chain of its own",
			sample:      func() islandSample { s := genesis(t0, 20); s.Peers = 3; return s }(),
			lastAdvance: t0, own: 2_500, want: "sidechain of its own (height 20",
		},
		{
			name:        "a sidechain that stopped moving",
			sample:      healthy(t0.Add(6*time.Minute), 8_000_000),
			lastAdvance: t0, own: 2_500, want: "not moved for 6 minutes",
		},
		{
			// Peers present, height long, but the pool is only us: an island
			// that has run long enough to outgrow the genesis check.
			name: "a pool no bigger than this machine",
			sample: func() islandSample {
				s := healthy(t0, 50_000)
				s.PoolHashrate = 3_000
				return s
			}(),
			lastAdvance: t0, own: 2_500, want: "mining alone",
		},
		{
			name:        "our hashrate unknown means no evidence either way",
			sample:      func() islandSample { s := healthy(t0, 8_000_000); s.PoolHashrate = 1; return s }(),
			lastAdvance: t0, own: 0,
		},
		{
			// p2pool reports 0 until it has a full window.
			name:        "a pool hashrate not yet computed is not a fault",
			sample:      func() islandSample { s := healthy(t0, 8_000_000); s.PoolHashrate = 0; return s }(),
			lastAdvance: t0, own: 2_500,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := islandFault(c.sample, c.lastAdvance, c.own)
			if c.want == "" {
				if got != "" {
					t.Errorf("got fault %q on a healthy reading", got)
				}
				return
			}
			if !strings.Contains(got, c.want) {
				t.Errorf("got %q, want it to mention %q", got, c.want)
			}
		})
	}
}

func TestIslandWatchHoldsOnlyAfterTheGrace(t *testing.T) {
	var w islandWatch
	for m := 0; m < int(islandGrace/time.Minute); m++ {
		if hold, reason := w.observe(genesis(t0.Add(time.Duration(m)*time.Minute), 20+uint64(m))); hold {
			t.Fatalf("held after %d minutes (%s); want the %s grace first", m, reason, islandGrace)
		}
	}
	hold, reason := w.observe(genesis(t0.Add(islandGrace), 30))
	if !hold || reason == "" {
		t.Fatalf("not held after %s on an island", islandGrace)
	}
}

func TestIslandWatchIgnoresABriefDropOfPeers(t *testing.T) {
	var w islandWatch
	w.observe(healthy(t0, 100))
	blip := healthy(t0.Add(time.Minute), 106)
	blip.Peers = 0
	w.observe(blip)
	w.observe(healthy(t0.Add(2*time.Minute), 112))
	if hold, _ := w.observe(healthy(t0.Add(4*time.Minute), 118)); hold {
		t.Error("held after peers reconnected within the grace")
	}
}

func TestIslandWatchStaysHeldWhileTheMinerIsStoodDown(t *testing.T) {
	// A long-running island with peers on a different consensus: height moves,
	// peers are up, only the pool-size check sees it.
	lonely := func(at time.Time, height, own uint64) islandSample {
		return islandSample{At: at, Peers: 5, PeersKnown: true, Height: height,
			Difficulty: 900_000, PoolHashrate: 3_000, OwnHashrate: own}
	}
	var w islandWatch
	var hold bool
	for m := 0; m <= int(islandGrace/time.Minute); m++ {
		hold, _ = w.observe(lonely(t0.Add(time.Duration(m)*time.Minute), 50_000+uint64(m)*6, 2_500))
	}
	if !hold {
		t.Fatal("not held on a one-machine pool")
	}
	// Held, the miner stops; its 15-minute average decays towards zero. That
	// must not make the pool look big enough to go back to.
	hold, _ = w.observe(lonely(t0.Add(10*time.Minute), 50_100, 300))
	if !hold {
		t.Error("released because our own decaying average made a one-machine pool look big")
	}
	// Real miners arrive: the pool grows well past what this machine does.
	grown := lonely(t0.Add(11*time.Minute), 50_110, 0)
	grown.PoolHashrate = 50_000
	if hold, _ = w.observe(grown); hold {
		t.Error("still held after the pool grew past this machine")
	}
}

func TestIslandWatchReleasesWhenTheChainMovesAgain(t *testing.T) {
	var w islandWatch
	w.observe(healthy(t0, 100))
	// Height frozen for long enough to stall, then past the grace.
	for m := 1; m <= 9; m++ {
		w.observe(healthy(t0.Add(time.Duration(m)*time.Minute), 100))
	}
	if hold, reason := w.observe(healthy(t0.Add(10*time.Minute), 100)); !hold || !strings.Contains(reason, "not moved") {
		t.Fatalf("hold=%v reason=%q, want held for a stalled chain", hold, reason)
	}
	if hold, _ := w.observe(healthy(t0.Add(11*time.Minute), 101)); hold {
		t.Error("still held after the sidechain moved again")
	}
}
