package core

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kind-miner/kind-miner/internal/advisory"
	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/nodes"
)

// The advisory is checked once a day, through Tor and never around it. Its
// verdict is re-applied every minute, because a fork height is passed by the
// chain moving, not by a new manifest arriving.
const (
	advisoryEvery      = 24 * time.Hour
	advisoryRetry      = time.Hour
	advisoryFirstAfter = time.Minute
	advisoryApplyEvery = time.Minute
	// advisoryStaleAfter is when not having checked becomes worth saying.
	// Never worth stopping for: a manifest that cannot be had is not news
	// that anything is wrong.
	advisoryStaleAfter = 3 * 24 * time.Hour
)

// AdvisoryStatus is what the advisory says about this machine now.
type AdvisoryStatus struct {
	// Enabled is false in a build with no advisory key: nothing can be
	// verified, so nothing is fetched.
	Enabled bool
	Serial  int64
	// Checked is when a manifest last verified; zero if one never has.
	Checked  time.Time
	Stop     string
	Warnings []string
}

func advisoryStore() advisory.Store {
	return advisory.Store{Dir: filepath.Dir(autoinstall.BinDir())}
}

// applyAdvisoryNodes gives node selection the advisory's default nodes, from
// the last manifest that verified, before the first selection.
func applyAdvisoryNodes() {
	pk, ok := advisory.Key()
	if !ok {
		return
	}
	m, ok, err := advisoryStore().Load(pk)
	if err != nil || !ok || !time.Now().Before(m.Expires) {
		return
	}
	list := make([]nodes.Node, 0, len(m.Nodes))
	for _, n := range m.Nodes {
		list = append(list, nodes.Node{Host: n.Host, RPCPort: n.RPCPort, ZMQPort: n.ZMQPort, TorOnly: strings.HasSuffix(n.Host, ".onion")})
	}
	nodes.SetDefaults(list)
}

// setHold holds the miner for reason from source, or releases source's hold
// for "". The scheduler has one hold; the island check and the advisory
// each own a part of it, so neither releasing clears the other's.
func (s *Supervisor) setHold(source, reason string) {
	s.mu.Lock()
	if s.holds == nil {
		s.holds = map[string]string{}
	}
	if reason == "" {
		delete(s.holds, source)
	} else {
		s.holds[source] = reason
	}
	combined := combineHolds(s.holds)
	sched := s.sched
	s.mu.Unlock()
	if sched != nil {
		sched.SetHold(combined)
	}
}

// combineHolds is the one hold reason the scheduler shows: the advisory's
// first, since an update is the one thing the user can act on.
func combineHolds(holds map[string]string) string {
	var parts []string
	for _, src := range []string{"advisory", "island"} {
		if r := holds[src]; r != "" {
			parts = append(parts, r)
		}
	}
	return strings.Join(parts, "; ")
}

// Advisory reports the advisory's standing.
func (s *Supervisor) Advisory() AdvisoryStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.adv
	st.Warnings = append([]string(nil), s.adv.Warnings...)
	return st
}

// watchAdvisory keeps the advisory current until stop closes.
func (s *Supervisor) watchAdvisory(stop <-chan struct{}) {
	pk, ok := advisory.Key()
	if !ok {
		return // a build with no key: nothing to verify a manifest with
	}
	s.mu.Lock()
	s.adv.Enabled = true
	s.mu.Unlock()

	store := advisoryStore()
	var m advisory.Manifest
	var have bool
	if cached, ok, err := store.Load(pk); err != nil {
		log.Printf("advisory: the kept copy is not usable (%v); fetching afresh", err)
	} else if ok {
		m, have = cached, true
	}
	checked := func() time.Time {
		fi, err := os.Stat(filepath.Join(store.Dir, "advisory.json.minisig"))
		if err != nil {
			return time.Time{}
		}
		return fi.ModTime()
	}

	fetchAt := time.Now().Add(advisoryFirstAfter)
	t := time.NewTicker(advisoryApplyEvery)
	defer t.Stop()
	for {
		now := time.Now()
		if !now.Before(fetchAt) {
			if !nodes.TorAvailable() {
				fetchAt = now.Add(advisoryRetry) // never around Tor
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				var floor int64
				if have {
					floor = m.Serial
				}
				got, data, sig, err := advisory.Fetch(ctx, fmt.Sprintf("127.0.0.1:%d", torSOCKSPort), pk, floor)
				cancel()
				if err != nil {
					log.Printf("advisory: not fetched: %v", err)
					fetchAt = now.Add(advisoryRetry)
				} else {
					if err := store.Save(data, sig); err != nil {
						log.Printf("advisory: could not keep it: %v", err)
					}
					if !have || got.Serial != m.Serial {
						log.Printf("advisory: serial %d", got.Serial)
					}
					m, have = got, true
					fetchAt = now.Add(advisoryEvery)
				}
			}
		}
		s.applyAdvisory(m, have, checked(), now)
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// applyAdvisory evaluates the manifest against what is running and holds or
// releases the miner.
func (s *Supervisor) applyAdvisory(m advisory.Manifest, have bool, checked, now time.Time) {
	var v advisory.Verdict
	if have {
		var h advisory.Heights
		if s.p2pool != nil {
			if st, ok := s.p2pool.Stats(); ok {
				h = advisory.Heights{Monero: st.NetworkHeight, Sidechain: st.SidechainHeight}
			}
		}
		v = advisory.Evaluate(m, now, advisory.Running(s.bin.versions), h)
	}
	if checked.IsZero() || now.Sub(checked) > advisoryStaleAfter {
		v.Warnings = append(v.Warnings, "the advisory has not been checked for more than 3 days")
	}
	s.mu.Lock()
	changed := s.adv.Stop != v.Stop
	s.adv.Serial, s.adv.Checked, s.adv.Stop, s.adv.Warnings = 0, checked, v.Stop, v.Warnings
	if have {
		s.adv.Serial = m.Serial
	}
	s.mu.Unlock()
	if changed {
		if v.Stop != "" {
			log.Printf("warning: holding the miner: %s", v.Stop)
		} else {
			log.Printf("advisory: nothing holds the miner any more")
		}
	}
	s.setHold("advisory", v.Stop)
}
