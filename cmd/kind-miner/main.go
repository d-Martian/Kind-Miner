package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/nodes"
	"github.com/kind-miner/kind-miner/internal/scheduler"
	"github.com/kind-miner/kind-miner/internal/tray"
)

var version = "dev"

func main() {
	flags := flag.NewFlagSet("kind-miner", flag.ExitOnError)
	showVersion := flags.Bool("version", false, "print version and exit")
	configPath := flags.String("config", "", "path to config file (default: ~/.config/kind-miner/config.yaml)")
	noTray := flags.Bool("no-tray", false, "run without system tray (logs to stdout)")
	_ = flags.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("kind-miner %s\n", version)
		return
	}

	if *configPath != "" {
		// Override the default config path by symlinking; for now just note it.
		// A proper implementation would pass the path through config.Load.
		log.Printf("note: -config flag not yet wired; using default path %s", config.Path())
	}

	cfg, err := config.Load()
	if errors.Is(err, os.ErrNotExist) || (err != nil && os.IsNotExist(err)) {
		cfg, err = firstRun()
		if err != nil {
			fatal(err)
		}
	} else if err != nil {
		fatal(err)
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Edit %s to fix the issue, then restart kind-miner.\n", config.Path())
		os.Exit(1)
	}

	setupLogging(cfg)

	// Auto-download any missing binaries before we need them.
	binDir := autoinstall.BinDir()
	xmrigPath, err := autoinstall.EnsureXMRig(binDir)
	if err != nil {
		fatal(err)
	}
	// Use the config-specified path if set, otherwise use the auto-installed one.
	if cfg.XMRigBinPath == "" {
		cfg.XMRigBinPath = xmrigPath
	}

	if cfg.Mode != config.ModePool && cfg.ManageP2Pool {
		p2poolPath, err := autoinstall.EnsureP2Pool(binDir)
		if err != nil {
			fatal(err)
		}
		if cfg.P2PoolBinPath == "" {
			cfg.P2PoolBinPath = p2poolPath
		}
	}

	poolURL, err := resolvePool(cfg)
	if err != nil {
		fatal(err)
	}

	// Start optional managed subprocesses.
	var monerodProc *engine.Monerod
	if cfg.Mode == config.ModeP2PoolLocal && cfg.ManageMonerod {
		monerodProc = engine.NewMonerod(cfg.MonerodPath)
		log.Println("Starting monerod (this may take a while for initial sync)…")
		if err := monerodProc.Start(5 * time.Minute); err != nil {
			fatal(err)
		}
		defer monerodProc.Stop()
	}

	var p2poolProc *engine.P2Pool
	if cfg.Mode != config.ModePool && cfg.ManageP2Pool {
		node, err := resolveNode(cfg)
		if err != nil {
			fatal(err)
		}
		log.Printf("Using monerod node: %s", node.Addr())

		var socks5Proxy string
		if node.TorOnly || strings.HasSuffix(node.Host, ".onion") {
			socks5Proxy = "127.0.0.1:9050"
			log.Printf("Routing p2pool through Tor (%s)", socks5Proxy)
		}

		p2poolProc = engine.NewP2Pool(
			cfg.P2PoolBinPath,
			cfg.Wallet,
			node.Host,
			node.RPCPort,
			node.ZMQPort,
			cfg.P2PoolChain,
			3333,
			socks5Proxy,
		)
		log.Println("Starting p2pool (syncing sidechain…)")
		if err := p2poolProc.Start(3 * time.Minute); err != nil {
			fatal(err)
		}
		defer p2poolProc.Stop()
		log.Println("p2pool ready.")
	}

	xmrig := engine.NewXMRig(cfg.XMRigBinPath, poolURL, cfg.Wallet, cfg.MaxThreads, 8080)
	if err := xmrig.Start(); err != nil {
		fatal(err)
	}

	sched := scheduler.New(cfg, xmrig)

	// Log state changes to stdout.
	go func() {
		for ev := range sched.Events {
			if ev.Reason != "" {
				log.Printf("state: %s (%s)", ev.State, ev.Reason)
			} else {
				log.Printf("state: %s", ev.State)
			}
		}
	}()

	go sched.Start()

	// Catch SIGINT/SIGTERM so defers (Stop calls) run on clean exit.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if *noTray {
		log.Println("kind-miner running (no tray). Press Ctrl+C to stop.")
		<-sigCh
		sched.Stop()
	} else {
		// Run tray on main goroutine; also watch for OS signals.
		quitCh := make(chan struct{})
		go func() {
			select {
			case <-sigCh:
				tray.Quit()
			case <-quitCh:
			}
		}()
		t := tray.New(cfg, sched, xmrig)
		t.Run() // blocks until Quit clicked or OS signal above fires
		close(quitCh)
		sched.Stop()
	}
}

// firstRun guides the user through initial setup.
// If no terminal is attached, it writes a default config and tells the user
// to edit it before re-running.
func firstRun() (*config.Config, error) {
	if !isTerminal() {
		cfg := config.Defaults()
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("creating default config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "kind-miner: no config found.\n")
		fmt.Fprintf(os.Stderr, "A default config has been written to:\n  %s\n\n", config.Path())
		fmt.Fprintf(os.Stderr, "Edit it to set your wallet address, then restart kind-miner.\n")
		os.Exit(0)
	}
	return config.RunWizard()
}

// resolvePool returns the XMRig pool URL based on the configured mode.
func resolvePool(cfg *config.Config) (string, error) {
	switch cfg.Mode {
	case config.ModePool:
		return cfg.PoolURL, nil
	default:
		// p2pool modes: XMRig connects to the local p2pool Stratum port.
		return "127.0.0.1:3333", nil
	}
}

// resolveNode returns the monerod node for p2pool to connect to.
// For remote modes it ensures Tor is running before selecting a node,
// since the primary node is only reachable via .onion.
// Retries up to 3 times with a 30s delay to handle transient Tor circuit
// failures or a briefly-rebooting Nodo.
func resolveNode(cfg *config.Config) (nodes.Node, error) {
	switch cfg.Mode {
	case config.ModeP2PoolLocal:
		return nodes.Node{Host: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083}, nil
	default:
		if !nodes.TorAvailable() {
			log.Println("Tor not running — attempting to start it automatically…")
			if nodes.EnsureTor() {
				log.Println("Tor is now running.")
			} else {
				log.Println("Could not start Tor automatically; will try clearnet nodes.")
			}
		}
		const maxAttempts = 3
		const retryDelay = 30 * time.Second
		var lastErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			node, err := nodes.SelectBest(cfg.RemoteNode)
			if err == nil {
				return node, nil
			}
			lastErr = err
			if attempt < maxAttempts {
				log.Printf("Node selection failed (attempt %d/%d): %v", attempt, maxAttempts, err)
				log.Printf("Retrying in %s…", retryDelay)
				time.Sleep(retryDelay)
			}
		}
		return nodes.Node{}, lastErr
	}
}

func setupLogging(cfg *config.Config) {
	switch cfg.LogLevel {
	case "debug", "info":
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	default:
		log.SetFlags(log.LstdFlags)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "kind-miner: %v\n", err)
	os.Exit(1)
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
