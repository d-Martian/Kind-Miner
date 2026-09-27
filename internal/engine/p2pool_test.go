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
	p := NewP2Pool(P2PoolOptions{BinPath: bin, Wallet: "wallet", NodeHost: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083, Chain: "mini", StratumPort: 3333})
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
	p := NewP2Pool(P2PoolOptions{BinPath: bin, Wallet: "wallet", NodeHost: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083, Chain: "mini", StratumPort: 3333})
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
	p := NewP2Pool(P2PoolOptions{BinPath: bin, Wallet: "wallet", NodeHost: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083, Chain: "mini", StratumPort: 3333, WorkDir: workDir})
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

func TestP2PoolArgs(t *testing.T) {
	indexOf := func(args []string, s string) int {
		for i, a := range args {
			if a == s {
				return i
			}
		}
		return -1
	}

	t.Run("the Nodo profile", func(t *testing.T) {
		p := NewP2Pool(P2PoolOptions{Wallet: "w", NodeHost: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083,
			RPCLogin: "nodo:secret", NoRandomX: true, Chain: "nano", StratumPort: 3333})
		args := p.buildArgs()
		for _, flag := range []string{"--no-randomx", "--no-cache", "--no-log-file"} {
			if indexOf(args, flag) < 0 {
				t.Errorf("args lack %s: %v", flag, args)
			}
		}
		// p2pool attaches --rpc-login to the --host before it; ahead of any
		// --host it would create an empty host entry and the login would be lost.
		login, host := indexOf(args, "--rpc-login"), indexOf(args, "--host")
		if login < 0 || host < 0 || login < host || args[login+1] != "nodo:secret" {
			t.Errorf("--rpc-login must follow --host with the login: %v", args)
		}
	})
	t.Run("a desktop runs light and passes no login", func(t *testing.T) {
		p := NewP2Pool(P2PoolOptions{Wallet: "w", NodeHost: "node.example", RPCPort: 18089, ZMQPort: 18083, Chain: "mini", StratumPort: 3333, LightMode: true})
		args := p.buildArgs()
		if indexOf(args, "--light-mode") < 0 {
			t.Errorf("desktop args lack --light-mode, so p2pool takes a second 2 GB: %v", args)
		}
		for _, flag := range []string{"--no-randomx", "--no-cache", "--no-log-file", "--rpc-login"} {
			if indexOf(args, flag) >= 0 {
				t.Errorf("desktop args carry %s: %v", flag, args)
			}
		}
	})
	t.Run("the Nodo profile has no use for light mode", func(t *testing.T) {
		// --no-randomx allocates neither dataset nor cache; adding --light-mode
		// would say nothing and suggest p2pool still hashes locally.
		p := NewP2Pool(P2PoolOptions{Wallet: "w", NodeHost: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083, Chain: "nano", StratumPort: 3333, NoRandomX: true, LightMode: true})
		if args := p.buildArgs(); indexOf(args, "--light-mode") >= 0 {
			t.Errorf("Nodo args carry --light-mode: %v", args)
		}
	})
}

func TestP2PoolStartRefusedAfterClose(t *testing.T) {
	bin := fakeP2Pool(t, `echo "StratumServer event loop started"; sleep 60`)
	p := NewP2Pool(P2PoolOptions{BinPath: bin, Wallet: "wallet", NodeHost: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083, Chain: "mini", StratumPort: 3333})
	if err := p.Start(10 * time.Second); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.Close()
	// What a Nodo config change landing during shutdown would do.
	if err := p.Reconnect(18093, "", 10*time.Second); err == nil {
		p.Stop()
		t.Fatal("Reconnect started p2pool again after Close")
	}
}

func TestXMRigArgsNeverCarryTheWallet(t *testing.T) {
	// The wallet is p2pool's to hold. With pool mode gone xmrig only ever
	// talks to p2pool on loopback, and must not be handed the address to
	// forward anywhere.
	args := NewXMRig("", "127.0.0.1:3333", 4, 8080).buildArgs()
	for i, a := range args {
		if a == "--user" || a == "-u" {
			t.Errorf("xmrig args pass %s %q: %v", a, args[i+1], args)
		}
	}
}

func TestP2PoolRetargetDropsTorForALocalNode(t *testing.T) {
	bin := fakeP2Pool(t, `echo "StratumServer event loop started"; sleep 60`)
	p := NewP2Pool(P2PoolOptions{BinPath: bin, Wallet: "w", NodeHost: "127.0.0.2", RPCPort: 18089, ZMQPort: 18083,
		SOCKS5Proxy: "127.0.0.1:9050", Chain: "nano", StratumPort: 3333})
	defer p.Close()
	if err := p.Start(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := p.Retarget("127.0.0.1", 18081, 18083, "", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(p.buildArgs(), " ")
	if !strings.Contains(args, "--host 127.0.0.1 --rpc-port 18081 --zmq-port 18083") {
		t.Errorf("args after retarget: %s", args)
	}
	if strings.Contains(args, "--socks5") {
		t.Errorf("a local node is still reached through Tor: %s", args)
	}
	if !p.Ready() {
		t.Error("p2pool is not running after the retarget")
	}
}
