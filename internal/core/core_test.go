package core

import (
	"errors"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/nodes"
)

func TestTorNeeded(t *testing.T) {
	cases := []struct {
		name   string
		mode   config.Mode
		remote string
		want   bool
	}{
		{"remote default Nodo is .onion", config.ModeP2PoolRemote, "", true},
		{"remote custom .onion node", config.ModeP2PoolRemote, "abcdef.onion:18089", true},
		{"remote custom clearnet node", config.ModeP2PoolRemote, "192.168.8.192:18089", false},
		{"local monerod", config.ModeP2PoolLocal, "", false},
		{"traditional pool", config.ModePool, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(&config.Config{Mode: tc.mode, RemoteNode: tc.remote})
			if got := s.torNeeded(); got != tc.want {
				t.Errorf("torNeeded() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveNodeDoesNotRetryBadAddress guards the difference between a slow
// failure and a fast one. Node selection retries 3x with 30s gaps for a node
// that is merely unreachable; a malformed address parses the same way every
// time, so retrying it only delays the error by a minute.
func TestResolveNodeDoesNotRetryBadAddress(t *testing.T) {
	cfg := &config.Config{Mode: config.ModeP2PoolRemote, RemoteNode: "192.168.8.192"}

	start := time.Now()
	_, err := resolveNode(cfg)
	elapsed := time.Since(start)

	if !errors.Is(err, nodes.ErrBadAddr) {
		t.Fatalf("resolveNode() error = %v, want it to wrap nodes.ErrBadAddr", err)
	}
	// A single retry would put this well past 30s.
	if elapsed > 5*time.Second {
		t.Errorf("resolveNode() took %s; it retried a permanent failure", elapsed)
	}
}
