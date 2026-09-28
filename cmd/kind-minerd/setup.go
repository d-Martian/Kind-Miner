package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/hub"
)

// A Nodo owner should not need SSH. A daemon started with no config does not
// fail; it waits to be set up from the desktop app, which finds it on the LAN
// and sends the one thing it cannot default — the wallet address. The daemon
// then writes its own config, turns the hub on, and starts mining; the desktop
// gets back the pairing code to mine to it with, and the owner token that
// lets it change the wallet later.

// stateAwaitingSetup is the status a daemon waiting for its first config
// reports, so status and doctor say what it is waiting for rather than that
// it is not running.
const stateAwaitingSetup = "waiting to be set up"

// awaitingReason says how to set the daemon up, for the log and for status.
const awaitingReason = "in the kind-miner desktop app, Settings → Connection → Find a hub; or over SSH, kind-minerd init --address 4…"

// setupPort is where a daemon waiting to be set up serves: the API's default
// port, the one the desktop's search asks on. A variable for the tests.
var setupPort = hub.DefaultAPIPort

// awaitSetup serves a hub with no config until a desktop sets it up, and
// returns the config it wrote. It returns nil, nil if ctx ends first.
func awaitSetup(ctx context.Context) (*config.Config, error) {
	id, err := hub.Ensure(core.HubDir())
	if err != nil {
		return nil, fmt.Errorf("making the hub's certificate: %w", err)
	}
	hello := core.HubHello()
	got := make(chan *config.Config, 1)
	srv, err := hub.Serve(fmt.Sprintf(":%d", setupPort), id, hub.Handlers{
		Hello: hello,
		Setup: func(address string) (hub.Pairing, error) {
			cfg, err := setupConfig(address, hello.Nodo)
			if err != nil {
				return hub.Pairing{}, err
			}
			if err := cfg.Save(); err != nil {
				return hub.Pairing{}, fmt.Errorf("saving the config: %w", err)
			}
			got <- cfg
			// The desktop replaces the host with the one it reached us at;
			// this one only has to parse.
			host, err := lanAddress()
			if err != nil {
				host = "127.0.0.1"
			}
			stratum, api := cfg.HubPorts()
			return id.PairingFor(host, "", stratum, api), nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("no config at %s, and the port to be set up on is busy (%w); run: kind-minerd init --address 4…", config.Path(), err)
	}
	defer srv.Close()
	beacon, err := hub.Announce(setupPort, func() hub.Beacon {
		return hub.Beacon{Hello: hello, APIPort: setupPort, Fingerprint: id.FingerprintHex()}
	})
	if err != nil {
		log.Printf("desktops cannot find this machine by searching (UDP port %d: %v); give them its address instead", setupPort, err)
	} else {
		defer beacon.Close()
	}
	log.Printf("No config at %s yet. Waiting to be set up: %s", config.Path(), awaitingReason)

	defer keepStatus(func(now time.Time) daemonStatus {
		return daemonStatus{Version: version, UpdatedAt: now, State: stateAwaitingSetup, Reason: awaitingReason}
	})()

	select {
	case <-ctx.Done():
		return nil, nil
	case cfg := <-got:
		log.Printf("Set up from the desktop app: mining to %s…, with the hub on", cfg.Wallet[:8])
		return cfg, nil
	}
}

// setupConfig is the config a desktop's setup writes: what init writes, with
// the hub on, since serving the house is what the desktop set it up to do.
func setupConfig(address string, isNodo bool) (*config.Config, error) {
	if err := config.CheckAddress(address); err != nil {
		return nil, err
	}
	cfg := newConfig(address, isNodo)
	cfg.Hub.Serve = true
	return cfg, cfg.Validate()
}

// errReload is returned by a wallet change to end the running supervisor, so
// the daemon starts again on the new config.
var errReload = errors.New("config changed")

// pushWallet applies the owner's wallet change: saved first, so a daemon
// restarted for any reason comes back on it, then a restart, since p2pool
// takes its wallet only on the command line. The file is re-read rather than
// the running config saved: the supervisor fills in engine paths at start,
// and those are this run's choice, not the owner's config.
func pushWallet(cfg *config.Config, address string, reload chan<- struct{}) error {
	if err := config.CheckAddress(address); err != nil {
		return err
	}
	if address == cfg.Wallet {
		return nil
	}
	next, err := config.Load()
	if err != nil {
		return fmt.Errorf("reading the config: %w", err)
	}
	next.Wallet = address
	if err := next.Save(); err != nil {
		return fmt.Errorf("saving the config: %w", err)
	}
	select {
	case reload <- struct{}{}:
	default: // a restart is already coming, and it reads the file
	}
	return nil
}

// pushable reports whether the hub's owner may change the config at path from
// their desktop. The one they may not is root's, handed to the service as a
// credential: it was written over SSH, and SSH is where it is changed.
func pushable(path, credentialsDir string) bool {
	return credentialsDir == "" || filepath.Dir(path) != filepath.Clean(credentialsDir)
}
