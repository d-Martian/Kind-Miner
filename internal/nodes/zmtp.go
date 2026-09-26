package nodes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"time"
)

// A ZMQ port that accepts a TCP connection proves almost nothing. Behind Tor
// it proves nothing at all: the SOCKS handshake completes as soon as the onion
// service accepts the stream, so a dial "succeeds" for a node whose ZMQ p2pool
// can never use — libzmq has no SOCKS support, so p2pool reaches the node's
// RPC through Tor and then sits without block notifications. The old check
// passed in exactly that state. The probe below speaks the first step of the
// protocol instead: send a ZMTP 3 greeting and require one back.

// zmtpGreetingLen is the fixed size of a ZMTP 3.x greeting.
const zmtpGreetingLen = 64

// zmtpGreeting is a ZMTP 3.0 greeting for the NULL security mechanism, which is
// what monerod's ZMQ publisher and p2pool's subscriber both use (RFC 23):
// signature (0xFF, 8 bytes of padding, 0x7F), version 3.0, mechanism "NULL"
// padded to 20 bytes, as-server 0, and 31 bytes of filler.
func zmtpGreeting() []byte {
	g := make([]byte, zmtpGreetingLen)
	g[0], g[9] = 0xFF, 0x7F
	g[10], g[11] = 3, 0
	copy(g[12:32], "NULL")
	return g
}

// checkGreeting validates a peer's greeting. It checks what p2pool depends on
// — a ZMTP 3 peer offering the NULL mechanism — and says which part was wrong,
// because "an HTTP server on the ZMQ port" and "ZMQ with CURVE" are different
// problems for whoever runs the node.
func checkGreeting(g []byte) error {
	if len(g) < zmtpGreetingLen {
		return fmt.Errorf("short ZMTP greeting (%d of %d bytes)", len(g), zmtpGreetingLen)
	}
	if g[0] != 0xFF || g[9] != 0x7F {
		return fmt.Errorf("not a ZMQ endpoint (no ZMTP signature)")
	}
	if g[10] < 3 {
		return fmt.Errorf("ZMTP %d.%d is too old; p2pool needs ZMTP 3", g[10], g[11])
	}
	if mech := string(bytes.TrimRight(g[12:32], "\x00")); mech != "NULL" {
		return fmt.Errorf("ZMQ offers the %q mechanism; p2pool needs NULL", mech)
	}
	return nil
}

// probeZMTP opens addr through dial and completes the greeting exchange within
// timeout. It hangs up straight after: the greeting is enough to prove a ZMQ
// endpoint is really there, and subscribing would pull block data for nothing.
func probeZMTP(ctx context.Context, dial dialFunc, addr string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := dial(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	// Send the whole greeting at once. libzmq sends its signature, waits to see
	// ours, then sends the rest; a peer that is not ZMQ never answers at all,
	// which the deadline turns into an error instead of a hang.
	if _, err := conn.Write(zmtpGreeting()); err != nil {
		return fmt.Errorf("sending ZMTP greeting: %w", err)
	}
	reply := make([]byte, zmtpGreetingLen)
	if n, err := io.ReadFull(conn, reply); err != nil {
		if n == 0 {
			return fmt.Errorf("no ZMTP greeting came back: %w", err)
		}
		return checkGreeting(reply[:n])
	}
	return checkGreeting(reply)
}

// dialFunc is the dialling seam: direct for clearnet nodes, Tor for onions,
// and a fake in tests.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)
