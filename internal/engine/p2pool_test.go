package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeP2Pool writes an executable shell script standing in for the p2pool
// binary, so Start's readiness/exit handling can be tested without the real
// thing (which needs a synced monerod).
func fakeP2Pool(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-binary tests use a shell script")
	}
	path := filepath.Join(t.TempDir(), "p2pool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestP2PoolStartReady(t *testing.T) {
	bin := fakeP2Pool(t, `echo "StratumServer event loop started"; sleep 60`)
	p := NewP2Pool(bin, "wallet", "127.0.0.1", 18081, 18083, "mini", 3333, "", "")
	defer p.Stop()

	start := time.Now()
	if err := p.Start(10 * time.Second); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Start took %s; expected to return as soon as the ready line appeared", elapsed)
	}
	if !p.Ready() {
		t.Error("Ready() = false after Start returned")
	}
}

func TestP2PoolStartFailsFastOnExit(t *testing.T) {
	bin := fakeP2Pool(t, `echo "ZMQReader failed to connect"; exit 1`)
	p := NewP2Pool(bin, "wallet", "127.0.0.1", 18081, 18083, "mini", 3333, "", "")
	defer p.Stop()

	start := time.Now()
	err := p.Start(30 * time.Second)
	if err == nil {
		t.Fatal("Start succeeded; want early-exit error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Start took %s; expected fail-fast, not the full timeout", elapsed)
	}
	if !strings.Contains(err.Error(), "exited before becoming ready") {
		t.Errorf("error = %q; want early-exit message", err)
	}
	if !strings.Contains(err.Error(), "ZMQReader failed to connect") {
		t.Errorf("error = %q; want p2pool's last output line included", err)
	}
}

func TestP2PoolWorkDir(t *testing.T) {
	// p2pool writes p2pool.cache/log to its cwd; verify Start runs it in the
	// configured work dir, creating the dir if needed.
	bin := fakeP2Pool(t, `pwd > owd.txt; echo "StratumServer event loop started"; sleep 60`)
	workDir := filepath.Join(t.TempDir(), "p2pool-data")
	p := NewP2Pool(bin, "wallet", "127.0.0.1", 18081, 18083, "mini", 3333, "", workDir)
	defer p.Stop()

	if err := p.Start(10 * time.Second); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(workDir, "owd.txt"))
	if err != nil {
		t.Fatalf("marker file not written in work dir: %v", err)
	}
	if cwd := strings.TrimSpace(string(got)); cwd != workDir {
		// Symlinked temp dirs (macOS) make an exact match too strict; the
		// marker landing in workDir already proves the cwd. Just log it.
		t.Logf("p2pool cwd = %q (work dir %q)", cwd, workDir)
	}
}
