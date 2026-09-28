package engine

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestTorManaged drives the real Tor subprocess: it launches Tor on a side port
// (so it won't clash with a system Tor on 9050), waits for bootstrap, checks the
// SOCKS port, then stops it. It needs a `tor` binary and network access, so it
// is gated behind KM_TOR_TEST and skipped in normal/CI runs.
func TestTorManaged(t *testing.T) {
	if os.Getenv("KM_TOR_TEST") == "" {
		t.Skip("set KM_TOR_TEST=1 to run the live Tor bootstrap test")
	}
	tor := NewTor("", t.TempDir(), 9555)
	if err := tor.Start(120 * time.Second); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tor.Stop()

	if !tor.Ready() {
		t.Fatal("Ready() = false after Start returned")
	}
	conn, err := net.DialTimeout("tcp", tor.SOCKSAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("SOCKS port %s not accepting: %v", tor.SOCKSAddr(), err)
	}
	conn.Close()
}

func TestTorrc(t *testing.T) {
	client := NewTor("", "/s/tor", 9050).torrc()
	if !strings.Contains(client, "SocksPort 127.0.0.1:9050\n") || strings.Contains(client, "HiddenService") {
		t.Errorf("client torrc:\n%s", client)
	}

	// The hub's own instance: serves the onion, offers no SOCKS port, so
	// nothing of the miner's ever goes out through it.
	hub := NewTor("", "/s/hub/tor", 0)
	hub.ServeOnion("/s/hub/onion", 18087, 18088)
	conf := hub.torrc()
	for _, want := range []string{
		"SocksPort 0\n",
		"DataDirectory /s/hub/tor\n",
		"HiddenServiceDir /s/hub/onion\n",
		"HiddenServicePort 18087 127.0.0.1:18087\n",
		"HiddenServicePort 18088 127.0.0.1:18088\n",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("hub torrc lacks %q:\n%s", want, conf)
		}
	}
}

func TestTorStartRefusedAfterClose(t *testing.T) {
	tor := NewTor("/nonexistent/tor", t.TempDir(), 0)
	tor.Close()
	if err := tor.Start(time.Second); err == nil || !strings.Contains(err.Error(), "shut down") {
		t.Errorf("Start after Close: %v", err)
	}
}
