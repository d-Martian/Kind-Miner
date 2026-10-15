package nodes

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
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

// The relay needs more than the greeting. p2pool 4.18 gives its ZMQ
// connection one second to connect and one to finish the whole ZMTP
// handshake (connect_timeout and handshake_ivl, both 1000 ms, in
// zmq_reader.cpp). A node over Tor is a second and a half away, so a relay
// that only copied bytes made p2pool hang up exactly one second into every
// handshake, retry two seconds later, and never once hear the node: it learnt
// of new blocks only from its slower RPC polling, and mined on stale
// templates for a while after each one.
//
// So the relay speaks ZMTP 3.0 at both ends of its own connection: to p2pool
// it is a publisher, finishing the handshake at loopback speed; to the node it
// is a subscriber, taking as long as Tor takes. After the handshake both sides
// exchange the same framed messages, so the two streams are spliced as they
// are. Both handshakes are 3.0, not 3.1, so both ends send subscriptions the
// same way — as messages, not 3.1's SUBSCRIBE commands — and nothing in the
// stream needs translating.

// zmtpReady is the NULL mechanism's READY command for a socket of kind
// socketType ("PUB", "SUB").
func zmtpReady(socketType string) []byte {
	var body bytes.Buffer
	body.WriteByte(byte(len("READY")))
	body.WriteString("READY")
	body.WriteByte(byte(len("Socket-Type")))
	body.WriteString("Socket-Type")
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(socketType)))
	body.Write(n[:])
	body.WriteString(socketType)
	return append([]byte{0x04, byte(body.Len())}, body.Bytes()...) // 0x04: a short command frame
}

// maxCommand bounds a handshake command, so a peer cannot have the relay
// allocate whatever size it names. READY carries a few short properties.
const maxCommand = 64 << 10

// readReady reads the peer's handshake command and checks it is READY.
func readReady(r io.Reader) error {
	var flags [1]byte
	if _, err := io.ReadFull(r, flags[:]); err != nil {
		return fmt.Errorf("ZMTP READY: %w", err)
	}
	if flags[0]&0x04 == 0 {
		return errors.New("ZMTP READY: a message came before the handshake finished")
	}
	var size uint64
	if flags[0]&0x02 != 0 { // a long frame: an 8-byte size
		var n [8]byte
		if _, err := io.ReadFull(r, n[:]); err != nil {
			return fmt.Errorf("ZMTP READY: %w", err)
		}
		size = binary.BigEndian.Uint64(n[:])
	} else {
		var n [1]byte
		if _, err := io.ReadFull(r, n[:]); err != nil {
			return fmt.Errorf("ZMTP READY: %w", err)
		}
		size = uint64(n[0])
	}
	if size > maxCommand {
		return fmt.Errorf("ZMTP READY: a %d-byte command", size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return fmt.Errorf("ZMTP READY: %w", err)
	}
	if len(body) < 6 || body[0] != 5 || string(body[1:6]) != "READY" {
		name := body
		if len(body) > 0 && 1+int(body[0]) <= len(body) {
			name = body[1 : 1+int(body[0])]
		}
		return fmt.Errorf("ZMTP: expected READY, got the %q command", name)
	}
	return nil
}

// zmtpHandshake runs one side of a ZMTP 3.0 NULL handshake as a socket of
// kind socketType. As in probeZMTP, the greeting is written whole before the
// peer's is read.
func zmtpHandshake(rw io.ReadWriter, socketType string) error {
	if _, err := rw.Write(zmtpGreeting()); err != nil {
		return fmt.Errorf("sending ZMTP greeting: %w", err)
	}
	g := make([]byte, zmtpGreetingLen)
	if _, err := io.ReadFull(rw, g); err != nil {
		return fmt.Errorf("reading ZMTP greeting: %w", err)
	}
	if err := checkGreeting(g); err != nil {
		return err
	}
	if _, err := rw.Write(zmtpReady(socketType)); err != nil {
		return fmt.Errorf("sending ZMTP READY: %w", err)
	}
	return readReady(rw)
}
