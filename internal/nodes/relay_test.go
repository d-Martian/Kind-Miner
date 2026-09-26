package nodes

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestRelayHost(t *testing.T) {
	if got := RelayHost(0); got != "127.0.0.2" {
		t.Errorf("RelayHost(0) = %s; 127.0.0.1 must stay free for everything else", got)
	}
	if got := RelayHost(1); got != "127.0.0.3" {
		t.Errorf("RelayHost(1) = %s", got)
	}
}

func TestTorIsolationUserDiffersPerNode(t *testing.T) {
	if torIsolationUser("a.onion") == torIsolationUser("b.onion") {
		t.Error("two nodes share a SOCKS username, so Tor would share their circuit")
	}
}

// fakeSOCKS5 is a minimal SOCKS5 server that records the username and target
// of each CONNECT, then splices the stream to upstream.
func fakeSOCKS5(t *testing.T, upstream string) (addr string, seen chan [2]string) {
	t.Helper()
	seen = make(chan [2]string, 4)
	addr = serveOnce(t, func(c net.Conn) {
		r := bufio.NewReader(c)
		// Greeting: offer must include username/password (0x02).
		hdr := make([]byte, 2)
		io.ReadFull(r, hdr)
		methods := make([]byte, hdr[1])
		io.ReadFull(r, methods)
		c.Write([]byte{5, 2})
		// RFC 1929 auth.
		io.ReadFull(r, hdr[:2])
		user := make([]byte, hdr[1])
		io.ReadFull(r, user)
		plen, _ := r.ReadByte()
		io.ReadFull(r, make([]byte, plen))
		c.Write([]byte{1, 0})
		// CONNECT request.
		req := make([]byte, 4)
		io.ReadFull(r, req)
		var host string
		switch req[3] {
		case 3:
			n, _ := r.ReadByte()
			b := make([]byte, n)
			io.ReadFull(r, b)
			host = string(b)
		case 1:
			b := make([]byte, 4)
			io.ReadFull(r, b)
			host = net.IP(b).String()
		}
		pb := make([]byte, 2)
		io.ReadFull(r, pb)
		seen <- [2]string{string(user), net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb))))}
		up, err := net.Dial("tcp", upstream)
		if err != nil {
			c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		defer up.Close()
		c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		go io.Copy(up, r)
		io.Copy(c, up)
	})
	return addr, seen
}

func TestTorDialerIsolatesAndKeepsOnionNamesForTor(t *testing.T) {
	echo := serveOnce(t, func(c net.Conn) { io.Copy(c, c) })
	socks, seen := fakeSOCKS5(t, echo)

	const onion = "44yfclfdry66bbpyolux6xmfkcapage7khfk5sax7xmmylwwmjaqukad.onion"
	dial, err := torDialerVia(socks, onion)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := dial(ctx, "tcp", net.JoinHostPort(onion, "18083"))
	if err != nil {
		t.Fatal(err)
	}
	c.Close()

	got := <-seen
	if got[0] != torIsolationUser(onion) {
		t.Errorf("SOCKS username = %q, want the per-node isolation user", got[0])
	}
	// An onion name resolved locally would leak it to DNS and fail anyway;
	// it must reach Tor as a name.
	if got[1] != onion+":18083" {
		t.Errorf("CONNECT target = %q, want the onion name itself", got[1])
	}
}

// startTestRelay relays to a local upstream with a direct dialer standing in
// for Tor, on the address the first relay would really use.
func startTestRelay(t *testing.T, upstream string, dial dialFunc) *Relay {
	t.Helper()
	host, port, _ := net.SplitHostPort(upstream)
	p, _ := strconv.Atoi(port)
	r, err := startRelay(Node{Host: host, RPCPort: p, ZMQPort: p}, RelayHost(0), dial)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func roundTrip(t *testing.T, addr string) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("got %q, %v through the relay", buf, err)
	}
}

func TestRelayCarriesRPCAndZMQ(t *testing.T) {
	echo := serveOnce(t, func(c net.Conn) { io.Copy(c, c) })
	r := startTestRelay(t, echo, directDial)
	l := r.Local()
	roundTrip(t, net.JoinHostPort(l.Host, strconv.Itoa(l.RPCPort)))
	roundTrip(t, net.JoinHostPort(l.Host, strconv.Itoa(l.ZMQPort)))
}

func TestRelayFallsBackWhenItsAddressCannotBeBound(t *testing.T) {
	echo := serveOnce(t, func(c net.Conn) { io.Copy(c, c) })
	// TEST-NET-1 is never configured locally, like 127.0.0.2 on macOS.
	r, err := startRelay(Node{Host: "x.onion", RPCPort: 18089, ZMQPort: 18083}, "192.0.2.1",
		func(ctx context.Context, network, _ string) (net.Conn, error) { return directDial(ctx, network, echo) })
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	l := r.Local()
	if l.Host != "127.0.0.1" || l.RPCPort == l.ZMQPort {
		t.Fatalf("fallback = %+v, want 127.0.0.1 on two distinct free ports", l)
	}
	roundTrip(t, net.JoinHostPort(l.Host, strconv.Itoa(l.ZMQPort)))
}

func TestRelayDropsTheLocalSideWhenTorCannotReachTheNode(t *testing.T) {
	r := startTestRelay(t, "127.0.0.1:1", func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("tor: host unreachable")
	})
	l := r.Local()
	c, err := net.DialTimeout("tcp", net.JoinHostPort(l.Host, strconv.Itoa(l.RPCPort)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	// p2pool must see a closed connection it will retry, not a hang.
	if _, err := c.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("read = %v, want EOF", err)
	}
}

func TestRelayCloseEndsLiveStreams(t *testing.T) {
	// A ZMQ subscription: the upstream holds the stream open and says nothing.
	quiet := serveOnce(t, func(c net.Conn) { io.Copy(io.Discard, c) })
	r := startTestRelay(t, quiet, directDial)
	l := r.Local()
	c, err := net.DialTimeout("tcp", net.JoinHostPort(l.Host, strconv.Itoa(l.ZMQPort)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	time.Sleep(50 * time.Millisecond) // let the relay dial upstream

	done := make(chan struct{})
	go func() { r.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close hung on an open stream; Shutdown would too")
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := c.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("read after Close = %v, want EOF", err)
	}
}
