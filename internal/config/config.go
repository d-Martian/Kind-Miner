package config

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kind-miner/kind-miner/internal/hub"
	"github.com/kind-miner/kind-miner/internal/kindness"
)

type Mode string

const (
	ModeP2PoolRemote Mode = "p2pool-remote"
	ModeP2PoolLocal  Mode = "p2pool-local"
	// ModeHub mines to a household hub — another machine's p2pool, usually a
	// Nodo's — with xmrig alone. No p2pool, no node, no Tor on this machine:
	// the hub does all of that once for the house.
	ModeHub Mode = "hub"
)

// modePool is the removed direct-to-pool mode, kept only so configs written
// while it existed still load. It sent the wallet address to a pool operator,
// took a fee, and connected outside Tor — the opposite of what kind-miner is
// for — and doubled everything that had to be tested. Load moves it to the
// default mode; for a machine too slow for mini, the nano sidechain is what
// pool mode used to be recommended for.
const modePool Mode = "pool"

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

// P2Pool sidechains. nano is the default for new configs: its blocks come every
// 30 seconds against the same 2160-block window, so a share keeps earning for
// 18 hours, and even a laptop lands shares often enough to stay in the window
// and be paid steadily. mini's share difficulty is higher, suiting desktops
// that want a busier chain; main is for large miners. The value is validated
// rather than passed through because an unrecognised chain makes p2pool
// silently join the main chain, where a small miner may never earn a payout.
const (
	ChainMini = "mini"
	ChainMain = "main"
	ChainNano = "nano"
)

// legacyDefaultChain is the sidechain a config that does not name one was
// written to mean: mini, the default before nano.
const legacyDefaultChain = ChainMini

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

	// hub mode: HubCode is the pairing code from kind-minerd pair on the
	// hub, and WorkerName what this machine is called in the hub's list
	// (blank uses the hostname).
	HubCode    string `yaml:"hub_code"`
	WorkerName string `yaml:"worker_name"`

	// Hub makes this machine the household hub; see HubOptions.
	Hub HubOptions `yaml:"hub"`

	// p2pool subprocess (modes A and B)
	ManageP2Pool  bool   `yaml:"manage_p2pool"`
	P2PoolBinPath string `yaml:"p2pool_path"`

	// PoolURL belonged to the removed pool mode. It is read so old configs
	// load, then cleared — omitempty keeps it out of everything we write.
	PoolURL string `yaml:"pool_url,omitempty"`

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

	// MineOnNodo runs the miner as a guest on a Nodo (or any big.LITTLE box
	// whose job is serving a Monero node): the low-power cores first, the big
	// ones only while the node's services are not waiting for CPU, nothing
	// while the node is syncing, and a 70 °C ceiling. Off by default and never
	// inferred — the node is the machine's purpose, and mining on it is the
	// owner's call.
	MineOnNodo bool `yaml:"mine_on_nodo"`

	LogLevel string `yaml:"log_level"`
}

// HubOptions turn on serving p2pool to the rest of the house: stratum over TLS
// on the LAN, and the statistics API paired devices read. Off by default — it
// opens two ports on every interface, and that is the owner's call.
type HubOptions struct {
	Serve bool `yaml:"serve"`
	// Ports; 0 uses hub.DefaultStratumPort and hub.DefaultAPIPort.
	StratumPort int `yaml:"stratum_port"`
	APIPort     int `yaml:"api_port"`
	// Onion also publishes both ports as a Tor onion service, from a Tor
	// instance of the hub's own, so paired laptops keep mining to the hub
	// away from home. Off by default: it runs a second Tor on the hub and
	// puts it on the Tor network, and that is the owner's call. (Anyone who
	// finds the address can only mine to the owner's wallet; the statistics
	// still need the token.)
	Onion bool `yaml:"onion"`
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

// HubPorts returns the hub's stratum and API ports, defaults filled in.
func (c *Config) HubPorts() (stratum, api int) {
	stratum, api = c.Hub.StratumPort, c.Hub.APIPort
	if stratum == 0 {
		stratum = hub.DefaultStratumPort
	}
	if api == 0 {
		api = hub.DefaultAPIPort
	}
	return stratum, api
}

// ChartWindows are the spans the graph settings offer, in seconds.
var ChartWindows = []int{30, 60, 120}

func Defaults() *Config {
	return &Config{
		Mode:                 ModeP2PoolRemote,
		P2PoolChain:          ChainNano,
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
	// Blanked for the same reason: new configs default to nano, but a config
	// without the key was written when mini was the default, and switching
	// its sidechain on load would drop the user out of mini's payout window
	// without a word.
	cfg.P2PoolChain = ""
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	// Absent, or present but empty (`p2pool_chain:` with nothing after it):
	// either way the config never chose, so it keeps what it meant when it was
	// written. "" must never survive, since p2pool reads it as the main chain.
	if cfg.P2PoolChain == "" {
		cfg.P2PoolChain = legacyDefaultChain
	}
	cfg.migrateKindness()
	cfg.migratePoolMode()
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

// migratePoolMode moves a config off the removed pool mode, onto the default:
// p2pool over Tor. It says so once in the log, because unlike a renamed key
// this changes where the machine mines.
func (c *Config) migratePoolMode() {
	if c.Mode == modePool {
		log.Printf("Pool mode has been removed; mining with p2pool (%s) instead. "+
			"On a machine under about 1 kH/s, set p2pool_chain: nano.", ModeP2PoolRemote)
		c.Mode = ModeP2PoolRemote
	}
	c.PoolURL = ""
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
	switch c.Mode {
	case ModeP2PoolRemote, ModeP2PoolLocal:
		if c.Wallet == "" {
			return fmt.Errorf("wallet address is required")
		}
	case ModeHub:
		// The hub pays its owner's wallet; this machine's is not used.
		if strings.TrimSpace(c.HubCode) == "" {
			return fmt.Errorf("hub mode needs hub_code: run kind-minerd pair on the hub and paste what it prints")
		}
		if _, err := hub.ParseCode(c.HubCode); err != nil {
			return fmt.Errorf("hub_code: %w", err)
		}
		if c.Hub.Serve {
			return fmt.Errorf("a machine mining to a hub cannot also be one (turn off hub.serve, or use a p2pool mode)")
		}
	default:
		return fmt.Errorf("mode must be p2pool-remote, p2pool-local or hub")
	}
	if c.Hub.Serve && !c.ManageP2Pool {
		return fmt.Errorf("hub.serve needs manage_p2pool: the hub serves the p2pool kind-miner runs")
	}
	for _, port := range []int{c.Hub.StratumPort, c.Hub.APIPort} {
		if port < 0 || port > 65535 {
			return fmt.Errorf("hub ports must be between 1 and 65535, got %d", port)
		}
	}
	// Empty is what a config written before kindness existed looks like after
	// migration has been skipped (e.g. a struct built by hand in a test), so it
	// is accepted and read as the default rather than rejected.
	if c.Kindness != "" && !kindness.Valid(c.Kindness) {
		return fmt.Errorf("kindness must be one of ghost, polite, balanced, full; got %q", c.Kindness)
	}
	// Checked here rather than left to node selection: an unparseable address
	// is not transient, and without this guard it surfaces only after the
	// binaries have been downloaded and Tor started — a minute of apparent
	// hang for a missing ":18089". nodes.parseAddr stays the authority on the
	// format; this rejects exactly what it would reject.
	if c.Mode == ModeP2PoolRemote && c.RemoteNode != "" {
		_, port, err := net.SplitHostPort(c.RemoteNode)
		if err != nil {
			return fmt.Errorf("remote_node %q must be host:port (e.g. 192.168.8.192:18089)", c.RemoteNode)
		}
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("remote_node %q has an invalid port %q", c.RemoteNode, port)
		}
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
