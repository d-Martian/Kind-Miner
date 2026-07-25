package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid p2pool-remote",
			cfg:     Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, ThrottleSensitivity: SensitivityMedium},
			wantErr: false,
		},
		{
			name:    "valid pool mode",
			cfg:     Config{Wallet: "4ABC", Mode: ModePool, PoolURL: "pool.host:3333", ThrottleSensitivity: SensitivityLow},
			wantErr: false,
		},
		{
			name:    "missing wallet",
			cfg:     Config{Mode: ModeP2PoolRemote, ThrottleSensitivity: SensitivityMedium},
			wantErr: true,
		},
		{
			name:    "invalid mode",
			cfg:     Config{Wallet: "4ABC", Mode: "unknown", ThrottleSensitivity: SensitivityMedium},
			wantErr: true,
		},
		{
			name:    "pool mode missing pool_url",
			cfg:     Config{Wallet: "4ABC", Mode: ModePool, ThrottleSensitivity: SensitivityMedium},
			wantErr: true,
		},
		{
			name:    "invalid sensitivity",
			cfg:     Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, ThrottleSensitivity: "extreme"},
			wantErr: true,
		},
		{
			name:    "explicit mini chain",
			cfg:     Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, ThrottleSensitivity: SensitivityMedium, P2PoolChain: ChainMini},
			wantErr: false,
		},
		{
			name:    "explicit main chain",
			cfg:     Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, ThrottleSensitivity: SensitivityMedium, P2PoolChain: ChainMain},
			wantErr: false,
		},
		{
			// Without this check a typo joins the main chain, where a desktop
			// miner may never accumulate a payout.
			name:    "misspelled chain is rejected",
			cfg:     Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, ThrottleSensitivity: SensitivityMedium, P2PoolChain: "minni"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Mode != ModeP2PoolRemote {
		t.Errorf("default mode = %q, want %q", cfg.Mode, ModeP2PoolRemote)
	}
	if cfg.P2PoolChain != "mini" {
		t.Errorf("default p2pool_chain = %q, want mini", cfg.P2PoolChain)
	}
	if !cfg.ManageP2Pool {
		t.Error("default manage_p2pool should be true")
	}
	if cfg.ThrottleSensitivity != SensitivityMedium {
		t.Errorf("default sensitivity = %q, want medium", cfg.ThrottleSensitivity)
	}
	if !cfg.PauseOnBattery {
		t.Error("default pause_on_battery should be true")
	}
	if cfg.TempLimitCelsius != 95 {
		t.Errorf("default temp_limit_celsius = %v, want 95", cfg.TempLimitCelsius)
	}
}

// A config that omits p2pool_chain, or leaves the key empty, must still mine on
// the mini sidechain — anything else silently moves the user to the main chain.
func TestLoadChainDefaults(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"key absent", "wallet: 4ABC\n", ChainMini},
		{"key present but empty", "wallet: 4ABC\np2pool_chain:\n", ChainMini},
		{"explicit main is kept", "wallet: 4ABC\np2pool_chain: main\n", ChainMain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			SetPath(path)
			defer SetPath("")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.P2PoolChain != tt.want {
				t.Errorf("p2pool_chain = %q, want %q", cfg.P2PoolChain, tt.want)
			}
		})
	}
}

func TestSensitivityProfiles(t *testing.T) {
	for name, p := range SensitivityProfiles {
		if p.ReduceAt >= p.PauseAt {
			t.Errorf("profile %q: ReduceAt (%.2f) must be < PauseAt (%.2f)", name, p.ReduceAt, p.PauseAt)
		}
		if p.ReduceAt < 0 || p.ReduceAt > 1 {
			t.Errorf("profile %q: ReduceAt %.2f out of [0,1]", name, p.ReduceAt)
		}
		if p.PauseAt < 0 || p.PauseAt > 1 {
			t.Errorf("profile %q: PauseAt %.2f out of [0,1]", name, p.PauseAt)
		}
	}
}
