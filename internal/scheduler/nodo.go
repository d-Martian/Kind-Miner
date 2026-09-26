package scheduler

import (
	"log"
	"runtime"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/monitor"
)

// Mining on a Nodo is mining as a guest on a machine with a job. The node —
// monerod and, where installed, the light-wallet server — is why the box
// exists, so the rules are stricter than on a desktop and they protect the
// services rather than a user at a keyboard:
//
//   - start on the low-power cores, which the node barely uses;
//   - take the big cores only while the node's services show no sign of
//     waiting for CPU, and hand them back the moment they do;
//   - stop outright while the node is syncing;
//   - hold the SoC to a lower temperature than a desktop limit, because a fanless
//     board that runs hot all day throttles the node too.
//
// The miner is never restarted to do any of this. It runs one thread per core
// from launch and is confined to a cluster by CPU affinity, so moving between
// clusters is a syscall rather than a RandomX re-initialisation.

// nodoTempLimit is the ceiling on a Nodo. The RK3588 starts throttling itself
// at 85 °C, and passive cases sit in the 50s under the node alone; 70 leaves
// the node's own bursts room without ever meeting the SoC's throttle.
const nodoTempLimit = 70

// widenAfterTicks is how long the node's services must stay unpressured before
// the miner may take the big cores — 30 seconds at tickInterval. It is the
// cluster-level version of Fall ≫ Rise: narrowing happens on the first
// pressured tick, widening only after sustained calm, so a service that is busy
// every few seconds keeps the big cores rather than fighting for them.
const widenAfterTicks = 15

// nodoServices are the units whose CPU pressure decides whether the miner may
// use the big cores. monero-lws is optional on a Nodo; an absent unit counts
// as calm.
var nodoServices = []string{"monerod.service", "monero-lws.service"}

// nodoRPC is monerod's default unrestricted listener, which Nodo's start
// script leaves in place beside its own restricted one.
const nodoRPC = "127.0.0.1:18081"

type pressureSource interface {
	Pressured() (pressured, known bool)
}

type syncSource interface {
	Synced() (synced, known bool)
}

// nodoControl holds the Nodo-specific samplers and the cluster state.
type nodoControl struct {
	pressure []pressureSource
	sync     syncSource
	// little and all are the low-power cluster and every core. When they are
	// the same length there is nothing to widen into.
	little, all []int
	wide        bool
	calm        int
}

func newNodoControl() *nodoControl {
	n := &nodoControl{sync: monitor.NewNodeSync(nodoRPC)}
	for _, unit := range nodoServices {
		n.pressure = append(n.pressure, monitor.NewServicePressure(unit))
	}
	if little, all, ok := monitor.CoreClusters(); ok {
		n.little, n.all = little, all
	} else {
		log.Printf("warning: cannot tell this machine's core clusters apart; mining on every core, still throttled by the duty cycle")
	}
	return n
}

// Threads is the thread count the miner launches with. It must be the same in
// the supervisor and the scheduler, or the two disagree about the miner's
// full-tilt share.
//
// On a Nodo it is one thread per core, not the cache cap. Affinity decides
// which of those cores the miner actually runs on, and it can only move onto a
// core it has a thread for. The cache cap is also the wrong model there: it
// assumes each 2 MiB scratchpad wants its own slice of a big shared L3, where
// the RK3588 has 3 MiB of L3 for eight cores and would be capped to one.
func Threads(cfg *config.Config) int {
	if cfg.MineOnNodo && cfg.MaxThreads <= 0 {
		return runtime.NumCPU()
	}
	return ThreadCap(cfg.MaxThreads)
}

// tempLimitFor applies the Nodo ceiling on top of the configured limit. A
// configured 0 ("no limit") does not lift it: the ceiling is part of what the
// owner opted into, not a preference layered on top.
func tempLimitFor(cfg *config.Config) float64 {
	limit := cfg.TempLimitCelsius
	if cfg.MineOnNodo && (limit <= 0 || limit > nodoTempLimit) {
		return nodoTempLimit
	}
	return limit
}

// nextWide advances the cluster decision by one tick. Pressure — or not being
// able to tell — narrows at once and resets the calm count; calm widens only
// once it has lasted widenAfterTicks.
func nextWide(wide bool, calm int, pressured bool) (bool, int) {
	if pressured {
		return false, 0
	}
	calm++
	if calm >= widenAfterTicks {
		return true, calm
	}
	return wide, calm
}

// anyPressured reports whether any service is short of CPU. A service that
// cannot say counts as pressured: the safe reading of "unknown" on a node is
// "do not take more".
func anyPressured(sources []pressureSource) bool {
	for _, p := range sources {
		pressured, known := p.Pressured()
		if pressured || !known {
			return true
		}
	}
	return false
}

// placeOnCores decides which cluster the miner runs on this tick and applies
// it. Outside Nodo mode, or on a machine with a single kind of core, it does
// nothing.
func (s *Scheduler) placeOnCores() {
	n := s.nodo
	if n == nil || len(n.little) == 0 || len(n.little) >= len(n.all) {
		return
	}
	wasWide := n.wide
	n.wide, n.calm = nextWide(n.wide, n.calm, anyPressured(n.pressure))
	cores := n.little
	if n.wide {
		cores = n.all
	}
	if n.wide != wasWide {
		if n.wide {
			log.Printf("node services are calm; mining on all %d cores", len(n.all))
		} else {
			log.Printf("node services want the CPU; mining on the %d low-power cores only", len(n.little))
		}
	}

	s.mu.Lock()
	s.activeCores = len(cores)
	s.mu.Unlock()

	// Applied every tick, not only on a change: xmrig creates its hashing
	// threads after the dataset is ready, and those must be confined too.
	if a, ok := s.xmrig.(affinitySetter); ok {
		if err := a.SetAffinity(cores); err != nil {
			log.Printf("could not set miner affinity: %v", err)
		}
	}
}
