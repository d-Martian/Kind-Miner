package core

import (
	"log"
	"strings"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/nodes"
)

// probeLocal is the local-node probe; a variable so tests can stand in for a
// monerod.
var probeLocal = nodes.ProbeLocal

// wantsAutomaticNode reports whether the user left the node for kind-miner to
// choose. Only then does a node on this machine take precedence: a remote_node
// the user named, or their own node in p2pool-local mode, is their choice to
// keep — and the Nodo profile has its own node already.
func wantsAutomaticNode(cfg *config.Config, nodoProfile bool) bool {
	return !nodoProfile && cfg.Mode == config.ModeP2PoolRemote && cfg.RemoteNode == ""
}

// findLocalNode looks for a usable monerod on this machine and records it. A
// node that is running but cannot be used is named in the log with the one
// change that would make it usable, and selection falls through to the remote
// node rather than waiting on it.
func (s *Supervisor) findLocalNode(nodoProfile bool) {
	if !wantsAutomaticNode(s.cfg, nodoProfile) {
		return
	}
	local := probeLocal()
	switch local.State {
	case nodes.LocalUsable:
		n := local.Node
		s.localNode = &n
		log.Printf("Using the Monero node running on this machine (%s); no Tor needed", n.Addr())
	case nodes.LocalAbsent:
		// The common case; nothing to say.
	default:
		log.Printf("Not using the local Monero node: %s. Using the remote node for now.", local.Detail)
	}
}

// routesOverTor reports whether reaching node needs Tor: only an onion does.
// Everything else — this machine, a LAN node, a clearnet remote_node — is
// dialled directly, because Tor adds seconds of latency to every template and
// hides nothing an address on the open internet was not already showing.
func routesOverTor(node nodes.Node) bool {
	return node.TorOnly || strings.HasSuffix(node.Host, ".onion")
}

// localWatchInterval is how often, while mining on the remote node, kind-miner
// looks again for a usable node on this machine. Gupax-style: a monerod that
// was still syncing at startup — a day or two, for a new node — or started
// later, is picked up when it is ready, without a restart.
const localWatchInterval = time.Minute

// watchLocal probes every interval until the local node is usable, hands it to
// onUsable once, and stops. The probe and the action are parameters so the
// loop can be tested without a monerod or a clock worth waiting on.
func watchLocal(stop <-chan struct{}, interval time.Duration, probe func() nodes.LocalNode, onUsable func(nodes.Node)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	last := nodes.LocalAbsent
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		local := probe()
		if local.State == nodes.LocalUsable {
			onUsable(local.Node)
			return
		}
		// Say what changed, not the same thing every minute: a node that
		// appears but is syncing, or loses ZMQ, is worth one line.
		if local.State != last && local.State != nodes.LocalAbsent {
			log.Printf("Local Monero node not usable yet: %s", local.Detail)
		}
		last = local.State
	}
}

// startLocalWatch begins watching for a local node while mining on the remote
// one. It applies only where automatic selection would have chosen a local
// node, and only when one was not already chosen.
func (s *Supervisor) startLocalWatch(nodoProfile bool) {
	if !wantsAutomaticNode(s.cfg, nodoProfile) || s.localNode != nil {
		return
	}
	stop := make(chan struct{})
	s.mu.Lock()
	s.localWatchStop = stop
	s.mu.Unlock()
	go watchLocal(stop, localWatchInterval, probeLocal, s.switchToLocal)
}

// switchToLocal moves p2pool from the remote node to the one on this machine.
// p2pool goes first, so it is never pointed at a relay that is already gone;
// then the relay and, if kind-miner started it, Tor itself, since nothing
// needs them any more. A system Tor is left alone — it is not ours.
func (s *Supervisor) switchToLocal(n nodes.Node) {
	log.Printf("The Monero node on this machine is ready; switching p2pool to %s, off Tor", n.Addr())
	if err := s.p2pool.Retarget(n.Host, n.RPCPort, n.ZMQPort, "", 3*time.Minute); err != nil {
		log.Printf("p2pool did not come back on the local node: %v", err)
		return
	}
	s.mu.Lock()
	s.localNode = &n
	s.nodeAddr, s.viaTor = n.Addr(), false
	relay, tor := s.relay, s.tor
	s.relay, s.tor = nil, nil
	s.mu.Unlock()
	if relay != nil {
		relay.Close()
	}
	if tor != nil {
		tor.Stop()
	}
}
