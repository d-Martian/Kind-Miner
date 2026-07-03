package engine_test

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/engine"
)

// TestTorDownloadAndBootstrap exercises the full zero-install path: download and
// SHA256-verify the Tor Expert Bundle, then bootstrap that tor on a side port.
// It pulls ~30 MB and needs network, so it is gated behind KM_TOR_DOWNLOAD_TEST.
func TestTorDownloadAndBootstrap(t *testing.T) {
	if os.Getenv("KM_TOR_DOWNLOAD_TEST") == "" {
		t.Skip("set KM_TOR_DOWNLOAD_TEST=1 to download and bootstrap the Tor Expert Bundle")
	}
	dir := t.TempDir()
	bin, err := autoinstall.EnsureTor(dir)
	if err != nil {
		t.Fatalf("EnsureTor: %v", err)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("tor binary missing after EnsureTor: %v", err)
	}

	tor := engine.NewTor(bin, dir+"/tor-data", 9556)
	if err := tor.Start(150 * time.Second); err != nil {
		t.Fatalf("Start (downloaded tor): %v", err)
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
