package core

import (
	"fmt"
	"log"
	"time"

	"github.com/kind-miner/kind-miner/internal/engine"
)

// An island is p2pool mining a sidechain of its own. It happens when p2pool
// cannot reach any peers, or reaches peers on a different consensus: it
// starts a fresh chain at its own genesis, accepts every share the miner
// sends, reports a healthy hashrate — and never pays, because nobody else is
// on that chain and it never finds a Monero block alone. From the outside it
// looks exactly like working mining, which is why it needs a check of its own.

// islandCheckInterval is how often the sidechain's health is judged.
const islandCheckInterval = time.Minute

// islandGrace is how long a fault must persist before mining is held. Peers
// drop and reconnect, and a sidechain can go a minute without a block; only a
// fault that outlasts a few checks is worth stopping for.
const islandGrace = 3 * time.Minute

// islandStallAfter is how long the sidechain height may stand still before it
// counts as stuck. nano targets a block every 30 seconds, so five minutes
// without one is ten missed blocks — odds of e^-10 on a live chain.
const islandStallAfter = 5 * time.Minute

// sidechainMinDifficulty is the share difficulty every p2pool sidechain starts
// from (MIN_DIFFICULTY in p2pool's side_chain.cpp, the same on main, mini and
// nano). A live sidechain carries enough hashrate to sit far above it.
const sidechainMinDifficulty = 100000

// sidechainWindow is the PPLNS window, 2160 blocks on every sidechain. A chain
// shorter than its own window is days old at most; the real sidechains are
// millions of blocks long.
const sidechainWindow = 2160

// islandPoolRatio is how much bigger than this machine the pool must be. On
// a real sidechain a home miner is a sliver of the pool; on an island the pool
// is this machine and the ratio sits near one.
const islandPoolRatio = 2

// islandSample is one reading of the sidechain.
type islandSample struct {
	At           time.Time
	Peers        uint64
	PeersKnown   bool
	Height       uint64
	Difficulty   uint64
	PoolHashrate uint64
	OwnHashrate  uint64
}

func sampleFromStats(st engine.P2PoolStats, at time.Time) islandSample {
	return islandSample{
		At:           at,
		Peers:        st.Peers,
		PeersKnown:   st.PeersKnown,
		Height:       st.SidechainHeight,
		Difficulty:   st.SidechainDifficulty,
		PoolHashrate: st.PoolHashrate,
		OwnHashrate:  st.MinerHashrate15m,
	}
}

// islandFault names the first check a sample fails, or "" when the sidechain
// looks like the real one. own is the hashrate to compare the pool against.
// The reasons are shown to the user as they are, so each says what is wrong in
// terms they can check.
func islandFault(s islandSample, lastAdvance time.Time, own uint64) string {
	if s.PeersKnown && s.Peers == 0 {
		return "p2pool has no peers"
	}
	if s.Height == 0 {
		return "p2pool has not synced the sidechain yet"
	}
	if s.Difficulty <= sidechainMinDifficulty && s.Height < sidechainWindow {
		return fmt.Sprintf("p2pool is on a sidechain of its own (height %d at starting difficulty)", s.Height)
	}
	if stalled := s.At.Sub(lastAdvance); stalled >= islandStallAfter {
		return fmt.Sprintf("the sidechain has not moved for %d minutes", int(stalled/time.Minute))
	}
	// Unknown either side is no evidence: p2pool reports a pool hashrate of 0
	// until it has a full window, and ours is 0 while the miner is stood down.
	if own > 0 && s.PoolHashrate > 0 && s.PoolHashrate < islandPoolRatio*own {
		return "the pool is no bigger than this machine, so p2pool is mining alone"
	}
	return ""
}

// islandWatch turns samples into a decision to hold mining.
type islandWatch struct {
	lastHeight   uint64
	lastAdvance  time.Time
	failingSince time.Time
	held         bool
	// heldOwn is this machine's hashrate when the hold began. While held the
	// miner is stood down and its own 15-minute average decays towards zero,
	// which would make a one-machine pool look big again and release the hold
	// — only for the miner to restart, recreate the island's evidence, and be
	// held again a few minutes later. Comparing against the hashrate the pool
	// has to beat keeps the hold until the pool actually grows past it.
	heldOwn uint64
}

// observe takes one sample and reports whether mining should be held, and
// why. A fault releases the moment a check passes: the cost of resuming on a
// real chain is nothing, and the scheduler already rises slowly.
func (w *islandWatch) observe(s islandSample) (hold bool, reason string) {
	if w.lastAdvance.IsZero() || s.Height != w.lastHeight {
		w.lastHeight, w.lastAdvance = s.Height, s.At
	}
	own := s.OwnHashrate
	if w.held {
		own = w.heldOwn
	}
	reason = islandFault(s, w.lastAdvance, own)
	if reason == "" {
		w.failingSince, w.held, w.heldOwn = time.Time{}, false, 0
		return false, ""
	}
	if w.failingSince.IsZero() {
		w.failingSince = s.At
	}
	if !w.held && s.At.Sub(w.failingSince) >= islandGrace {
		w.held, w.heldOwn = true, s.OwnHashrate
	}
	return w.held, reason
}

// staleStatsAfter is how old p2pool's statistics may be and still be judged.
// Older means p2pool has stopped writing them, which is a different failure
// from an island and not one this check should name.
const staleStatsAfter = 2 * time.Minute

// watchIslands checks the sidechain every islandCheckInterval and holds or
// releases mining through the scheduler, until stop closes.
func (s *Supervisor) watchIslands(stop <-chan struct{}) {
	var w islandWatch
	var lastReason string
	t := time.NewTicker(islandCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		st, ok := s.p2pool.Stats()
		now := time.Now()
		if !ok || now.Sub(st.UpdatedAt) > staleStatsAfter {
			continue
		}
		hold, reason := w.observe(sampleFromStats(st, now))
		if !hold {
			reason = ""
		}
		if reason != lastReason {
			if reason != "" {
				log.Printf("warning: holding the miner: %s", reason)
			} else {
				log.Printf("sidechain looks healthy again; releasing the miner")
			}
			lastReason = reason
		}
		s.setHold("island", reason)
	}
}
