package core

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/hub"
	"github.com/kind-miner/kind-miner/internal/nodo"
)

// HubDir is where the hub keeps its certificate and API token: beside the
// binaries in the state directory, which on the system service is the one
// place its dynamic user can write.
func HubDir() string {
	return filepath.Join(filepath.Dir(autoinstall.BinDir()), "hub")
}

// hubServing reports whether this machine serves the household hub.
func hubServing(cfg *config.Config) bool {
	return cfg.Hub.Serve && cfg.ManageP2Pool && cfg.Mode != config.ModeHub
}

// startHubAPI serves the household statistics once p2pool is up, and answers
// desktops looking for a hub on the LAN. A failure is logged, not returned:
// the API is what paired desktops read their numbers from, but the devices
// mine to stratum, which p2pool already serves — a busy API port must not
// stop the house mining.
func (s *Supervisor) startHubAPI(id hub.Identity) {
	stratumPort, apiPort := s.cfg.HubPorts()
	hello := HubHello()
	srv, err := hub.Serve(fmt.Sprintf(":%d", apiPort), id, hub.Handlers{
		Household: s.household,
		Hello:     hello,
		SetWallet: s.hubWallet,
	})
	if err != nil {
		log.Printf("hub: statistics API not available on port %d: %v", apiPort, err)
		return
	}
	hello.SetUp = true // a hub mining has its config
	b := hub.Beacon{Hello: hello, APIPort: apiPort, Fingerprint: id.FingerprintHex()}
	beacon, err := hub.Announce(hub.DefaultAPIPort, func() hub.Beacon { return b })
	if err != nil {
		// Only finding the hub needs it; pairing by code still works.
		log.Printf("hub: desktops cannot find this hub by searching (UDP port %d: %v)", hub.DefaultAPIPort, err)
	}
	s.mu.Lock()
	s.hubAPI, s.hubBeacon = srv, beacon
	s.mu.Unlock()
	log.Printf("Serving the household hub: stratum on port %d (TLS), statistics on %d. "+
		"Pair a device with: kind-minerd pair", stratumPort, apiPort)
}

// HubHello is how a hub on this machine introduces itself: by hostname, and
// as a Nodo when it is one, which is what a desktop's list of hubs shows.
func HubHello() hub.Hello {
	h := hub.Hello{Name: "kind-miner hub"}
	if host, err := os.Hostname(); err == nil && host != "" {
		h.Name = host
	}
	_, h.Nodo, _ = nodo.Detect()
	return h
}

// SetHubWallet lets the hub's owner change its wallet from their desktop:
// set is called with the new address and does whatever applying it takes.
// Unset, the API refuses — the GUI never sets it, and kind-minerd leaves it
// unset when its config is root's file. Call before Start.
func (s *Supervisor) SetHubWallet(set func(address string) error) { s.hubWallet = set }

// startHubOnion publishes the hub's two ports as an onion service, from a
// Tor of the hub's own: its own torrc and data directory in the state
// directory, no SOCKS port, nothing shared with a system Tor. A Nodo's setup
// replaces /etc/tor/torrc with its own, which would silently delete a hidden
// service written there, and the service's dynamic user could not write it
// anyway.
//
// It runs in the background and never fails Start: the onion is for devices
// away from home, and the house mines over the LAN without it.
func (s *Supervisor) startHubOnion() {
	bin, err := s.torBinary()
	if err != nil {
		log.Printf("hub: no onion service — could not obtain tor: %v", err)
		return
	}
	stratumPort, apiPort := s.cfg.HubPorts()
	t := engine.NewTor(bin, filepath.Join(HubDir(), "tor"), 0)
	t.ServeOnion(hub.OnionDir(HubDir()), stratumPort, apiPort)
	s.mu.Lock()
	s.hubTor = t
	s.mu.Unlock()
	go func() {
		if err := t.Start(3 * time.Minute); err != nil {
			log.Printf("hub: onion service not available: %v", err)
			return
		}
		if onion, err := hub.ReadOnion(HubDir()); err == nil && onion != "" {
			log.Printf("hub: also reachable as %s (ports %d and %d). "+
				"Pair again with kind-minerd pair to give devices the onion.", onion, stratumPort, apiPort)
		}
	}()
}

// HubOnion is the hub's onion address, or "" when it serves none (yet).
func (s *Supervisor) HubOnion() string {
	if !hubServing(s.cfg) || !s.cfg.Hub.Onion {
		return ""
	}
	onion, _ := hub.ReadOnion(HubDir())
	return onion
}

// household is what the hub's API answers with.
func (s *Supervisor) household() hub.Household {
	h := hub.Household{UpdatedAt: time.Now(), Chain: s.cfg.P2PoolChain, Payouts: s.payouts.Recent(10)}
	if s.p2pool != nil {
		h.Stats, h.StatsOK = s.p2pool.Stats()
		h.Workers = hub.ParseWorkers(h.Stats.Workers)
	}
	return h
}

// Workers lists the devices mining to this machine's p2pool — the hub's
// household, or just this machine's own miner. Nil when p2pool is not ours.
func (s *Supervisor) Workers() []hub.Worker {
	if s.p2pool == nil {
		return nil
	}
	st, ok := s.p2pool.Stats()
	if !ok {
		return nil
	}
	return hub.ParseWorkers(st.Workers)
}

// workerName is what this machine logs in to a hub as.
func workerName(cfg *config.Config) string {
	if n := hub.WorkerName(cfg.WorkerName); n != "" {
		return n
	}
	if host, err := os.Hostname(); err == nil {
		if n := hub.WorkerName(host); n != "" {
			return n
		}
	}
	return "desktop"
}

// householdInterval is how often a paired device asks the hub for the
// household's numbers. p2pool itself refreshes them about this often.
const householdInterval = 30 * time.Second

// watchHousehold keeps the latest household statistics from the hub, until
// stop closes. Payouts the hub saw are added to this machine's record, which
// is what puts them in the tray: they are the household's, paid to the hub
// owner's wallet, and the payout list says what it has seen, not whose.
func (s *Supervisor) watchHousehold(p hub.Pairing, stop <-chan struct{}) {
	t := time.NewTicker(householdInterval)
	defer t.Stop()
	for {
		// Long enough for a Tor circuit to an onion service to be built.
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		h, viaTor, err := hub.Fetch(ctx, p, torSOCKSFor(p))
		cancel()
		s.mu.Lock()
		if err == nil {
			s.hh, s.hhOK = h, true
			// Which way the hub answered is the best view of which way the
			// miner reaches it: the two routes fail and recover together.
			s.nodeAddr, s.viaTor = p.StratumAddr(), viaTor
			if viaTor {
				s.nodeAddr = p.OnionStratumAddr()
			}
		}
		s.hhErr = err
		s.mu.Unlock()
		if err != nil {
			log.Printf("hub: cannot read the household's statistics: %v", err)
		}
		for _, pay := range h.Payouts {
			s.payouts.Record(pay.Height, pay.Atomic, pay.SeenAt)
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// torSOCKSFor is the Tor a paired device reaches the hub's onion through:
// the one ensureTor made available, when the hub has an onion at all.
func torSOCKSFor(p hub.Pairing) string {
	if p.Onion == "" {
		return ""
	}
	return fmt.Sprintf("127.0.0.1:%d", torSOCKSPort)
}

// Household returns the latest statistics from the hub this machine mines to.
// ok is false until the hub has answered once; err is the last attempt's
// failure, if it failed — the numbers stay those of the last success.
func (s *Supervisor) Household() (h hub.Household, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hh, s.hhOK, s.hhErr
}

// PoolStats returns the p2pool statistics this machine's earnings are
// estimated from: its own p2pool's, or on a paired device the hub's.
func (s *Supervisor) PoolStats() (engine.P2PoolStats, bool) {
	if s.p2pool != nil {
		return s.p2pool.Stats()
	}
	h, ok, _ := s.Household()
	if !ok || !h.StatsOK {
		return engine.P2PoolStats{}, false
	}
	return h.Stats, true
}

// Hubbed reports whether this machine mines to a household hub.
func (s *Supervisor) Hubbed() bool { return s.cfg.Mode == config.ModeHub }
