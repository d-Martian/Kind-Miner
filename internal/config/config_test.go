package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kind-miner/kind-miner/internal/kindness"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid p2pool-remote",
			cfg:  Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: kindness.Polite},
		},
		{
			name: "valid pool mode",
			cfg:  Config{Wallet: "4ABC", Mode: ModePool, PoolURL: "pool.host:3333", Kindness: kindness.Ghost},
		},
		{
			name:    "missing wallet",
			cfg:     Config{Mode: ModeP2PoolRemote, Kindness: kindness.Polite},
			wantErr: true,
		},
		{
			name:    "invalid mode",
			cfg:     Config{Wallet: "4ABC", Mode: "unknown", Kindness: kindness.Polite},
			wantErr: true,
		},
		{
			name:    "pool mode missing pool_url",
			cfg:     Config{Wallet: "4ABC", Mode: ModePool, Kindness: kindness.Polite},
			wantErr: true,
		},
		{
			name:    "invalid kindness",
			cfg:     Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: "ferocious"},
			wantErr: true,
		},
		{
			// A struct built before migration has run reads as the default
			// rather than being rejected.
			name: "empty kindness is accepted",
			cfg:  Config{Wallet: "4ABC", Mode: ModeP2PoolRemote},
		},
		{
			name: "explicit mini chain",
			cfg:  Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: kindness.Polite, P2PoolChain: ChainMini},
		},
		{
			name: "explicit main chain",
			cfg:  Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: kindness.Polite, P2PoolChain: ChainMain},
		},
		{
			name: "explicit nano chain",
			cfg:  Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: kindness.Polite, P2PoolChain: ChainNano},
		},
		{
			name: "remote node with host:port",
			cfg:  Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: kindness.Polite, RemoteNode: "192.168.8.192:18089"},
		},
		{
			// Blank means "use the default Nodo over Tor", not a bad address.
			name: "blank remote node is accepted",
			cfg:  Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: kindness.Polite, RemoteNode: ""},
		},
		{
			// The failure this guards: caught here it is one dialog at startup;
			// left to node selection it is a minute of downloads and retries
			// before the same message appears.
			name:    "remote node without a port is rejected",
			cfg:     Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: kindness.Polite, RemoteNode: "192.168.8.192"},
			wantErr: true,
		},
		{
			name:    "remote node with a non-numeric port is rejected",
			cfg:     Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: kindness.Polite, RemoteNode: "192.168.8.192:rpc"},
			wantErr: true,
		},
		{
			name:    "remote node with an out-of-range port is rejected",
			cfg:     Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: kindness.Polite, RemoteNode: "192.168.8.192:70000"},
			wantErr: true,
		},
		{
			// Other modes ignore remote_node, so a stale value must not block
			// a config that is otherwise fine.
			name: "remote node ignored in local mode",
			cfg:  Config{Wallet: "4ABC", Mode: ModeP2PoolLocal, Kindness: kindness.Polite, RemoteNode: "192.168.8.192"},
		},
		{
			// Without this check a typo joins the main chain, where a desktop
			// miner may never accumulate a payout.
			name:    "misspelled chain is rejected",
			cfg:     Config{Wallet: "4ABC", Mode: ModeP2PoolRemote, Kindness: kindness.Polite, P2PoolChain: "minni"},
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
	if cfg.P2PoolChain != ChainMini {
		t.Errorf("default p2pool_chain = %q, want mini", cfg.P2PoolChain)
	}
	if !cfg.ManageP2Pool {
		t.Error("default manage_p2pool should be true")
	}
	if cfg.Kindness != kindness.Polite {
		t.Errorf("default kindness = %q, want polite", cfg.Kindness)
	}
	if !cfg.PauseOnBattery {
		t.Error("default pause_on_battery should be true")
	}
	if cfg.MineOnlyWhenLocked {
		t.Error("default mine_only_when_locked should be false — most people want the machine earning whenever they are away")
	}
	if cfg.TempLimitCelsius != 95 {
		t.Errorf("default temp_limit_celsius = %v, want 95", cfg.TempLimitCelsius)
	}
	if !cfg.Chart.ShadeHeadroom || !cfg.Chart.MarkBackoff || !cfg.Chart.FillMiner {
		t.Error("the chart defaults should show the miner stepping aside; that is the point of drawing it")
	}
	if cfg.Chart.WindowSeconds <= 0 {
		t.Errorf("default chart window = %d, want a positive span", cfg.Chart.WindowSeconds)
	}
}

func loadYAML(t *testing.T, contents string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	SetPath(path)
	t.Cleanup(func() { SetPath("") })

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return cfg
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
		{"explicit nano is kept", "wallet: 4ABC\np2pool_chain: nano\n", ChainNano},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := loadYAML(t, tt.yaml).P2PoolChain; got != tt.want {
				t.Errorf("p2pool_chain = %q, want %q", got, tt.want)
			}
		})
	}
}

// Configs written before kindness existed must keep behaving the way their
// owner set them up, without being asked to reconfigure anything.
func TestLoadMigratesThrottleSensitivity(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want kindness.Level
	}{
		{
			// "high" meant highly sensitive to other work, so it maps to the
			// kindest preset — the opposite end of the name.
			name: "high sensitivity becomes ghost",
			yaml: "wallet: 4ABC\nthrottle_sensitivity: high\n",
			want: kindness.Ghost,
		},
		{
			name: "medium sensitivity becomes polite",
			yaml: "wallet: 4ABC\nthrottle_sensitivity: medium\n",
			want: kindness.Polite,
		},
		{
			name: "low sensitivity becomes balanced",
			yaml: "wallet: 4ABC\nthrottle_sensitivity: low\n",
			want: kindness.Balanced,
		},
		{
			name: "neither key present falls back to the default",
			yaml: "wallet: 4ABC\n",
			want: kindness.Default,
		},
		{
			// An explicit choice must win: the old key is only a fallback for
			// people who never made one. "greedy" is also the pre-rename name
			// of Full, so this doubles as the alias-migration case.
			name: "an explicit kindness wins over the old key",
			yaml: "wallet: 4ABC\nkindness: greedy\nthrottle_sensitivity: high\n",
			want: kindness.Full,
		},
		{
			name: "an unrecognised old value falls back to the default",
			yaml: "wallet: 4ABC\nthrottle_sensitivity: extreme\n",
			want: kindness.Default,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadYAML(t, tt.yaml)
			if cfg.Kindness != tt.want {
				t.Errorf("kindness = %q, want %q", cfg.Kindness, tt.want)
			}
			if cfg.ThrottleSensitivity != "" {
				t.Errorf("throttle_sensitivity = %q, want it cleared after migration", cfg.ThrottleSensitivity)
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("migrated config does not validate: %v", err)
			}
		})
	}
}

// Migration is only complete once the dead key stops being written back.
func TestSaveDropsThrottleSensitivity(t *testing.T) {
	cfg := loadYAML(t, "wallet: 4ABC\nthrottle_sensitivity: high\n")
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "throttle_sensitivity") {
		t.Errorf("saved config still carries throttle_sensitivity:\n%s", data)
	}
	if !strings.Contains(string(data), "kindness: ghost") {
		t.Errorf("saved config does not carry the migrated kindness:\n%s", data)
	}
}

// The chart preferences must survive a round trip, including the false ones —
// a bool that silently reverts to its default on restart is worse than not
// offering the setting.
func TestChartOptionsRoundTrip(t *testing.T) {
	cfg := loadYAML(t, "wallet: 4ABC\n")
	cfg.Chart = ChartOptions{
		ShadeHeadroom: false,
		MarkBackoff:   false,
		DrawTemp:      true,
		FillMiner:     false,
		WindowSeconds: 120,
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.Chart != cfg.Chart {
		t.Errorf("chart options = %+v, want %+v", reloaded.Chart, cfg.Chart)
	}
}

// A zero or missing window would divide the chart by nothing.
func TestLoadRepairsChartWindow(t *testing.T) {
	cfg := loadYAML(t, "wallet: 4ABC\nchart:\n  window_seconds: 0\n")
	if cfg.Chart.WindowSeconds <= 0 {
		t.Errorf("chart window = %d, want it repaired to a positive span", cfg.Chart.WindowSeconds)
	}
}

func TestPresetFollowsConfig(t *testing.T) {
	cfg := &Config{Kindness: kindness.Full}
	if got := cfg.Preset().Level; got != kindness.Full {
		t.Errorf("Preset() = %v, want full", got)
	}
	// An empty level must still yield a usable preset, not a zero value whose
	// ceiling of 0 would stop the miner entirely.
	empty := &Config{}
	if got := empty.Preset(); got.Ceiling <= 0 {
		t.Errorf("Preset() on an empty config has ceiling %.2f, want the default's", got.Ceiling)
	}
}
