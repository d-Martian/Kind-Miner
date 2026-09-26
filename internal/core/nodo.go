package core

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/nodes"
	"github.com/kind-miner/kind-miner/internal/nodo"
)

// detectNodo looks for a Nodo's configuration and records it, returning its
// settings and whether p2pool should run in the Nodo profile. Detection only
// reads: nothing under /home/nodo is ever written.
//
// A Nodo whose config.json exists but cannot be parsed is reported and then
// treated as an ordinary machine. Guessing its ports would connect p2pool to
// the wrong ZMQ publisher, and the failure would look like a sidechain that
// never syncs.
func (s *Supervisor) detectNodo() (n nodo.Node, profile bool) {
	n, ok, err := nodo.Detect()
	if err != nil {
		log.Printf("warning: this looks like a Nodo, but %s could not be read (%v); "+
			"using the standard p2pool setup", nodo.ConfigPath, err)
		return nodo.Node{}, false
	}
	if !ok {
		return nodo.Node{}, false
	}
	s.mu.Lock()
	s.nodo = &n
	s.mu.Unlock()
	profile = nodoProfileApplies(s.cfg.Mode, true)
	if profile {
		log.Printf("Nodo detected: following its own monerod (ZMQ %d%s) with p2pool's --no-randomx profile",
			n.ZMQPort, map[bool]string{true: ", RPC login", false: ""}[n.RPCLogin != ""])
	} else {
		log.Printf("Nodo detected, but mode is %s; set mode: %s to mine against this Nodo's own node",
			s.cfg.Mode, config.ModeP2PoolLocal)
	}
	return n, profile
}

// nodoProfileApplies reports whether p2pool should run in the Nodo profile.
// It follows the configured mode rather than overriding it: on a Nodo the
// profile is for following the Nodo's own monerod, and an owner who pointed
// p2pool at some other node did so on purpose.
func nodoProfileApplies(mode config.Mode, isNodo bool) bool {
	return isNodo && mode == config.ModeP2PoolLocal
}

// nodoNode is the monerod p2pool follows on a Nodo: loopback, the unrestricted
// RPC port, and the ZMQ port the owner configured.
func nodoNode(n nodo.Node) nodes.Node {
	return nodes.Node{Host: "127.0.0.1", RPCPort: nodo.RPCPort, ZMQPort: n.ZMQPort}
}

// statsDirFor picks where p2pool writes its JSON statistics. They are
// rewritten every few seconds and worthless after a restart, so on a Nodo they
// belong on tmpfs: systemd's RuntimeDirectory= when running as the service, the
// user's runtime directory otherwise, and the persistent work directory only
// when neither exists.
func statsDirFor(runtimeDir, xdgRuntimeDir, fallback string) string {
	switch {
	case runtimeDir != "":
		return filepath.Join(runtimeDir, "api")
	case xdgRuntimeDir != "":
		return filepath.Join(xdgRuntimeDir, "kind-miner", "api")
	}
	return fallback
}

func nodoStatsDir(fallback string) string {
	return statsDirFor(os.Getenv("RUNTIME_DIRECTORY"), os.Getenv("XDG_RUNTIME_DIR"), fallback)
}

// watchNodo restarts p2pool whenever the owner changes the node settings in one
// of Nodo's UIs. Nodo restarts monerod after such a change, and p2pool reads its
// ZMQ port and RPC login only at start-up, so without this a changed login
// leaves p2pool locked out until Kind Miner itself is restarted.
func (s *Supervisor) watchNodo(current nodo.Node) {
	stop := make(chan struct{})
	s.mu.Lock()
	s.nodoStop = stop
	s.mu.Unlock()
	go func() {
		err := nodo.Watch(stop, current, func(n nodo.Node) {
			log.Printf("Nodo settings changed; restarting p2pool against ZMQ %d", n.ZMQPort)
			if err := s.p2pool.Reconnect(n.ZMQPort, n.RPCLogin, 3*time.Minute); err != nil {
				log.Printf("p2pool did not come back after the Nodo change: %v", err)
			}
		})
		if err != nil {
			log.Printf("warning: not watching %s for changes (%v); restart Kind Miner after "+
				"changing the node's RPC login or ZMQ port", nodo.ConfigPath, err)
		}
	}()
}
