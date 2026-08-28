package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kind-miner/kind-miner/internal/kindness"
)

type Mode string

const (
	ModeP2PoolRemote Mode = "p2pool-remote"
	ModeP2PoolLocal  Mode = "p2pool-local"
	ModePool         Mode = "pool"
)

// Sensitivity is the superseded throttle control, kept only so configs written
// before kindness existed still load. Load migrates it and Save drops it.
type Sensitivity string

const (
	SensitivityLow    Sensitivity = "low"
	SensitivityMedium Sensitivity = "medium"
	SensitivityHigh   Sensitivity = "high"
)

// kindnessForSensitivity maps the old three-point scale onto the kindness
// presets. The old scale ran the other way round — "high" meant highly
// sensitive to other work, so it becomes the kindest preset.
var kindnessForSensitivity = map[Sensitivity]kindness.Level{
	SensitivityHigh:   kindness.Ghost,
	SensitivityMedium: kindness.Polite,
	SensitivityLow:    kindness.Balanced,
}

// P2Pool sidechains. mini is the default: its share difficulty is roughly 100x
// lower than the main chain, so a desktop-sized miner finds shares — and
// therefore earns — often enough to matter. nano is lower again, for machines
// under about 1 kH/s. The value is validated rather than passed through
// because an unrecognised chain makes p2pool silently join the main chain,
// where a small miner may never earn a payout.
const (
	ChainMini = "mini"
	ChainMain = "main"
	ChainNano = "nano"
)

// Chains lists the sidechains in the order the UI offers them.
var Chains = []string{ChainMain, ChainMini, ChainNano}

type Config struct {
	Wallet string `yaml:"wallet"`
	Mode   Mode   `yaml:"mode"`

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

	// Kindness names how much of the machine the miner may take and how fast it
	// lets go. See internal/kindness.
	Kindness kindness.Level `yaml:"kindness"`

	// ThrottleSensitivity is the pre-kindness control. It is read so old configs
	// migrate, then cleared — omitempty keeps it out of everything we write.
	ThrottleSensitivity Sensitivity `yaml:"throttle_sensitivity,omitempty"`

	// throttle
	MaxThreads       int     `yaml:"max_threads"`
	PauseOnBattery   bool    `yaml:"pause_on_battery"`
	TempLimitCelsius float64 `yaml:"temp_limit_celsius"`

	// ThermalGovernor eases mining off as the CPU approaches TempLimitCelsius,
	// rather than mining flat out and then hard-stopping at the limit. On by
	// default: a machine that idles warm should hold a temperature, not
	// oscillate between full speed and a dead stop.
	ThermalGovernor bool `yaml:"thermal_governor"`

	// PauseOnThrottle is the superseded thermal control. It is parsed so old
	// configs still load, then ignored; the thermal governor replaced it.
	PauseOnThrottle bool `yaml:"pause_on_thermal_throttle,omitempty"`

	// MineOnlyWhenLocked restricts mining to a locked session. Off by default:
	// most people want the machine earning whenever they are not using it, not
	// only when they remembered to lock it.
	MineOnlyWhenLocked bool `yaml:"mine_only_when_locked"`

	// Chart holds the dashboard graph preferences. They change nothing about
	// mining, but they live in the config so the window opens the way the user
	// left it.
	Chart ChartOptions `yaml:"chart"`

	// IdleFullAfterSeconds is how long the keyboard and mouse must be quiet
	// before mining is allowed to use every configured thread. Until then it
	// stays at reduced speed. 0 disables the wait and mines at full speed
	// whenever the CPU is free.
	IdleFullAfterSeconds int `yaml:"idle_full_after_seconds"`

	LogLevel string `yaml:"log_level"`
}

// ChartOptions are the dashboard graph's display preferences.
type ChartOptions struct {
	// ShadeHeadroom shades the band above the kindness ceiling — the part of
	// the machine mining will not touch.
	ShadeHeadroom bool `yaml:"shade_headroom"`
	// MarkBackoff ticks the moments the miner gave CPU back.
	MarkBackoff bool `yaml:"mark_backoff"`
	// DrawTemp overlays CPU temperature.
	DrawTemp bool `yaml:"draw_temperature"`
	// FillMiner fills the area under the miner line.
	FillMiner bool `yaml:"fill_miner"`
	// WindowSeconds is how much history the chart shows.
	WindowSeconds int `yaml:"window_seconds"`
}

// ChartWindows are the spans the graph settings offer, in seconds.
var ChartWindows = []int{30, 60, 120}

func Defaults() *Config {
	return &Config{
		Mode:                 ModeP2PoolRemote,
		P2PoolChain:          ChainMini,
		ManageP2Pool:         true,
		ManageTor:            true,
		Kindness:             kindness.Default,
		PauseOnBattery:       true,
		ThermalGovernor:      true,
		TempLimitCelsius:     95,
		IdleFullAfterSeconds: 300,
		LogLevel:             "info",
		Chart: ChartOptions{
			ShadeHeadroom: true,
			MarkBackoff:   true,
			FillMiner:     true,
			WindowSeconds: 30,
		},
	}
}

// Preset returns the kindness preset the config selects.
func (c *Config) Preset() kindness.Preset { return kindness.Get(c.Kindness) }

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
	// Blanked so an absent `kindness:` key is distinguishable from one set to
	// the default — the difference decides whether the old throttle setting
	// still gets a say.
	cfg.Kindness = ""
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	// A key present but empty (`p2pool_chain:` with nothing after it) overwrites
	// the default with "", which downstream reads as the main chain. Restore the
	// default instead of silently switching sidechains.
	if cfg.P2PoolChain == "" {
		cfg.P2PoolChain = ChainMini
	}
	cfg.migrateKindness()
	if cfg.Chart.WindowSeconds <= 0 {
		cfg.Chart.WindowSeconds = Defaults().Chart.WindowSeconds
	}
	return cfg, nil
}

// migrateKindness carries a pre-kindness config forward. The old
// throttle_sensitivity only applies when the user has not chosen a kindness
// preset; either way the dead key is cleared so the next Save drops it.
func (c *Config) migrateKindness() {
	if c.Kindness == "" {
		if l, ok := kindnessForSensitivity[c.ThrottleSensitivity]; ok {
			c.Kindness = l
		} else {
			c.Kindness = kindness.Default
		}
	}
	// Normalise a stored alias (e.g. the old "greedy") to its current name so
	// the next Save rewrites it and the rest of the app only ever sees canon.
	c.Kindness = kindness.Canonical(c.Kindness)
	c.ThrottleSensitivity = ""
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
	// Empty is what a config written before kindness existed looks like after
	// migration has been skipped (e.g. a struct built by hand in a test), so it
	// is accepted and read as the default rather than rejected.
	if c.Kindness != "" && !kindness.Valid(c.Kindness) {
		return fmt.Errorf("kindness must be one of ghost, polite, balanced, full; got %q", c.Kindness)
	}
	if c.IdleFullAfterSeconds < 0 {
		return fmt.Errorf("idle_full_after_seconds cannot be negative")
	}
	// Empty means "unset" and Load normalises it to mini. A typo must not fall
	// through to the main chain, where a small miner may never earn a payout.
	switch c.P2PoolChain {
	case "", ChainMini, ChainMain, ChainNano:
	default:
		return fmt.Errorf("p2pool_chain must be one of %q, %q or %q, got %q",
			ChainMain, ChainMini, ChainNano, c.P2PoolChain)
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
