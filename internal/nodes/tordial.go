package nodes

import (
	"context"
	"fmt"
	"net"

	"golang.org/x/net/proxy"
)

// torIsolationUser is the SOCKS username Kind Miner presents to Tor for
// traffic to host.
//
// Tor puts streams with different SOCKS credentials on different circuits
// (IsolateSOCKSAuth, on by default). Keying the username on the destination
// keeps each node on a circuit of its own, so one exit — or one guard
// watching timing — does not see Kind Miner talking to every node it knows,
// and a slow circuit to one node does not stall the others.
func torIsolationUser(host string) string {
	return "kind-miner/" + host
}

// torDialer returns a dialer that reaches host through Tor on its own circuit.
func torDialer(host string) (dialFunc, error) { return torDialerVia(torSOCKS5, host) }

func torDialerVia(socksAddr, host string) (dialFunc, error) {
	// Tor only reads the credentials as an isolation key; the password is never
	// checked, but SOCKS5 username/password auth needs one.
	auth := &proxy.Auth{User: torIsolationUser(host), Password: "kind-miner"}
	d, err := proxy.SOCKS5("tcp", socksAddr, auth, proxy.Direct)
	if err != nil {
		return nil, err
	}
	cd, ok := d.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("SOCKS5 dialer cannot take a deadline")
	}
	return cd.DialContext, nil
}

// directDial dials without a proxy.
func directDial(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// dialerFor picks how to reach a node: through Tor, isolated per node, for an
// onion; directly otherwise.
func dialerFor(n Node, hasTor bool) (dialFunc, error) {
	if !n.onion() {
		return directDial, nil
	}
	if !hasTor {
		return nil, fmt.Errorf(".onion node requires Tor at %s", torSOCKS5)
	}
	return torDialer(n.Host)
}
