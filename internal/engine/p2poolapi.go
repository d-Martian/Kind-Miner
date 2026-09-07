package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// P2PoolStats is the subset of p2pool's JSON statistics kind-miner uses.
//
// Field names come from p2pool v4.18: network/stats and pool/stats are
// written in src/p2pool.cpp, local/stratum in src/stratum_server.cpp. Re-check
// them when the pinned p2pool version changes.
type P2PoolStats struct {
	// NetworkDifficulty is the Monero network difficulty (network/stats).
	NetworkDifficulty uint64
	// NetworkHeight is the Monero chain height (network/stats).
	NetworkHeight uint64
	// BlockReward is the current coinbase reward in atomic units (network/stats).
	BlockReward uint64
	// PoolHashrate is the whole sidechain's hashrate in H/s (pool/stats).
	PoolHashrate uint64
	// SidechainHeight is the p2pool sharechain height (pool/stats).
	SidechainHeight uint64
	// SidechainDifficulty is the difficulty of one p2pool share (pool/stats).
	SidechainDifficulty uint64
	// PPLNSWindowSize is how many sidechain blocks the payout window spans.
	PPLNSWindowSize uint64
	// MinerHashrate15m is our own 15-minute average in H/s (local/stratum).
	MinerHashrate15m uint64
	// SharesFound is how many sidechain shares we have landed.
	SharesFound uint64
	// RewardSharePercent is our current slice of the next block reward.
	RewardSharePercent float64

	UpdatedAt time.Time
}

// sidechainBlockTime is p2pool's target time between sidechain blocks. It is 10
// seconds on the main, mini and nano sidechains alike.
const sidechainBlockTime = 10 * time.Second

// moneroBlockTime is the Monero network's target block interval. Network
// hashrate is difficulty divided by it.
const moneroBlockTime = 120 * time.Second

// atomicUnitsPerXMR converts piconero — the units every Monero RPC reports
// amounts in — to whole XMR.
const atomicUnitsPerXMR = 1e12

// statsPollInterval is how often the JSON files are re-read. They are rewritten
// by p2pool roughly once per sidechain block, so polling faster gains nothing.
const statsPollInterval = 15 * time.Second

type networkStatsFile struct {
	Difficulty uint64 `json:"difficulty"`
	Height     uint64 `json:"height"`
	Reward     uint64 `json:"reward"`
}

type poolStatsFile struct {
	PoolStatistics struct {
		HashRate            uint64 `json:"hashRate"`
		Miners              uint64 `json:"miners"`
		PPLNSWindowSize     uint64 `json:"pplnsWindowSize"`
		SidechainDifficulty uint64 `json:"sidechainDifficulty"`
		SidechainHeight     uint64 `json:"sidechainHeight"`
	} `json:"pool_statistics"`
}

type localStratumFile struct {
	Hashrate15m        uint64  `json:"hashrate_15m"`
	SharesFound        uint64  `json:"shares_found"`
	RewardSharePercent float64 `json:"block_reward_share_percent"`
}

// Stats returns the most recent statistics read from p2pool's data API. ok is
// false until a full set has been read, which takes a few minutes from a cold
// start because p2pool has to sync the sidechain first.
func (p *P2Pool) Stats() (P2PoolStats, bool) {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	return p.stats, p.statsOK
}

// pollStats refreshes the cached statistics until the context-less stop channel
// closes. Started by Start when a data API directory is configured.
func (p *P2Pool) pollStats(stop <-chan struct{}) {
	t := time.NewTicker(statsPollInterval)
	defer t.Stop()
	for {
		p.readStats()
		select {
		case <-t.C:
		case <-stop:
			return
		}
	}
}

// readStats reads the three JSON files. p2pool writes them atomically, so a
// plain read cannot observe a half-written file.
func (p *P2Pool) readStats() {
	dir := p.dataAPIDir
	if dir == "" {
		return
	}

	var net networkStatsFile
	var pool poolStatsFile
	var local localStratumFile

	if err := readJSONFile(filepath.Join(dir, "network", "stats"), &net); err != nil {
		return
	}
	if err := readJSONFile(filepath.Join(dir, "pool", "stats"), &pool); err != nil {
		return
	}
	// local/stratum only appears once the Stratum server has served a miner, so
	// its absence is normal early on and must not discard the other two files.
	_ = readJSONFile(filepath.Join(dir, "local", "stratum"), &local)

	s := P2PoolStats{
		NetworkDifficulty:   net.Difficulty,
		NetworkHeight:       net.Height,
		BlockReward:         net.Reward,
		PoolHashrate:        pool.PoolStatistics.HashRate,
		SidechainHeight:     pool.PoolStatistics.SidechainHeight,
		SidechainDifficulty: pool.PoolStatistics.SidechainDifficulty,
		PPLNSWindowSize:     pool.PoolStatistics.PPLNSWindowSize,
		MinerHashrate15m:    local.Hashrate15m,
		SharesFound:         local.SharesFound,
		RewardSharePercent:  local.RewardSharePercent,
		UpdatedAt:           time.Now(),
	}

	p.statsMu.Lock()
	p.stats = s
	p.statsOK = true
	p.statsMu.Unlock()
}

func readJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// RewardETA estimates how long until this miner's share of a p2pool block
// reward arrives. ok is false when the inputs are not yet known.
//
// The estimate composes two waits:
//
//	T_block  = network difficulty / pool hashrate
//	           how often the whole sidechain finds a Monero block
//	T_share  = sidechain difficulty / our hashrate
//	           how often we land a share
//	T_window = PPLNS window size * 10s
//	           how long a share keeps earning
//
// A miner is paid from every pool block found while at least one of their
// shares sits in the PPLNS window. If shares come faster than the window
// empties (T_share <= T_window) the window is effectively always occupied, so
// the wait is just T_block. Otherwise only a T_window/T_share fraction of pool
// blocks pay out, which stretches the wait by the inverse of that fraction.
//
// This is an expectation, not a schedule: mining is a Poisson process, so any
// individual wait varies widely around it. Present it as an approximation.
func (s P2PoolStats) RewardETA() (time.Duration, bool) {
	if s.PoolHashrate == 0 || s.NetworkDifficulty == 0 {
		return 0, false
	}
	if s.MinerHashrate15m == 0 || s.SidechainDifficulty == 0 || s.PPLNSWindowSize == 0 {
		return 0, false
	}

	blockSecs := float64(s.NetworkDifficulty) / float64(s.PoolHashrate)
	shareSecs := float64(s.SidechainDifficulty) / float64(s.MinerHashrate15m)
	windowSecs := float64(s.PPLNSWindowSize) * sidechainBlockTime.Seconds()

	etaSecs := blockSecs
	if shareSecs > windowSecs {
		etaSecs *= shareSecs / windowSecs
	}

	// Guard against a nonsensical result from a transient reading rather than
	// showing the user a negative or absurd duration.
	if etaSecs <= 0 || etaSecs > float64((10*365*24*time.Hour)/time.Second) {
		return 0, false
	}
	return time.Duration(etaSecs * float64(time.Second)), true
}

// NetworkHashrate returns the whole Monero network's hashrate in H/s, derived
// from difficulty and the target block time. ok is false before difficulty is
// known.
func (s P2PoolStats) NetworkHashrate() (float64, bool) {
	if s.NetworkDifficulty == 0 {
		return 0, false
	}
	return float64(s.NetworkDifficulty) / moneroBlockTime.Seconds(), true
}

// EstimatedXMRPerDay returns the expected daily earnings in XMR for a miner
// running at hashrate H/s.
//
// The expectation is the same whether mining solo or on p2pool, because p2pool
// takes no fee and pays proportionally to work done: over a day this machine
// contributes hashrate × 86400 hashes against a difficulty of that many hashes
// per block, and each block pays BlockReward.
//
// It is a long-run average over a random process. P2Pool pays per block found,
// so actual income arrives in lumps and a single day rarely resembles this.
func (s P2PoolStats) EstimatedXMRPerDay(hashrate float64) (float64, bool) {
	if hashrate <= 0 || s.NetworkDifficulty == 0 || s.BlockReward == 0 {
		return 0, false
	}
	const secondsPerDay = 24 * 60 * 60
	blocksPerDay := hashrate * secondsPerDay / float64(s.NetworkDifficulty)
	return blocksPerDay * float64(s.BlockReward) / atomicUnitsPerXMR, true
}

// ShareInterval returns the average time between this miner's p2pool shares.
// Landing shares is what puts a miner in the payout window, so it is the
// number that decides whether the chosen sidechain suits the machine.
func (s P2PoolStats) ShareInterval() (time.Duration, bool) {
	if s.MinerHashrate15m == 0 || s.SidechainDifficulty == 0 {
		return 0, false
	}
	secs := float64(s.SidechainDifficulty) / float64(s.MinerHashrate15m)
	return time.Duration(secs * float64(time.Second)), true
}

// PayoutWindow returns how long a share keeps earning.
func (s P2PoolStats) PayoutWindow() (time.Duration, bool) {
	if s.PPLNSWindowSize == 0 {
		return 0, false
	}
	return time.Duration(s.PPLNSWindowSize) * sidechainBlockTime, true
}

// WindowOccupancy returns the fraction of the time this miner has at least one
// share in the PPLNS payout window — in other words, the share of pool blocks
// it is in line to be paid from.
//
// It saturates at 1: once shares arrive faster than the window empties, the
// miner is continuously in the window and mining harder shortens the wait for a
// block rather than increasing the fraction of them that pay.
func (s P2PoolStats) WindowOccupancy() (float64, bool) {
	shareEvery, ok := s.ShareInterval()
	if !ok {
		return 0, false
	}
	window, ok := s.PayoutWindow()
	if !ok {
		return 0, false
	}
	if shareEvery <= window {
		return 1, true
	}
	return float64(window) / float64(shareEvery), true
}

// SuggestedChain names the sidechain that suits this miner's hashrate, and
// whether it differs from the one in use.
//
// The rule is the share interval: a miner wants shares often enough to stay in
// the payout window, and the window is the same length on every chain, so the
// right chain is the one whose difficulty this machine can hit inside it.
func (s P2PoolStats) SuggestedChain(current string) (string, bool) {
	window, ok := s.PayoutWindow()
	if !ok {
		return current, false
	}
	shareEvery, ok := s.ShareInterval()
	if !ok {
		return current, false
	}
	// Comfortably inside the window on the current chain: no reason to move.
	if shareEvery <= window {
		return current, false
	}
	switch current {
	case "main":
		return "mini", true
	case "mini":
		return "nano", true
	}
	return current, false
}

// statsState is embedded in P2Pool; kept here so the API fields live with the
// code that maintains them.
type statsState struct {
	statsMu sync.Mutex
	stats   P2PoolStats
	statsOK bool
}
