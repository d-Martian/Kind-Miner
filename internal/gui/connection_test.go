package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/d-Martian/Kind-Miner/internal/config"
	"github.com/d-Martian/Kind-Miner/internal/engine"
)

// The advice has to make sense to someone who has never heard of a PPLNS
// window, and has to be true: a share every 16 hours on nano used to read
// "keeps you in the payout window continuously. nano suits this machine."
func TestChainAdviceSaysHowOftenAShareIsCounting(t *testing.T) {
	nano := func(difficulty, hashrate uint64) engine.P2PoolStats {
		return engine.P2PoolStats{SidechainDifficulty: difficulty, MinerHashrate15m: hashrate, PPLNSWindowSize: 2160, BlockTime: 30 * time.Second}
	}
	main := func(difficulty, hashrate uint64) engine.P2PoolStats {
		return engine.P2PoolStats{SidechainDifficulty: difficulty, MinerHashrate15m: hashrate, PPLNSWindowSize: 2160, BlockTime: 10 * time.Second}
	}
	tests := []struct {
		name    string
		stats   engine.P2PoolStats
		chain   string
		want    []string
		wantNot []string
	}{
		{
			name:  "with no hashrate yet it is still measuring",
			stats: engine.P2PoolStats{},
			chain: "nano",
			want:  []string{chainMeasuring},
		},
		{
			name:    "a share every 16h against nano's 18h is counting two-thirds of the time, not always",
			stats:   nano(351_420_000, 6000),
			chain:   "nano",
			want:    []string{"about every 16h 16m", "counts toward payouts for 18h", "67% of the time"},
			wantNot: []string{"continuously", "suits", "easiest"},
		},
		{
			name:    "a machine that finds shares constantly is told its chain suits it",
			stats:   main(12_000_000, 60_000),
			chain:   "main",
			want:    []string{"practically always", "main suits this computer."},
			wantNot: []string{"100%"},
		},
		{
			name:    "too slow for main is pointed at mini",
			stats:   main(259_200_000, 6000),
			chain:   "main",
			want:    []string{"about every 12h", "for 6h", "39% of the time", "The mini sidechain has easier shares"},
			wantNot: []string{"suits"},
		},
		{
			name:    "too slow even for nano is told there is nowhere smaller, not that nano suits it",
			stats:   nano(2_000_000_000, 6000),
			chain:   "nano",
			want:    []string{"18% of the time", "nano already has the easiest shares"},
			wantNot: []string{"suits"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chainAdvice(tt.stats, tt.chain)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("advice lacks %q:\n%s", w, got)
				}
			}
			for _, w := range tt.wantNot {
				if strings.Contains(got, w) {
					t.Errorf("advice says %q:\n%s", w, got)
				}
			}
		})
	}
}

// Each mode shows only the fields it uses; shown under the others, they invite
// filling in a connection this computer is not making.
func TestConnectionShowsOnlyTheFieldsTheModeUses(t *testing.T) {
	u, _, f := newSettingsTest(t)
	f.connectionTab(u)
	for _, tt := range []struct {
		mode                     config.Mode
		node, chain, hubSettings bool
	}{
		{config.ModeP2PoolRemote, true, true, false},
		{config.ModeHub, false, false, true},
		{config.ModeP2PoolLocal, false, true, false},
		{config.ModeP2PoolRemote, true, true, false},
	} {
		f.mode.SetSelected(string(tt.mode))
		for _, c := range []struct {
			name      string
			got, want bool
		}{
			{"remote node", f.nodeBox.Visible(), tt.node},
			{"sidechain", f.chainBox.Visible(), tt.chain},
			{"hub settings", f.hubBox.Visible(), tt.hubSettings},
		} {
			if c.got != c.want {
				t.Errorf("mode %s: %s shown = %v, want %v", tt.mode, c.name, c.got, c.want)
			}
		}
	}
}

// A desktop already paired opens its settings on hub mode, and sees them.
func TestHubSettingsShowWhenOpenedInHubMode(t *testing.T) {
	u, cfg, _ := newSettingsTest(t)
	cfg.Mode = config.ModeHub
	f := u.newSettingsForm(cfg)
	f.connectionTab(u)
	if !f.hubBox.Visible() {
		t.Fatal("a desktop in hub mode opened its settings with the hub section hidden")
	}
}
