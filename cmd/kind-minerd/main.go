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
//	kind-minerd init --address 4…   write a config for this wallet
//	kind-minerd run                 mine until stopped (what the service runs)
//	kind-minerd status [--json]     what the running daemon is doing
//	kind-minerd doctor              one sentence on what is wrong, if anything
//	kind-minerd pair [--name N]     the code that pairs a device with this hub
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/instance"
	"github.com/kind-miner/kind-miner/internal/nodo"
)

var version = "dev"

const usage = `usage: kind-minerd <command> [flags]

commands:
  init --address 4…   write a config for this wallet
  run                 mine until stopped
  status [--json]     what the running daemon is doing
  doctor              say what is wrong, if anything
  pair [--name N]     print the code that pairs a device with this hub
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
	asJSON := fs.Bool("json", false, "machine-readable output (status)")
	name := fs.String("name", "", "device name for a plain-xmrig config (pair)")
	host := fs.String("host", "", "address devices reach this hub at (pair; default: this machine's LAN address)")
	_ = fs.Parse(args)
	if p := configFor(*configPath, os.Getenv("CREDENTIALS_DIRECTORY"), os.Getuid(), exists); p != "" {
		config.SetPath(p)
	}

	var err error
	switch cmd {
	case "init":
		err = runInit(*address, *force)
	case "run":
		err = runDaemon()
	case "status":
		os.Exit(runStatus(*asJSON, os.Stdout))
	case "doctor":
		os.Exit(runDoctor(os.Stdout))
	case "pair":
		err = runPair(os.Stdout, *name, *host)
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

// serviceConfig is where the system service's config lives. The unit hands it
// to the daemon with LoadCredential=, so it can stay root-owned and 0600: the
// service's dynamic user reads its private copy, never this file.
const serviceConfig = "/etc/kind-miner/config.yaml"

// configFor picks the config path. An explicit --config wins; the service
// reads the credential systemd passed it; root — setting up or checking the
// service from a shell — uses the service's config; anyone else keeps the
// per-user default (returned as "").
func configFor(flagPath, credentialsDir string, uid int, exists func(string) bool) string {
	switch {
	case flagPath != "":
		return flagPath
	case credentialsDir != "":
		return filepath.Join(credentialsDir, "config.yaml")
	case uid == 0:
		return serviceConfig
	case exists(serviceConfig):
		// A user running status or doctor on a box where the service is set
		// up is asking about the service.
		return serviceConfig
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runInit writes a config for the given wallet. It asks nothing else: the
// defaults are the same ones the desktop's one-field setup uses, and on a Nodo
// the node is the Nodo's own.
func runInit(address string, force bool) error {
	if err := config.CheckAddress(address); err != nil {
		return fmt.Errorf("--address: %w", err)
	}
	if _, err := os.Stat(config.Path()); err == nil && !force {
		return fmt.Errorf("%s already exists; use --force to replace it", config.Path())
	}
	cfg := config.Defaults()
	cfg.Wallet = address
	if _, isNodo, _ := nodo.Detect(); isNodo {
		// The Nodo profile follows the Nodo's own monerod; see internal/nodo.
		cfg.Mode = config.ModeP2PoolLocal
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Wrote %s. Start mining with: kind-minerd run\n", config.Path())
	return nil
}

// runDaemon mines until SIGINT or SIGTERM. Errors come back rather than
// exiting mid-way, so Shutdown always runs for whatever Start got going.
func runDaemon() error {
	cfg, err := config.Load()
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no config at %s; run: kind-minerd init --address 4…", config.Path())
	}
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("%w (edit %s)", err, config.Path())
	}

	status, release := instance.Claim(instance.AppID, nil)
	if status == instance.Running {
		return errors.New("kind-miner is already running in this session")
	}
	defer release()

	sup := core.New(cfg)
	defer sup.Shutdown()
	log.Printf("kind-minerd %s starting", version)
	if err := sup.Start(func(st core.Step) { log.Printf("%s…", st) }); err != nil {
		return err
	}
	log.Println("kind-minerd mining. Stop with SIGINT or SIGTERM.")

	stop := make(chan struct{})
	defer close(stop)
	go writeStatusEvery(sup, statusPath(), statusInterval, stop)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("kind-minerd stopping")
	return nil
}

// statusInterval is how often the status file is refreshed; status treats a
// file older than staleAfter as a daemon that is no longer running.
const (
	statusInterval = 5 * time.Second
	staleAfter     = 30 * time.Second
)
