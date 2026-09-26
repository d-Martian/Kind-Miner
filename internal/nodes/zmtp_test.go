package nodes

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestCheckGreeting(t *testing.T) {
	good := zmtpGreeting()
	mutate := func(f func(g []byte)) []byte {
		g := append([]byte(nil), good...)
		f(g)
		return g
	}
	cases := []struct {
		name    string
		in      []byte
		wantErr string
	}{
		{name: "a ZMTP 3 NULL peer is what p2pool needs", in: good},
		{name: "ZMTP 3.1 is fine too", in: mutate(func(g []byte) { g[11] = 1 })},
		{name: "an HTTP server on the ZMQ port is not ZMQ", in: []byte("HTTP/1.1 400 Bad Request\r\n\r\n" + strings.Repeat(" ", 40)), wantErr: "not a ZMQ endpoint"},
		{name: "ZMTP 2 is too old", in: mutate(func(g []byte) { g[10] = 2 }), wantErr: "too old"},
		{name: "a CURVE-secured publisher is refused by name", in: mutate(func(g []byte) { copy(g[12:32], "CURVE\x00\x00\x00") }), wantErr: `"CURVE"`},
		{name: "a truncated greeting says so", in: good[:11], wantErr: "short"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkGreeting(c.in)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %v, want one mentioning %q", err, c.wantErr)
			}
		})
	}
}

// serveOnce accepts connections on a loopback port and hands each to handle.
func serveOnce(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); handle(c) }()
		}
	}()
	return l.Addr().String()
}

func TestProbeZMTP(t *testing.T) {
	cases := []struct {
		name    string
		handle  func(net.Conn)
		wantErr bool
	}{
		{
			name: "a real ZMQ endpoint answers with its greeting",
			handle: func(c net.Conn) {
				buf := make([]byte, zmtpGreetingLen)
				if _, err := io.ReadFull(c, buf); err == nil && checkGreeting(buf) == nil {
					c.Write(zmtpGreeting())
				}
			},
		},
		{
			// The failure the old check missed: over Tor the SOCKS handshake
			// completes for any onion stream, so "the port opened" was all it
			// ever learned. A peer that accepts and never speaks ZMTP must fail.
			name:    "a port that accepts but never speaks ZMTP fails",
			handle:  func(c net.Conn) { io.Copy(io.Discard, c) },
			wantErr: true,
		},
		{
			name:    "a peer that hangs up at once fails",
			handle:  func(c net.Conn) {},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr := serveOnce(t, c.handle)
			start := time.Now()
			err := probeZMTP(context.Background(), directDial, addr, 500*time.Millisecond)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if time.Since(start) > 2*time.Second {
				t.Errorf("probe took %s; the timeout must bound it", time.Since(start))
			}
		})
	}
}
