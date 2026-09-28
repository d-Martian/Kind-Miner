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

// startHubAPI serves the household statistics once p2pool is up. A failure is
// logged, not returned: the API is what paired desktops read their numbers
// from, but the devices mine to stratum, which p2pool already serves — a busy
// API port must not stop the house mining.
func (s *Supervisor) startHubAPI(id hub.Identity) {
	_, apiPort := s.cfg.HubPorts()
	srv, err := hub.Serve(fmt.Sprintf(":%d", apiPort), id, s.household)
	if err != nil {
		log.Printf("hub: statistics API not available on port %d: %v", apiPort, err)
		return
	}
	s.mu.Lock()
	s.hubAPI = srv
	s.mu.Unlock()
	stratumPort, _ := s.cfg.HubPorts()
	log.Printf("Serving the household hub: stratum on port %d (TLS), statistics on %d. "+
		"Pair a device with: kind-minerd pair", stratumPort, apiPort)
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
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		h, err := hub.Fetch(ctx, p)
		cancel()
		s.mu.Lock()
		if err == nil {
			s.hh, s.hhOK = h, true
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
