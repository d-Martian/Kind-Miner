package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/gui"
)

var version = "dev"

func main() {
	flags := flag.NewFlagSet("kind-miner", flag.ExitOnError)
	showVersion := flags.Bool("version", false, "print version and exit")
	configPath := flags.String("config", "", "path to config file (default: ~/.config/kind-miner/config.yaml)")
	noTray := flags.Bool("no-tray", false, "run headless without any GUI (logs to stdout)")
	headless := flags.Bool("headless", false, "alias for -no-tray")
	forceGUI := flags.Bool("gui", false, "force the graphical interface even from a terminal")
	_ = flags.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("kind-miner %s\n", version)
		return
	}
	if *configPath != "" {
		config.SetPath(*configPath)
	}

	mode := decideMode(*noTray || *headless, *forceGUI, isTerminal(), displayAvailable())
	if *forceGUI && mode != modeGUI {
		log.Println("--gui requested but no display is available; running headless instead.")
	}

	cfg, loadErr := config.Load()
	firstRun := errors.Is(loadErr, os.ErrNotExist) || (loadErr != nil && os.IsNotExist(loadErr))

	if mode == modeGUI {
		runGUI(cfg, loadErr, firstRun)
		return
	}
	runTerminal(mode, cfg, loadErr, firstRun)
}

// runGUI launches the full graphical interface. First-run onboarding happens
// inside the GUI; an unreadable or invalid existing config is shown as an
// error window rather than a silent exit.
func runGUI(cfg *config.Config, loadErr error, firstRun bool) {
	switch {
	case firstRun:
		cfg = config.Defaults() // wallet collected by the onboarding screen
	case loadErr != nil:
		gui.RunConfigError(loadErr)
		return
	default:
		if err := cfg.Validate(); err != nil {
			gui.RunConfigError(fmt.Errorf("%w\n\nEdit %s, then restart kind-miner.", err, config.Path()))
			return
		}
	}
	setupLogging(cfg)

	sup := core.New(cfg)

	// Clean teardown on Ctrl-C / SIGTERM even in GUI mode: quit the GUI (which
	// deregisters the tray icon) and fall through to Shutdown so the p2pool and
	// XMRig subprocesses stop too.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		gui.Quit()
	}()

	// Deferred so subprocesses are also stopped when the GUI panics (e.g. a
	// display/GL init failure) — a plain call after RunGraphical would be
	// skipped during unwind, leaving p2pool and XMRig running headless.
	defer sup.Shutdown()
	gui.RunGraphical(sup, firstRun)
}

// runTerminal handles the tray and headless launch paths. It preserves the
// historical terminal experience: synchronous startup with progress on stdout,
// a system tray (tray mode) or a plain signal wait (headless mode).
func runTerminal(mode uiMode, cfg *config.Config, loadErr error, firstRun bool) {
	if firstRun {
		var err error
		cfg, err = firstRunTerminal()
		if err != nil {
			fatal(err)
		}
		if cfg == nil {
			return // template written; user must edit and restart
		}
	} else if loadErr != nil {
		fatal(loadErr)
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Edit %s to fix the issue, then restart kind-miner.\n", config.Path())
		os.Exit(1)
	}
	setupLogging(cfg)

	sup := core.New(cfg)
	// Deferred (not just called at the end) so the subprocesses die with us on
	// a GUI panic; the explicit call before fatal() is needed because os.Exit
	// skips deferred functions.
	defer sup.Shutdown()
	if err := sup.Start(nil); err != nil {
		sup.Shutdown() // Start doesn't tear down what it already launched
		fatal(err)
	}

	// Log scheduler state changes to stdout, as the terminal build always has.
	go func() {
		for ev := range sup.Scheduler().Events {
			if ev.Reason != "" {
				log.Printf("state: %s (%s)", ev.State, ev.Reason)
			} else {
				log.Printf("state: %s", ev.State)
			}
		}
	}()

	// Catch SIGINT/SIGTERM so Shutdown runs on a clean exit.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if mode == modeTray {
		go func() {
			<-sigCh
			gui.Quit()
		}()
		gui.RunTray(sup) // blocks until Quit clicked or the signal above fires
	} else {
		log.Println("kind-miner running (headless). Press Ctrl+C to stop.")
		<-sigCh
	}
}

// firstRunTerminal runs the text wizard when a terminal is attached. With no
// terminal (e.g. a systemd service with no display) it writes a config
// template and returns (nil, nil) so the caller exits cleanly with a hint.
func firstRunTerminal() (*config.Config, error) {
	if isTerminal() {
		return config.RunWizard()
	}
	cfg := config.Defaults()
	if err := cfg.Save(); err != nil {
		return nil, fmt.Errorf("creating default config: %w", err)
	}
	log.Printf("No terminal and no display detected. A config template was written to:\n  %s\nSet your wallet address and restart kind-miner.", config.Path())
	return nil, nil
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

// isTerminal reports whether stdin is attached to a terminal, distinguishing a
// shell launch from a desktop (double-click / .desktop) launch.
func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
