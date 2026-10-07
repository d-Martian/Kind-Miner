// Command kind-minerd is kind-miner without a screen: the daemon that turns a
// Nodo, or any headless box, into a miner and, with hub.serve on, the
// household's p2pool hub.
//
// It is built with CGO_ENABLED=0 and never links Fyne: a static binary with no
// C toolchain behind it, which cross-compiles for arm64 and rebuilds
// bit-for-bit anywhere. CI fails if Fyne ever appears in its dependencies (see
// .github/workflows/kind-minerd.yml), because one import of internal/gui would
// quietly bring back cgo, OpenGL and X11 on a box that has none of them.
//
//	kind-minerd init --address 4…   set up for this wallet (with sudo: the service, hub on, pairing code)
//	kind-minerd run                 mine until stopped (what the service runs);
//	                                with no config, wait to be set up from the desktop
//	kind-minerd status [--json]     what the running daemon is doing
//	kind-minerd doctor              one sentence on what is wrong, if anything
//	kind-minerd pair [--name N]     the code that pairs a device with this hub
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/hub"
	"github.com/kind-miner/kind-miner/internal/instance"
	"github.com/kind-miner/kind-miner/internal/msr"
	"github.com/kind-miner/kind-miner/internal/nodo"
	"github.com/kind-miner/kind-miner/internal/rollout"
)

var version = "dev"

const usage = `usage: kind-minerd <command> [flags]

commands:
  init --address 4…   set up for this wallet (with sudo: the service and its hub, then the pairing code)
  run                 mine until stopped
  status [--json]     what the running daemon is doing
  doctor              say what is wrong, if anything
  pair [--name N]     print the code that pairs a device with this hub
  msr on|off|status   opt-in, as root: xmrig's MSR boost (x86 only)
  version             print the version

common flags:
  --config PATH       config file (default: ~/.config/kind-miner/config.yaml)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet("kind-minerd "+cmd, flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	address := fs.String("address", "", "Monero payout address (init)")
	force := fs.Bool("force", false, "overwrite an existing config (init)")
	noHub := fs.Bool("no-hub", false, "don't serve the household hub (init, for the service)")
	asJSON := fs.Bool("json", false, "machine-readable output (status)")
	name := fs.String("name", "", "device name for a plain-xmrig config (pair)")
	host := fs.String("host", "", "address devices reach this hub at (pair; default: this machine's LAN address)")
	_ = fs.Parse(args)
	if p := configFor(*configPath, os.Getenv("CREDENTIALS_DIRECTORY"), os.Getenv("STATE_DIRECTORY"), os.Getuid(), exists); p != "" {
		config.SetPath(p)
	}

	var err error
	switch cmd {
	case "init":
		err = runInit(os.Stdout, *address, *force, *noHub, liveInitEnv(*configPath != ""))
	case "run":
		err = runDaemon()
	case "status":
		os.Exit(runStatus(*asJSON, os.Stdout))
	case "doctor":
		os.Exit(runDoctor(os.Stdout))
	case "pair":
		err = runPair(os.Stdout, *name, *host)
	case "msr":
		err = msr.Run(args, os.Stdout)
	case "version", "--version", "-version":
		fmt.Printf("kind-minerd %s\n", version)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "kind-minerd: %v\n", err)
		os.Exit(1)
	}
}

// The system service's config is in one of two places. /etc/kind-miner is
// root's: written over SSH by init, and handed to the service with
// LoadCredential=, so it can stay root-owned and 0600 — the service's dynamic
// user reads its private copy, never the file. A Nodo set up from the desktop
// app has none; the service wrote its own, in its state directory, the one
// place it can write.
// Variables only so the tests can point them at a temporary directory.
var (
	serviceConfig      = "/etc/kind-miner/config.yaml"
	serviceStateConfig = "/var/lib/kind-miner/config.yaml"
)

// credentialConfig is /etc/kind-miner/config.yaml as the service receives it.
// The unit loads the whole directory, which may be empty, because a
// credential naming a missing file stops the service from starting at all;
// systemd names each file the credential's name, an underscore, and the
// file's.
const credentialConfig = "kind-miner_config.yaml"

// configFor picks the config path. An explicit --config wins. The service
// reads root's config when there is one, else its own; root — setting up or
// checking the service from a shell — and a user asking about a service that
// is set up, the service's; anyone else keeps the per-user default (returned
// as "").
func configFor(flagPath, credentialsDir, stateDir string, uid int, exists func(string) bool) string {
	switch {
	case flagPath != "":
		return flagPath
	case credentialsDir != "" && exists(filepath.Join(credentialsDir, credentialConfig)):
		return filepath.Join(credentialsDir, credentialConfig)
	case stateDir != "":
		return filepath.Join(stateDir, "config.yaml")
	case exists(serviceConfig):
		return serviceConfig
	case exists(serviceStateConfig):
		// Readable by root only: the state directory is the dynamic user's.
		return serviceStateConfig
	case uid == 0:
		return serviceConfig
	}
	return ""
}

// newConfig is a config for address with nothing else asked: the defaults
// the desktop's one-field setup uses, and on a Nodo the Nodo's own node.
func newConfig(address string, isNodo bool) *config.Config {
	cfg := config.Defaults()
	cfg.Wallet = address
	if isNodo {
		// The Nodo profile follows the Nodo's own monerod; see internal/nodo.
		cfg.Mode = config.ModeP2PoolLocal
	}
	return cfg
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// initEnv is what init needs to know about the machine, and the two things
// it does to the service, behind fields so the decisions can be tested
// without root or systemd.
type initEnv struct {
	root             bool
	flagConfig       bool // --config was given: the caller chose the path
	serviceInstalled bool
	isNodo           bool
	restart          func() error
	// pairing prints the pairing code once the restarted service has its
	// hub identity.
	pairing func(w io.Writer) error
}

// liveInitEnv is the real machine.
func liveInitEnv(flagConfig bool) initEnv {
	_, isNodo, _ := nodo.Detect()
	return initEnv{
		root:             os.Getuid() == 0,
		flagConfig:       flagConfig,
		serviceInstalled: serviceInstalled(),
		isNodo:           isNodo,
		restart:          restartService,
		pairing:          printPairingWhenReady,
	}
}

// serviceUnits are where the unit file is when the service is installed:
// the .deb's location, then a hand install's.
var serviceUnits = []string{
	"/usr/lib/systemd/system/kind-minerd.service",
	"/etc/systemd/system/kind-minerd.service",
}

func serviceInstalled() bool {
	for _, u := range serviceUnits {
		if exists(u) {
			return true
		}
	}
	return false
}

// runInit sets kind-minerd up for a wallet, in one command.
//
// For the system service — run as root where it is installed — that is the
// whole setup: the service's config, the household hub turned on (serving the
// house is what a Nodo or a headless box is for; --no-hub opts out), the
// service restarted onto it, and the pairing code for the desktops printed.
// Anywhere else it writes this user's config, as before. Run without root on
// a machine with the service, it refuses: a config in the user's home is one
// the service never reads, and writing it would look like success.
func runInit(w io.Writer, address string, force, noHub bool, env initEnv) error {
	if err := config.CheckAddress(address); err != nil {
		return fmt.Errorf("--address: %w", err)
	}
	forService := config.Path() == serviceConfig
	if env.serviceInstalled && !env.root && !env.flagConfig {
		return fmt.Errorf("kind-minerd is installed as a service; set it up as root:\n\n  sudo kind-minerd init --address %s", address)
	}
	if _, err := os.Stat(config.Path()); err == nil && !force {
		return fmt.Errorf("%s already exists; use --force to replace it", config.Path())
	}
	cfg := newConfig(address, env.isNodo)
	cfg.Hub.Serve = forService && !noHub
	if err := cfg.Save(); err != nil {
		return err
	}
	if !forService || !env.serviceInstalled {
		fmt.Fprintf(w, "Wrote %s. Start mining with: kind-minerd run\n", config.Path())
		return nil
	}
	fmt.Fprintf(w, "Wrote %s.\n", config.Path())
	if err := env.restart(); err != nil {
		return fmt.Errorf("restarting the service: %w (try: sudo systemctl restart kind-minerd)", err)
	}
	fmt.Fprintln(w, "kind-minerd restarted with it, and is starting to mine.")
	if !cfg.Hub.Serve {
		fmt.Fprintln(w, "Follow it with: kind-minerd status")
		return nil
	}
	fmt.Fprintln(w)
	return env.pairing(w)
}

// serviceActive reports whether the system service is running.
func serviceActive() bool {
	return exists("/run/systemd/system") && exec.Command("systemctl", "is-active", "--quiet", "kind-minerd").Run() == nil
}

// restartService restarts the system service, when systemd is running it.
func restartService() error {
	if !exists("/run/systemd/system") {
		return errors.New("systemd is not running")
	}
	out, err := exec.Command("systemctl", "restart", "kind-minerd").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// printPairingWhenReady prints the pairing code once the restarted service
// has its hub identity — at once if it made one while waiting to be set up,
// within seconds of starting otherwise.
func printPairingWhenReady(w io.Writer) error {
	dir := hubDirFor(config.Path())
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(time.Second) {
		if _, err := hub.Load(dir); err == nil {
			return runPair(w, "", "")
		}
	}
	return errors.New("the hub has not started yet; in a minute, get the pairing code with: sudo kind-minerd pair")
}

// runDaemon mines until SIGINT or SIGTERM, first waiting to be set up if
// there is no config. Errors come back rather than exiting mid-way, so
// Shutdown always runs for whatever Start got going.
func runDaemon() error {
	// Started by hand while the service runs, a second daemon would fight it
	// for its ports and fail with a bind error that says nothing useful.
	// systemd sets INVOCATION_ID for the service itself.
	if os.Getenv("INVOCATION_ID") == "" && serviceActive() {
		return errors.New("kind-minerd is already running as the system service; see what it is doing with: kind-minerd status")
	}
	status, release := instance.Claim(instance.AppID, nil)
	if status == instance.Running {
		return errors.New("kind-miner is already running in this session")
	}
	defer release()

	log.Printf("kind-minerd %s starting", version)
	cfg, err := config.Load()
	if errors.Is(err, os.ErrNotExist) {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		cfg, err = awaitSetup(ctx)
		stop()
		if cfg == nil {
			return err
		}
	} else if err != nil {
		return err
	}
	for {
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("%w (edit %s)", err, config.Path())
		}
		if err := mine(cfg); !errors.Is(err, errReload) {
			return err
		}
		if cfg, err = config.Load(); err != nil {
			return err
		}
	}
}

// mine runs one supervisor until a signal, or until it needs starting again
// (errReload): the owner changed the wallet, or new engines failed their
// health check and the previous ones take over.
func mine(cfg *config.Config) error {
	eng := chooseEngines(packagedRoot(), engineStateDir())
	if eng.choice.RolledBack != "" {
		log.Printf("warning: %s", eng.choice.RolledBack)
	}
	sup := core.New(cfg)
	defer sup.Shutdown()
	if use := eng.choice.Use; use != nil {
		sup.UseEngines(use.XMRig, use.P2Pool)
	}
	reload := make(chan struct{}, 1)
	if pushable(config.Path(), os.Getenv("CREDENTIALS_DIRECTORY")) {
		sup.SetHubWallet(func(address string) error { return pushWallet(cfg, address, reload) })
	}
	if err := sup.Start(func(st core.Step) { log.Printf("%s…", st) }); err != nil {
		// New engines that cannot even start have failed already.
		if eng.choice.Probation && eng.reject("it did not start: "+err.Error()) {
			return errReload
		}
		return err
	}
	log.Println("kind-minerd mining. Stop with SIGINT or SIGTERM.")

	stop := make(chan struct{})
	defer close(stop)
	failed := make(chan struct{})
	if eng.choice.Probation {
		log.Printf("%s is new here: checking it works for the next %s", eng.current.Version, rollout.Probation)
		go eng.watchProbation(sup, failed, stop)
	}
	defer keepStatus(func(now time.Time) daemonStatus {
		st := snapshot(sup, now)
		if use := eng.choice.Use; use != nil {
			st.Engines = use.Version
		}
		st.EnginesWarning = eng.warning()
		return st
	})()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	select {
	case <-sig:
		log.Println("kind-minerd stopping")
		return nil
	case <-reload:
		log.Println("The hub's owner changed its wallet from their desktop; starting again to mine to it")
		return errReload
	case <-failed:
		return errReload
	}
}

// statusInterval is how often the status file is refreshed; status treats a
// file older than staleAfter as a daemon that is no longer running.
const (
	statusInterval = 5 * time.Second
	staleAfter     = 30 * time.Second
)
