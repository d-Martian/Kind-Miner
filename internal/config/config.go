package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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
		P2PoolChain:         "mini",
		ManageP2Pool:        true,
		ThrottleSensitivity: SensitivityMedium,
		PauseOnBattery:      true,
		TempLimitCelsius:    95,
		LogLevel:            "info",
	}
}

func Dir() string {
	if runtime.GOOS == "windows" {
		dir, _ := os.UserConfigDir()
		return filepath.Join(dir, "kind-miner")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "kind-miner")
}

func Path() string {
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
	return nil
}

// RunWizard interactively collects the minimum required settings and writes
// a config file. It writes to stdout/stdin so it must only be called when
// a terminal is attached.
func RunWizard() (*Config, error) {
	cfg := Defaults()
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("kind-miner — first-run setup")
	fmt.Println("─────────────────────────────")
	fmt.Println()

	cfg.Wallet = prompt(scanner, "Your Monero wallet address: ", "")
	if cfg.Wallet == "" {
		return nil, fmt.Errorf("wallet address is required")
	}

	fmt.Println()
	fmt.Println("Connection mode:")
	fmt.Println("  1  Remote node + P2Pool  (recommended — decentralized, no local setup)")
	fmt.Println("  2  Local node + P2Pool   (fully trustless — requires ~180 GB free disk)")
	fmt.Println("  3  Traditional pool      (simplest — small fee applies)")
	choice := prompt(scanner, "Choice [1]: ", "1")
	switch choice {
	case "2":
		cfg.Mode = ModeP2PoolLocal
		fmt.Println()
		fmt.Println("  kind-miner can manage your monerod process for you.")
		fmt.Println("  You will need the monerod binary installed and ~180 GB free.")
		ans := prompt(scanner, "  Manage monerod automatically? [y/N]: ", "n")
		cfg.ManageMonerod = strings.ToLower(ans) == "y"
	case "3":
		cfg.Mode = ModePool
		cfg.PoolURL = prompt(scanner, "  Pool URL (e.g. pool.supportxmr.com:3333): ", "")
		if cfg.PoolURL == "" {
			return nil, fmt.Errorf("pool URL is required for pool mode")
		}
	default:
		cfg.Mode = ModeP2PoolRemote
	}

	fmt.Println()
	fmt.Println("Throttle sensitivity (how quickly kind-miner backs off when your system gets busy):")
	fmt.Println("  1  low     — backs off only when CPU is very busy")
	fmt.Println("  2  medium  — balanced default (recommended)")
	fmt.Println("  3  high    — backs off early, maximum responsiveness")
	sens := prompt(scanner, "Choice [2]: ", "2")
	switch sens {
	case "1":
		cfg.ThrottleSensitivity = SensitivityLow
	case "3":
		cfg.ThrottleSensitivity = SensitivityHigh
	default:
		cfg.ThrottleSensitivity = SensitivityMedium
	}

	fmt.Println()
	threadsStr := prompt(scanner, "Max CPU threads (0 = auto, half of available cores) [0]: ", "0")
	n, err := strconv.Atoi(threadsStr)
	if err == nil {
		cfg.MaxThreads = n
	}

	if err := cfg.Save(); err != nil {
		return nil, fmt.Errorf("saving config: %w", err)
	}
	fmt.Printf("\nConfig saved to %s\n\n", Path())
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
