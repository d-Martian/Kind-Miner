package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/rollout"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// The .deb lays its engines out beside the daemon: /opt/kind-miner/engines
// is the pair this release shipped, and /opt/kind-miner/engines.previous the
// last different pair, kept by the package's maintainer scripts on upgrade
// (packaging/deb/preinst and postinst).
const (
	enginesDir         = "engines"
	previousEnginesDir = "engines.previous"
)

// engineSetup is what the daemon decided about engines for one run.
type engineSetup struct {
	choice    rollout.Choice
	current   *rollout.Engines
	state     rollout.State
	statePath string
	// canRollBack: a failure of current has somewhere to go.
	canRollBack bool
	// warn is the amber status line: the choice's, or a failure in this
	// run that had nowhere to go back to. Read by the status writer.
	warn atomic.Pointer[string]
}

// warning is what status shows about the engines, if anything.
func (e *engineSetup) warning() string {
	if w := e.warn.Load(); w != nil {
		return *w
	}
	return e.choice.RolledBack
}

// chooseEngines reads the packaged pairs beside the executable at root and
// what earlier runs learned about them. A broken layout is logged and
// treated as no packaged engines, so the daemon falls back to downloading
// rather than refusing to start.
func chooseEngines(root, stateDir string) *engineSetup {
	current, err := rollout.Load(filepath.Join(root, enginesDir))
	if err != nil {
		log.Printf("warning: the packaged engines cannot be used (%v); downloading the pinned ones instead", err)
		return &engineSetup{}
	}
	if current == nil {
		return &engineSetup{} // not a packaged install
	}
	previous, err := rollout.Load(filepath.Join(root, previousEnginesDir))
	if err != nil {
		log.Printf("warning: the previous engines cannot be used (%v)", err)
		previous = nil
	}
	path := filepath.Join(stateDir, "engines.json")
	st, err := rollout.ReadState(path)
	if err != nil {
		log.Printf("warning: %s is unreadable (%v); judging the engines afresh", path, err)
		st = rollout.State{}
	}
	return &engineSetup{
		choice:      rollout.Choose(current, previous, st),
		current:     current,
		state:       st,
		statePath:   path,
		canRollBack: rollout.CanRollBackTo(current, previous, st),
	}
}

// packagedRoot is where a packaged install keeps its engines: beside the
// executable.
func packagedRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// engineStateDir is where what the daemon learned about engines is kept: the
// state directory, beside the downloaded binaries.
func engineStateDir() string { return filepath.Dir(autoinstall.BinDir()) }

// reject records that the current pair failed, and whether there is a pair
// to go back to. Recorded before anything else, so a daemon killed mid-way
// still does not run the failed pair on its next start.
func (e *engineSetup) reject(reason string) (rollBack bool) {
	e.state.Rejected, e.state.Reason, e.state.At = e.current.ID, reason, time.Now().UTC()
	if err := rollout.WriteState(e.statePath, e.state); err != nil {
		log.Printf("warning: could not record the failed engines: %v", err)
	}
	if e.canRollBack {
		log.Printf("warning: %s failed its health check: %s. Going back to the previous engines.", e.current.Version, reason)
	} else {
		w := fmt.Sprintf("%s failed its health check (%s), and there is no earlier pair known to work to go back to", e.current.Version, reason)
		e.warn.Store(&w)
		log.Printf("warning: %s", w)
	}
	return e.canRollBack
}

// pass records that the current pair passed.
func (e *engineSetup) pass() {
	e.state.Healthy, e.state.At = e.current.ID, time.Now().UTC()
	if e.state.Rejected == e.current.ID {
		e.state.Rejected, e.state.Reason = "", ""
	}
	if err := rollout.WriteState(e.statePath, e.state); err != nil {
		log.Printf("warning: could not record the healthy engines: %v", err)
	}
	log.Printf("%s passed its health check", e.current.Version)
}

// observe is one look at a running supervisor, for the health check.
func observe(sup *core.Supervisor, now time.Time) rollout.Observation {
	var o rollout.Observation
	if p := sup.P2Pool(); p != nil {
		o.P2PoolExpected = true
		st, ok := p.Stats()
		o.P2PoolSynced = ok && now.Sub(st.UpdatedAt) < 2*time.Minute
	}
	if s := sup.Scheduler(); s != nil {
		o.Island, _ = s.Held()
		_, duty := s.Allowance()
		o.MinerAllowed = duty > 0
		rate, _ := stats.Mean(s.History(), time.Minute, stats.Hashrate)
		o.Hashing = rate > 0
	}
	return o
}

// probationInterval is how often a pair on probation is looked at.
const probationInterval = 30 * time.Second

// watchProbation looks at sup until the current pair's probation ends,
// recording the verdict. It closes failed if the pair failed and there is a
// pair to go back to, and returns early when stop closes.
func (e *engineSetup) watchProbation(sup *core.Supervisor, failed chan<- struct{}, stop <-chan struct{}) {
	check := rollout.NewCheck(time.Now())
	t := time.NewTicker(probationInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			switch v, reason := check.Observe(now, observe(sup, now)); v {
			case rollout.Healthy:
				e.pass()
				return
			case rollout.Failed:
				if e.reject(reason) {
					close(failed)
				}
				return
			}
		}
	}
}
