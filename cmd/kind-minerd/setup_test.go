package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/hub"
)

const wallet = "4AdUndXHHZ6cfufTMvppY6JwXNouMBzSkbLYfpAV5Usx3skxNgYeYTRj5UzqtReoS44qo9mtmXCqY45DJ852K5Jv2684Rge"

// freePort finds a port free for both TCP (the API) and UDP (the beacon).
func freePort(t *testing.T) int {
	t.Helper()
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		if pc, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", port)); err == nil {
			pc.Close()
			return port
		}
	}
	t.Fatal("no free port")
	return 0
}

func TestSetUpFromTheDesktop(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATE_DIRECTORY", dir)
	t.Setenv("RUNTIME_DIRECTORY", dir)
	config.SetPath(filepath.Join(dir, "config.yaml"))
	t.Cleanup(func() { config.SetPath("") })
	port := freePort(t)
	setupPort = port
	t.Cleanup(func() { setupPort = hub.DefaultAPIPort })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		cfg *config.Config
		err error
	}
	done := make(chan result, 1)
	go func() {
		cfg, err := awaitSetup(ctx)
		done <- result{cfg, err}
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	var hello hub.Hello
	var fp [32]byte
	var err error
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if hello, fp, err = hub.Probe(ctx, addr, [32]byte{}); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("the waiting daemon never answered: %v", err)
	}
	if hello.SetUp {
		t.Error("a daemon with no config says it is set up")
	}
	st, running, err := readStatus(statusPath(), time.Now())
	if err != nil || !running || st.State != stateAwaitingSetup {
		t.Errorf("status = %+v, running %v, %v; want it waiting to be set up", st, running, err)
	}
	if !strings.Contains(formatStatus(st), "Find a hub") {
		t.Errorf("status does not say how to set it up:\n%s", formatStatus(st))
	}

	if _, _, err := hub.SetUp(ctx, addr, fp, "8notanaddress"); err == nil {
		t.Error("a bad address was accepted")
	}
	p, owner, err := hub.SetUp(ctx, addr, fp, wallet)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "127.0.0.1" || owner == "" {
		t.Errorf("pairing %+v, owner %q", p, owner)
	}

	var r result
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("awaitSetup did not return once set up")
	}
	if r.err != nil {
		t.Fatal(r.err)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Wallet != wallet || !saved.Hub.Serve || r.cfg.Wallet != wallet {
		t.Errorf("saved config: wallet %q, hub.serve %v", saved.Wallet, saved.Hub.Serve)
	}
	if err := saved.Validate(); err != nil {
		t.Errorf("the config setup wrote does not validate: %v", err)
	}
	id, err := hub.Load(filepath.Join(dir, "hub"))
	if err != nil || id.Owner != owner || id.Fingerprint != fp {
		t.Errorf("hub identity after setup: %v, owner kept %v", err, id.Owner == owner)
	}
}

func TestAwaitSetupStopsOnSignal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATE_DIRECTORY", dir)
	t.Setenv("RUNTIME_DIRECTORY", dir)
	config.SetPath(filepath.Join(dir, "config.yaml"))
	t.Cleanup(func() { config.SetPath("") })
	setupPort = freePort(t)
	t.Cleanup(func() { setupPort = hub.DefaultAPIPort })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	cfg, err := awaitSetup(ctx)
	if cfg != nil || err != nil {
		t.Errorf("got %v, %v; want nothing, quietly", cfg, err)
	}
	if _, err := os.Stat(statusPath()); !errors.Is(err, os.ErrNotExist) {
		t.Error("a stopped daemon left its status behind")
	}
}

func TestPushWallet(t *testing.T) {
	dir := t.TempDir()
	config.SetPath(filepath.Join(dir, "config.yaml"))
	t.Cleanup(func() { config.SetPath("") })
	cfg := newConfig(wallet, false)
	cfg.Hub.Serve = true
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	next := sampleAddress
	reload := make(chan struct{}, 1)

	if err := pushWallet(cfg, "not an address", reload); err == nil {
		t.Error("a bad address was accepted")
	}
	// The shape of an address, with a checksum that fails: p2pool aborts on
	// one, so the hub must refuse it rather than restart onto it.
	if err := pushWallet(cfg, "4"+strings.Repeat("B", 94), reload); err == nil || len(reload) != 0 {
		t.Errorf("an address with a bad checksum: err %v, restart asked %v", err, len(reload) != 0)
	}
	if err := pushWallet(cfg, wallet, reload); err != nil || len(reload) != 0 {
		t.Errorf("the same wallet: err %v, restart asked %v", err, len(reload) != 0)
	}
	if err := pushWallet(cfg, next, reload); err != nil {
		t.Fatal(err)
	}
	if len(reload) != 1 {
		t.Error("a new wallet did not ask for a restart")
	}
	// A second change before the restart does not block the API.
	if err := pushWallet(cfg, thirdAddress, reload); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load()
	if err != nil || saved.Wallet != thirdAddress || !saved.Hub.Serve {
		t.Errorf("saved: %v, %+v", err, saved)
	}
}
