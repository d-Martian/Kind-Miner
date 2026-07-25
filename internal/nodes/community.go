// Package nodes selects a p2pool-compatible monerod node (one that has both
// RPC and ZMQ ports reachable) and handles Tor .onion nodes transparently.
package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// Node is a monerod endpoint suitable for p2pool.
type Node struct {
	Host    string
	RPCPort int
	ZMQPort int
	// TorOnly marks nodes reachable only via Tor SOCKS5 (.onion).
	TorOnly bool
}

// Addr returns "host:rpcport" for logging and p2pool --host / --rpc-port args.
func (n Node) Addr() string {
	return fmt.Sprintf("%s:%d", n.Host, n.RPCPort)
}

// communityNodes lists the p2pool-compatible monerod nodes used by kind-miner.
// The Nodo is the only entry: it is the primary backbone of this project and
// confirmed to expose ZMQ on port 18083 via its Tor hidden service.
// Most public monerod nodes do NOT expose ZMQ and are therefore unusable for
// p2pool — we don't list nodes we can't verify.
var communityNodes = []Node{
	{Host: "44yfclfdry66bbpyolux6xmfkcapage7khfk5sax7xmmylwwmjaqukad.onion", RPCPort: 18089, ZMQPort: 18083, TorOnly: true},
}

// torSOCKS5 is the standard local Tor SOCKS5 proxy address.
const torSOCKS5 = "127.0.0.1:9050"

// EnsureTor checks whether Tor is reachable at 127.0.0.1:9050. If not, it
// attempts to start the Tor system service (Linux/macOS only) and waits up
// to 10 seconds for the SOCKS5 port to open. Returns true if Tor is ready.
func EnsureTor() bool {
	if torReachable() {
		return true
	}
	if !tryStartTor() {
		return false
	}
	// Wait for SOCKS5 port to come up.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if torReachable() {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// TorAvailable returns true if a Tor SOCKS5 proxy is reachable at 127.0.0.1:9050.
func TorAvailable() bool {
	return torReachable()
}

func torReachable() bool {
	conn, err := net.DialTimeout("tcp", torSOCKS5, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// SelectBest probes candidate nodes and returns the fastest healthy one that
// has both RPC (synchronized) and ZMQ (port open) reachable.
//
// If customAddr is non-empty it is tried first; success returns it immediately.
// .onion nodes are skipped when Tor is not available.
func SelectBest(customAddr string) (Node, error) {
	hasTor := TorAvailable()

	if customAddr != "" {
		n, err := parseAddr(customAddr)
		if err != nil {
			return Node{}, fmt.Errorf("invalid remote_node %q: %w", customAddr, err)
		}
		if n.TorOnly && !hasTor {
			return Node{}, fmt.Errorf("node %s is a .onion address but Tor is not running on %s", customAddr, torSOCKS5)
		}
		if _, err := probeNode(n, hasTor); err != nil {
			return Node{}, fmt.Errorf("custom node %s: %w", customAddr, err)
		}
		return n, nil
	}

	type result struct {
		node    Node
		latency time.Duration
		err     error
	}

	ch := make(chan result, len(communityNodes))
	for _, n := range communityNodes {
		n := n
		if n.TorOnly && !hasTor {
			ch <- result{err: fmt.Errorf("skip: Tor not available")}
			continue
		}
		go func() {
			lat, err := probeNode(n, hasTor)
			ch <- result{n, lat, err}
		}()
	}

	var winners []result
	var failures []result
	for range communityNodes {
		r := <-ch
		if r.err == nil {
			winners = append(winners, r)
		} else if r.err.Error() != "skip: Tor not available" {
			failures = append(failures, r)
		}
	}

	if len(winners) == 0 {
		for _, f := range failures {
			log.Printf("node %s: %v", f.node.Addr(), f.err)
		}
		if !hasTor {
			return Node{}, fmt.Errorf(
				"the kind-miner node is only reachable via Tor, but Tor is not running.\n" +
					"\n" +
					"Install Tor and ensure it is running:\n" +
					"  Linux:   sudo apt install tor  (or dnf install tor)\n" +
					"  macOS:   brew install tor && brew services start tor\n" +
					"  Windows: https://www.torproject.org/download/\n" +
					"\n" +
					"Alternatively, set mode: pool in your config to use a traditional pool.")
		}
		return Node{}, fmt.Errorf(
			"the kind-miner node is unreachable — check the log above for details.\n" +
				"The node may be briefly offline; kind-miner will retry automatically.\n" +
				"If the problem persists, set mode: pool in your config to use a traditional pool.")
	}

	// Prefer the Nodo (TorOnly) when Tor is available; otherwise pick fastest.
	sort.Slice(winners, func(i, j int) bool {
		ni, nj := winners[i].node, winners[j].node
		if ni.TorOnly != nj.TorOnly {
			return ni.TorOnly // Tor/Nodo first
		}
		return winners[i].latency < winners[j].latency
	})
	return winners[0].node, nil
}

// probeNode checks that a node has synchronized RPC and an open ZMQ port.
func probeNode(n Node, hasTor bool) (time.Duration, error) {
	dialer, err := makeDialer(n, hasTor)
	if err != nil {
		return 0, err
	}

	start := time.Now()

	// 1. RPC check — must be synchronized.
	if err := checkRPC(n, dialer); err != nil {
		return 0, fmt.Errorf("RPC: %w", err)
	}

	// 2. ZMQ check — p2pool requires this port to be open.
	if err := checkZMQ(n, dialer); err != nil {
		return 0, fmt.Errorf("ZMQ port %d not reachable (node doesn't support p2pool): %w", n.ZMQPort, err)
	}

	return time.Since(start), nil
}

func makeDialer(n Node, hasTor bool) (proxy.Dialer, error) {
	if n.TorOnly || strings.HasSuffix(n.Host, ".onion") {
		if !hasTor {
			return nil, fmt.Errorf(".onion node requires Tor at %s", torSOCKS5)
		}
		return proxy.SOCKS5("tcp", torSOCKS5, nil, proxy.Direct)
	}
	return proxy.Direct, nil
}

func checkRPC(n Node, dialer proxy.Dialer) error {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}

	url := fmt.Sprintf("http://%s:%d/json_rpc", n.Host, n.RPCPort)
	body := strings.NewReader(`{"jsonrpc":"2.0","id":"0","method":"get_info"}`)
	resp, err := client.Post(url, "application/json", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			Status       string `json:"status"`
			Synchronized bool   `json:"synchronized"`
		} `json:"result"`
		Error *struct{ Message string `json:"message"` } `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Error != nil {
		return fmt.Errorf("rpc error: %s", result.Error.Message)
	}
	if result.Result.Status != "OK" {
		return fmt.Errorf("status: %s", result.Result.Status)
	}
	if !result.Result.Synchronized {
		return fmt.Errorf("not fully synchronized")
	}
	return nil
}

func checkZMQ(n Node, dialer proxy.Dialer) error {
	addr := net.JoinHostPort(n.Host, strconv.Itoa(n.ZMQPort))

	if !n.TorOnly && !strings.HasSuffix(n.Host, ".onion") {
		// Clearnet: use net.DialTimeout so firewalled ports fail fast.
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			return err
		}
		conn.Close()
		return nil
	}

	// Tor: SOCKS5 dialer doesn't support timeouts, so race against a timer.
	type result struct{ conn net.Conn; err error }
	ch := make(chan result, 1)
	go func() {
		conn, err := dialer.Dial("tcp", addr)
		ch <- result{conn, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		r.conn.Close()
		return nil
	case <-time.After(20 * time.Second):
		return fmt.Errorf("ZMQ timeout (Tor circuit took too long)")
	}
}

// parseAddr turns "host:port" into a Node, inferring the ZMQ port.
func parseAddr(addr string) (Node, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return Node{}, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Node{}, err
	}
	isTor := strings.HasSuffix(host, ".onion")
	// Infer ZMQ port: the conventional p2pool pairing is ZMQ 18083 for both
	// standard RPC ports (18081 unrestricted, 18089 restricted) — the same
	// pairing the local mode uses. Anything else falls back to RPC+3.
	zmq := port + 3
	if port == 18089 || port == 18081 {
		zmq = 18083
	}
	return Node{Host: host, RPCPort: port, ZMQPort: zmq, TorOnly: isTor}, nil
}
