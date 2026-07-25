package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type Mode string

const (
	ModeP2PoolRemote Mode = "p2pool-remote"
	ModeP2PoolLocal  Mode = "p2pool-local"
	ModePool         Mode = "pool"
)

type Sensitivity string

const (
	SensitivityLow    Sensitivity = "low"
	SensitivityMedium Sensitivity = "medium"
	SensitivityHigh   Sensitivity = "high"
)

type ThresholdProfile struct {
	// CPU fraction (0.0–1.0) at which mining threads are halved
	ReduceAt float64
	// CPU fraction at which mining is fully paused
	PauseAt float64
}

var SensitivityProfiles = map[Sensitivity]ThresholdProfile{
	SensitivityLow:    {ReduceAt: 0.70, PauseAt: 0.80},
	SensitivityMedium: {ReduceAt: 0.40, PauseAt: 0.60},
	SensitivityHigh:   {ReduceAt: 0.20, PauseAt: 0.40},
}

// P2Pool sidechains. mini is the default: its share difficulty is roughly 100x
// lower than the main chain, so a desktop-sized miner finds shares — and
// therefore earns — often enough to matter. Anything that is not exactly
// ChainMini makes p2pool join the main chain, which is why the value is
// validated rather than passed through.
const (
	ChainMini = "mini"
	ChainMain = "main"
)

type Config struct {
	Wallet  string `yaml:"wallet"`
	Mode    Mode   `yaml:"mode"`

	// p2pool-remote
	RemoteNode  string `yaml:"remote_node"`
	P2PoolChain string `yaml:"p2pool_chain"`

	// p2pool-local
	ManageMonerod bool   `yaml:"manage_monerod"`
	MonerodPath   string `yaml:"monerod_path"`

	// p2pool subprocess (modes A and B)
	ManageP2Pool  bool   `yaml:"manage_p2pool"`
	P2PoolBinPath string `yaml:"p2pool_path"`

	// pool mode
	PoolURL string `yaml:"pool_url"`

	// xmrig
	XMRigBinPath string `yaml:"xmrig_path"`

	// tor — kind-miner runs its own Tor process when none is reachable, so
	// users don't need to install or start the Tor service themselves.
	ManageTor  bool   `yaml:"manage_tor"`
	TorBinPath string `yaml:"tor_path"`

	// RunAtStartup mirrors the OS login item managed by internal/autostart. The
	// OS registration is the thing that actually works; this is the record of
	// what the user asked for, used to re-register if the entry goes missing.
	RunAtStartup bool `yaml:"run_at_startup"`

	// throttle
	MaxThreads          int         `yaml:"max_threads"`
	ThrottleSensitivity Sensitivity `yaml:"throttle_sensitivity"`
	PauseOnBattery      bool        `yaml:"pause_on_battery"`
	TempLimitCelsius    float64     `yaml:"temp_limit_celsius"`

	LogLevel string `yaml:"log_level"`
}

func Defaults() *Config {
	return &Config{
		Mode:                ModeP2PoolRemote,
		P2PoolChain:         ChainMini,
		ManageP2Pool:        true,
		ManageTor:           true,
		ThrottleSensitivity: SensitivityMedium,
		PauseOnBattery:      true,
		TempLimitCelsius:    95,
		LogLevel:            "info",
	}
}

// pathOverride, when set via SetPath, replaces the default config location.
var pathOverride string

// SetPath overrides the config file path (used by the --config flag). Pass an
// empty string to restore the default location.
func SetPath(p string) { pathOverride = p }

func Dir() string {
	if pathOverride != "" {
		return filepath.Dir(pathOverride)
	}
	if runtime.GOOS == "windows" {
		dir, _ := os.UserConfigDir()
		return filepath.Join(dir, "kind-miner")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "kind-miner")
}

func Path() string {
	if pathOverride != "" {
		return pathOverride
	}
	return filepath.Join(Dir(), "config.yaml")
}

func Load() (*Config, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		return nil, fmt.Errorf("cannot read config at %s: %w", Path(), err)
	}
	cfg := Defaults()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	// A key present but empty (`p2pool_chain:` with nothing after it) overwrites
	// the default with "", which downstream reads as the main chain. Restore the
	// default instead of silently switching sidechains.
	if cfg.P2PoolChain == "" {
		cfg.P2PoolChain = ChainMini
	}
	return cfg, nil
}

func (c *Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), data, 0o600)
}

func (c *Config) Validate() error {
	if c.Wallet == "" {
		return fmt.Errorf("wallet address is required")
	}
	switch c.Mode {
	case ModeP2PoolRemote, ModeP2PoolLocal, ModePool:
	default:
		return fmt.Errorf("mode must be p2pool-remote, p2pool-local, or pool")
	}
	if c.Mode == ModePool && c.PoolURL == "" {
		return fmt.Errorf("pool_url is required when mode is 'pool'")
	}
	if _, ok := SensitivityProfiles[c.ThrottleSensitivity]; !ok {
		return fmt.Errorf("throttle_sensitivity must be low, medium, or high")
	}
	// Empty means "unset" and Load normalises it to mini. A typo must not fall
	// through to the main chain, where a small miner may never earn a payout.
	switch c.P2PoolChain {
	case "", ChainMini, ChainMain:
	default:
		return fmt.Errorf("p2pool_chain must be %q or %q, got %q", ChainMini, ChainMain, c.P2PoolChain)
	}
	return nil
}

// RunWizard collects the one thing that has no sensible default — the wallet
// address — and writes a config with safe defaults for everything else.
// It must only be called when a terminal is attached.
func RunWizard() (*Config, error) {
	cfg := Defaults()
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("kind-miner — first run")
	fmt.Println("──────────────────────")
	fmt.Println()
	fmt.Println("Mines Monero in the background using idle CPU cycles.")
	fmt.Println("Your coins go directly to your wallet — no account, no fee.")
	fmt.Println()

	cfg.Wallet = prompt(scanner, "Monero wallet address: ", "")
	if cfg.Wallet == "" {
		return nil, fmt.Errorf("wallet address is required")
	}

	if err := cfg.Save(); err != nil {
		return nil, fmt.Errorf("saving config: %w", err)
	}
	fmt.Println()
	return cfg, nil
}

func prompt(scanner *bufio.Scanner, label, defaultVal string) string {
	fmt.Print(label)
	if !scanner.Scan() {
		return defaultVal
	}
	v := strings.TrimSpace(scanner.Text())
	if v == "" {
		return defaultVal
	}
	return v
}
