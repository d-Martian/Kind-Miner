package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/nodes"
	"github.com/kind-miner/kind-miner/internal/nodo"
	"github.com/kind-miner/kind-miner/internal/stats"
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

func TestNodoProfileApplies(t *testing.T) {
	cases := []struct {
		name   string
		mode   config.Mode
		isNodo bool
		want   bool
	}{
		{"a Nodo following its own node", config.ModeP2PoolLocal, true, true},
		{"a Nodo pointed at another node on purpose", config.ModeP2PoolRemote, true, false},
		{"a desktop running its own monerod", config.ModeP2PoolLocal, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nodoProfileApplies(c.mode, c.isNodo); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestNodoNodeUsesTheUnrestrictedRPC(t *testing.T) {
	// Not monero_rpc_port (18089): that is the restricted listener, which
	// refuses the calc_pow calls --no-randomx depends on.
	n := nodoNode(nodo.Node{ZMQPort: 18093})
	if n.Host != "127.0.0.1" || n.RPCPort != 18081 || n.ZMQPort != 18093 {
		t.Errorf("got %+v", n)
	}
}

func TestStatsDirFor(t *testing.T) {
	cases := []struct {
		name                 string
		runtimeDir, xdg, fbk string
		want                 string
	}{
		{"systemd's RuntimeDirectory wins", "/run/kind-miner", "/run/user/1000", "/var/lib/x/api", "/run/kind-miner/api"},
		{"the user's runtime dir otherwise", "", "/run/user/1000", "/var/lib/x/api", "/run/user/1000/kind-miner/api"},
		{"the persistent dir only as a last resort", "", "", "/var/lib/x/api", "/var/lib/x/api"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := statsDirFor(c.runtimeDir, c.xdg, c.fbk); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestLedgerSavesOnATimerAndStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	l := stats.NewLedger()
	l.Add(stats.Sample{At: time.Now(), Hashrate: 1000})
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { saveLedgerEvery(l, path, 10*time.Millisecond, stop); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the ledger was never saved")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the saver outlived Shutdown")
	}

	restored := stats.NewLedger()
	restoreLedger(restored, path)
	if _, ok := restored.Rate(time.Minute, time.Now()); !ok {
		t.Error("the saved minute did not come back on restore")
	}
}
