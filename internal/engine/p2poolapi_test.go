package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeAPIFiles lays out a data-api directory the way p2pool does.
func writeAPIFiles(t *testing.T, network, pool, local string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []struct{ sub, name, body string }{
		{"network", "stats", network},
		{"pool", "stats", pool},
		{"local", "stratum", local},
	} {
		if f.body == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Join(dir, f.sub), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.sub, f.name), []byte(f.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The JSON below is the shape p2pool v4.15.1 writes (src/p2pool.cpp and
// src/stratum_server.cpp). If a version bump changes a field name, this test is
// what should catch it.
const (
	sampleNetwork = `{"difficulty":410000000000,"hash":"abc","height":3100000,"reward":600000000000,"timestamp":1700000000}`
	samplePool    = `{"pool_list":["pplns"],"pool_statistics":{"hashRate":9500000,"miners":1200,"totalHashes":123456789,"lastBlockFoundTime":1700000000,"lastBlockFound":3099000,"totalBlocksFound":500,"pplnsWeight":1234,"pplnsWindowSize":2160,"sidechainDifficulty":37000000,"sidechainHeight":8000000}}`
	sampleLocal   = `{"hashrate_15m":2500,"hashrate_1h":2400,"hashrate_24h":2300,"total_hashes":9000000,"total_stratum_shares":12,"last_share_found_time":1700000000,"shares_found":7,"shares_failed":0,"average_effort":98.5,"current_effort":42.1,"connections":1,"incoming_connections":0,"block_reward_share_percent":0.83,"wallet":"4ABC","workers":[]}`
)

func TestReadStats(t *testing.T) {
	dir := writeAPIFiles(t, sampleNetwork, samplePool, sampleLocal)
	p := NewP2Pool(P2PoolOptions{DataAPIDir: dir})

	p.readStats()
	s, ok := p.Stats()
	if !ok {
		t.Fatal("Stats() not ok after reading a full set of files")
	}

	if s.NetworkDifficulty != 410000000000 {
		t.Errorf("NetworkDifficulty = %d, want 410000000000", s.NetworkDifficulty)
	}
	if s.PoolHashrate != 9500000 {
		t.Errorf("PoolHashrate = %d, want 9500000", s.PoolHashrate)
	}
	if s.SidechainDifficulty != 37000000 {
		t.Errorf("SidechainDifficulty = %d, want 37000000", s.SidechainDifficulty)
	}
	if s.PPLNSWindowSize != 2160 {
		t.Errorf("PPLNSWindowSize = %d, want 2160", s.PPLNSWindowSize)
	}
	if s.MinerHashrate15m != 2500 {
		t.Errorf("MinerHashrate15m = %d, want 2500", s.MinerHashrate15m)
	}
	if s.SharesFound != 7 {
		t.Errorf("SharesFound = %d, want 7", s.SharesFound)
	}
	if s.RewardSharePercent != 0.83 {
		t.Errorf("RewardSharePercent = %v, want 0.83", s.RewardSharePercent)
	}
}

// Nothing should be reported until the files exist — a cold start must not show
// a made-up estimate.
func TestStatsNotOKBeforeFilesExist(t *testing.T) {
	p := NewP2Pool(P2PoolOptions{DataAPIDir: t.TempDir()})
	p.readStats()
	if _, ok := p.Stats(); ok {
		t.Error("Stats() ok with no files present")
	}
}

// local/stratum only appears once a miner has connected; its absence must not
// throw away the pool and network numbers.
func TestReadStatsWithoutLocalFile(t *testing.T) {
	dir := writeAPIFiles(t, sampleNetwork, samplePool, "")
	p := NewP2Pool(P2PoolOptions{DataAPIDir: dir})

	p.readStats()
	s, ok := p.Stats()
	if !ok {
		t.Fatal("Stats() not ok without local/stratum")
	}
	if s.PoolHashrate == 0 {
		t.Error("pool stats discarded when local/stratum was missing")
	}
	if _, ok := s.RewardETA(); ok {
		t.Error("RewardETA() ok with no miner hashrate; want not ok")
	}
}

func TestRewardETA(t *testing.T) {
	// Base case: pool finds a block every 410e9/9.5e6 ≈ 43158s (~12h). Our
	// 2500 H/s lands a 37e6-difficulty share every 14800s, well inside the
	// 2160*10s = 21600s window, so the ETA is just the pool block time.
	base := P2PoolStats{
		NetworkDifficulty:   410000000000,
		PoolHashrate:        9500000,
		SidechainDifficulty: 37000000,
		PPLNSWindowSize:     2160,
		MinerHashrate15m:    2500,
	}
	got, ok := base.RewardETA()
	if !ok {
		t.Fatal("RewardETA() not ok for a complete stats set")
	}
	if want := 43158 * time.Second; absDiff(got, want) > time.Minute {
		t.Errorf("RewardETA() = %v, want about %v", got, want)
	}

	// A much slower miner: shares now take 37e6/250 = 148000s, far longer than
	// the 21600s window, so most pool blocks pay nothing and the wait stretches
	// by 148000/21600 ≈ 6.85x.
	slow := base
	slow.MinerHashrate15m = 250
	gotSlow, ok := slow.RewardETA()
	if !ok {
		t.Fatal("RewardETA() not ok for the slow miner")
	}
	if gotSlow <= got {
		t.Errorf("slow miner ETA %v should exceed the fast one %v", gotSlow, got)
	}
	wantSlowSecs := 43158.0 * (148000.0 / 21600.0)
	if want := time.Duration(wantSlowSecs * float64(time.Second)); absDiff(gotSlow, want) > 10*time.Minute {
		t.Errorf("RewardETA() = %v, want about %v", gotSlow, want)
	}

	// The mini sidechain has a much lower share difficulty, so the same miner
	// keeps the window occupied and waits only for the pool's next block.
	mini := base
	mini.SidechainDifficulty = 370000
	gotMini, ok := mini.RewardETA()
	if !ok {
		t.Fatal("RewardETA() not ok on the mini sidechain")
	}
	if absDiff(gotMini, 43158*time.Second) > time.Minute {
		t.Errorf("mini RewardETA() = %v, want about the pool block time", gotMini)
	}
}

func TestRewardETAIncomplete(t *testing.T) {
	full := P2PoolStats{
		NetworkDifficulty:   410000000000,
		PoolHashrate:        9500000,
		SidechainDifficulty: 37000000,
		PPLNSWindowSize:     2160,
		MinerHashrate15m:    2500,
	}
	tests := []struct {
		name   string
		mutate func(*P2PoolStats)
	}{
		{"no network difficulty", func(s *P2PoolStats) { s.NetworkDifficulty = 0 }},
		{"no pool hashrate", func(s *P2PoolStats) { s.PoolHashrate = 0 }},
		{"no miner hashrate", func(s *P2PoolStats) { s.MinerHashrate15m = 0 }},
		{"no sidechain difficulty", func(s *P2PoolStats) { s.SidechainDifficulty = 0 }},
		{"no pplns window", func(s *P2PoolStats) { s.PPLNSWindowSize = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := full
			tt.mutate(&s)
			if d, ok := s.RewardETA(); ok {
				t.Errorf("RewardETA() = %v, ok; want not ok", d)
			}
		})
	}
}

func absDiff(a, b time.Duration) time.Duration {
	if a > b {
		return a - b
	}
	return b - a
}

// The stats only exist if p2pool is actually told to write them, so check the
// arguments rather than trusting the flag names.
func TestStartPassesDataAPIFlags(t *testing.T) {
	bin := fakeP2Pool(t, `echo "$@" > "$KM_ARGS_OUT"; echo "StratumServer event loop started"; sleep 60`)
	apiDir := filepath.Join(t.TempDir(), "api")
	argsOut := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("KM_ARGS_OUT", argsOut)

	p := NewP2Pool(P2PoolOptions{
		BinPath: bin, Wallet: "wallet", NodeHost: "127.0.0.1",
		RPCPort: 18081, ZMQPort: 18083, Chain: "mini", StratumPort: 3333,
		DataAPIDir: apiDir,
	})
	defer p.Stop()

	if err := p.Start(10 * time.Second); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got, err := os.ReadFile(argsOut)
	if err != nil {
		t.Fatalf("args not captured: %v", err)
	}
	args := string(got)
	for _, want := range []string{"--data-api " + apiDir, "--local-api", "--mini"} {
		if !strings.Contains(args, want) {
			t.Errorf("p2pool args %q missing %q", args, want)
		}
	}
	if _, err := os.Stat(apiDir); err != nil {
		t.Errorf("data-api dir not created: %v", err)
	}
}
