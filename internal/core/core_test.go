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

func TestRandomXModeFor(t *testing.T) {
	const gib = int64(1) << 30
	cases := []struct {
		name      string
		available int64
		known     bool
		want      string
	}{
		// A 16 GB Nodo with the node running has about 13 GiB available.
		{"plenty left for the page cache takes fast mode", 13 * gib, true, "fast"},
		// An 8 GB board would give up most of its LMDB cache to the dataset.
		{"a tight board keeps its page cache and mines light", 5 * gib, true, "light"},
		{"unknown memory takes the kind answer", 0, false, "light"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := randomXModeFor(c.available, c.known); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
