package engine

import (
	"net"
	"os"
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
