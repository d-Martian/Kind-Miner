package core

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/nodes"
)

func TestWantsAutomaticNode(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
		nodo bool
		want bool
	}{
		{"remote mode with no node named", config.Config{Mode: config.ModeP2PoolRemote}, false, true},
		{"a remote_node the user named is kept", config.Config{Mode: config.ModeP2PoolRemote, RemoteNode: "node.example:18089"}, false, false},
		{"p2pool-local already uses this machine", config.Config{Mode: config.ModeP2PoolLocal}, false, false},
		{"the Nodo profile has its own node", config.Config{Mode: config.ModeP2PoolRemote}, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wantsAutomaticNode(&c.cfg, c.nodo); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// Only an onion needs Tor. Anything else goes direct: Tor would add latency to
// every block template and hide nothing an open address was not already showing.
func TestRoutesOverTor(t *testing.T) {
	cases := []struct {
		node nodes.Node
		want bool
	}{
		{nodes.Node{Host: "44yfclfdry66bbpyolux6xmfkcapage7khfk5sax7xmmylwwmjaqukad.onion", TorOnly: true}, true},
		{nodes.Node{Host: "abcdef.onion"}, true},
		{nodes.Node{Host: "127.0.0.1"}, false},
		{nodes.Node{Host: "192.168.8.192"}, false},
		{nodes.Node{Host: "node.example.org"}, false},
	}
	for _, c := range cases {
		if got := routesOverTor(c.node); got != c.want {
			t.Errorf("%s: routesOverTor = %v, want %v", c.node.Host, got, c.want)
		}
	}
}

func stubProbe(t *testing.T, result nodes.LocalNode) *int {
	t.Helper()
	calls := 0
	old := probeLocal
	probeLocal = func() nodes.LocalNode { calls++; return result }
	t.Cleanup(func() { probeLocal = old })
	return &calls
}

func TestAUsableLocalNodeReplacesTheOnionAndTor(t *testing.T) {
	stubProbe(t, nodes.LocalNode{State: nodes.LocalUsable,
		Node: nodes.Node{Host: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083}})
	s := New(&config.Config{Mode: config.ModeP2PoolRemote})
	if !s.torNeeded() {
		t.Fatal("precondition: the automatic onion node needs Tor")
	}
	s.findLocalNode(false)
	if s.localNode == nil || s.localNode.Host != "127.0.0.1" {
		t.Fatalf("local node = %+v, want this machine's", s.localNode)
	}
	if s.torNeeded() {
		t.Error("Tor is still started with a usable node on this machine")
	}
}

func TestAnUnusableLocalNodeFallsBackToTheRemote(t *testing.T) {
	for _, state := range []nodes.LocalState{nodes.LocalAbsent, nodes.LocalSyncing, nodes.LocalNoZMQ, nodes.LocalLocked, nodes.LocalBroken} {
		stubProbe(t, nodes.LocalNode{State: state, Detail: "reason"})
		s := New(&config.Config{Mode: config.ModeP2PoolRemote})
		s.findLocalNode(false)
		if s.localNode != nil || !s.torNeeded() {
			t.Errorf("state %v: local node %+v, torNeeded %v; want the remote over Tor", state, s.localNode, s.torNeeded())
		}
	}
}

func TestANamedRemoteNodeIsNotSecondGuessed(t *testing.T) {
	calls := stubProbe(t, nodes.LocalNode{State: nodes.LocalUsable, Node: nodes.Node{Host: "127.0.0.1"}})
	s := New(&config.Config{Mode: config.ModeP2PoolRemote, RemoteNode: "192.168.8.192:18089"})
	s.findLocalNode(false)
	if *calls != 0 || s.localNode != nil {
		t.Errorf("probed %d times and chose %+v over the node the user named", *calls, s.localNode)
	}
	if s.torNeeded() {
		t.Error("a clearnet remote_node started Tor")
	}
}

func TestWatchLocalSwitchesOnceWhenTheNodeIsReady(t *testing.T) {
	seq := []nodes.LocalState{nodes.LocalSyncing, nodes.LocalSyncing, nodes.LocalNoZMQ, nodes.LocalUsable, nodes.LocalUsable}
	probes := 0
	probe := func() nodes.LocalNode {
		st := seq[probes]
		probes++
		return nodes.LocalNode{State: st, Node: nodes.Node{Host: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083}}
	}
	var switched []nodes.Node
	done := make(chan struct{})
	go func() {
		watchLocal(make(chan struct{}), time.Millisecond, probe, func(n nodes.Node) { switched = append(switched, n) })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watch kept going after the node became usable")
	}
	if len(switched) != 1 || switched[0].RPCPort != 18081 {
		t.Errorf("switched %v, want once to the local node", switched)
	}
	if probes != 4 {
		t.Errorf("probed %d times, want to stop at the first usable answer (4)", probes)
	}
}

func TestWatchLocalStopsWithTheSupervisor(t *testing.T) {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		watchLocal(stop, time.Millisecond, func() nodes.LocalNode { return nodes.LocalNode{State: nodes.LocalAbsent} },
			func(nodes.Node) { t.Error("switched to a node that never appeared") })
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the watch outlived Shutdown")
	}
}

// fakeP2PoolBin stands in for p2pool: it reports ready and waits.
func fakeP2PoolBin(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake binary is a shell script")
	}
	path := filepath.Join(t.TempDir(), "p2pool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'StratumServer event loop started'\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSwitchToLocalLeavesTorBehind(t *testing.T) {
	onion := nodes.Node{Host: "44yfclfdry66bbpyolux6xmfkcapage7khfk5sax7xmmylwwmjaqukad.onion", RPCPort: 28089, ZMQPort: 28083, TorOnly: true}
	relay, err := nodes.StartRelay(onion, 7)
	if err != nil {
		t.Skipf("cannot start a relay here: %v", err)
	}
	s := New(&config.Config{Mode: config.ModeP2PoolRemote})
	s.relay = relay
	s.nodeAddr, s.viaTor = onion.Addr(), true
	l := relay.Local()
	s.p2pool = engine.NewP2Pool(engine.P2PoolOptions{BinPath: fakeP2PoolBin(t), Wallet: "w",
		NodeHost: l.Host, RPCPort: l.RPCPort, ZMQPort: l.ZMQPort, SOCKS5Proxy: "127.0.0.1:9050",
		Chain: "nano", StratumPort: 3333})
	defer s.p2pool.Close()
	if err := s.p2pool.Start(10 * time.Second); err != nil {
		t.Fatal(err)
	}

	s.switchToLocal(nodes.Node{Host: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083})

	if addr, viaTor := s.Node(); addr != "127.0.0.1:18081" || viaTor {
		t.Errorf("Node() = %s, viaTor %v; want the local node, direct", addr, viaTor)
	}
	if s.relay != nil {
		t.Error("the Tor relay is still held after switching to the local node")
	}
	if _, err := net.DialTimeout("tcp", net.JoinHostPort(l.Host, strconv.Itoa(l.RPCPort)), 200*time.Millisecond); err == nil {
		t.Error("the relay still accepts connections after the switch")
	}
	if !s.p2pool.Ready() {
		t.Error("p2pool is not running on the local node")
	}
}
