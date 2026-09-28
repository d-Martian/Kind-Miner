package nodes

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestParseAddr(t *testing.T) {
	tests := []struct {
		input       string
		wantHost    string
		wantRPC     int
		wantZMQ     int
		wantTorOnly bool
		wantErr     bool
	}{
		{
			input:    "node.example.com:18089",
			wantHost: "node.example.com",
			wantRPC:  18089,
			wantZMQ:  18083,
		},
		{
			input:    "node.example.com:18081",
			wantHost: "node.example.com",
			wantRPC:  18081,
			wantZMQ:  18083, // conventional p2pool pairing, same as local mode
		},
		{
			input:    "node.example.com:18085",
			wantHost: "node.example.com",
			wantRPC:  18085,
			wantZMQ:  18088, // port+3 fallback
		},
		{
			input:       "abc123.onion:18089",
			wantHost:    "abc123.onion",
			wantRPC:     18089,
			wantZMQ:     18083,
			wantTorOnly: true,
		},
		{
			input:   "notahost",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			n, err := parseAddr(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseAddr(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAddr(%q) unexpected error: %v", tt.input, err)
			}
			if n.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", n.Host, tt.wantHost)
			}
			if n.RPCPort != tt.wantRPC {
				t.Errorf("RPCPort = %d, want %d", n.RPCPort, tt.wantRPC)
			}
			if n.ZMQPort != tt.wantZMQ {
				t.Errorf("ZMQPort = %d, want %d", n.ZMQPort, tt.wantZMQ)
			}
			if n.TorOnly != tt.wantTorOnly {
				t.Errorf("TorOnly = %v, want %v", n.TorOnly, tt.wantTorOnly)
			}
		})
	}
}

func TestNodeAddr(t *testing.T) {
	n := Node{Host: "example.com", RPCPort: 18089, ZMQPort: 18083}
	got := n.Addr()
	want := "example.com:18089"
	if got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
}

func TestCommunityNodesHaveZMQ(t *testing.T) {
	for _, n := range communityNodes {
		if n.ZMQPort == 0 {
			t.Errorf("community node %s has ZMQPort=0", n.Host)
		}
		if n.RPCPort == 0 {
			t.Errorf("community node %s has RPCPort=0", n.Host)
		}
	}
}

// TestParseAddrErrIsBadAddr pins the sentinel that tells a typo apart from an
// unreachable node: resolveNode retries the second and must not retry the
// first.
func TestParseAddrErrIsBadAddr(t *testing.T) {
	for _, addr := range []string{"notahost", "192.168.8.192", "192.168.8.192:rpc"} {
		t.Run(addr, func(t *testing.T) {
			_, err := parseAddr(addr)
			if !errors.Is(err, ErrBadAddr) {
				t.Errorf("parseAddr(%q) error = %v, want it to wrap ErrBadAddr", addr, err)
			}
		})
	}
}

func TestCheckRPCWaitsAsLongAsItIsTold(t *testing.T) {
	// A node that answers, slowly — what an onion looks like over a Tor
	// that has only just bootstrapped.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte(`{"result":{"status":"OK","synchronized":true}}`))
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	rpcPort, _ := strconv.Atoi(port)
	n := Node{Host: host, RPCPort: rpcPort}

	if err := checkRPC(n, directDial, 5*time.Second); err != nil {
		t.Errorf("a slow node that answers inside the timeout failed: %v", err)
	}
	if err := checkRPC(n, directDial, 50*time.Millisecond); err == nil {
		t.Error("a node slower than the timeout passed")
	}
	// The onion allowance must cover a first circuit on a fresh Tor, which
	// took longer than the 15 s the RPC check used to have.
	if torProbeTimeout <= 15*time.Second {
		t.Errorf("torProbeTimeout = %s", torProbeTimeout)
	}
}
