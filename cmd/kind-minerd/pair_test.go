package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/hub"
)

func TestPair(t *testing.T) {
	state := t.TempDir()
	t.Setenv("STATE_DIRECTORY", state) // where core.HubDir looks, as under systemd
	config.SetPath(filepath.Join(t.TempDir(), "config.yaml"))
	t.Cleanup(func() { config.SetPath("") })

	cfg := config.Defaults()
	cfg.Wallet = sampleAddress
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runPair(&out, "", "10.0.0.2"); err == nil || !strings.Contains(err.Error(), "serve: true") {
		t.Errorf("pair with the hub off: err = %v, want the setting to turn on", err)
	}

	cfg.Hub.Serve = true
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	// pair only reads the identity the daemon makes, so a root shell never
	// leaves a key the service's own user cannot read.
	if err := runPair(&out, "", "10.0.0.2"); err == nil || !strings.Contains(err.Error(), "restart") {
		t.Errorf("pair before the hub started: err = %v", err)
	}

	id, err := hub.Ensure(core.HubDir())
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runPair(&out, "garage server", "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	var code string
	for _, f := range strings.Fields(out.String()) {
		if strings.HasPrefix(f, "km1-") {
			code = f
		}
	}
	p, err := hub.ParseCode(code)
	if err != nil {
		t.Fatalf("pair printed no usable code (%v):\n%s", err, out.String())
	}
	if p.Host != "10.0.0.2" || p.StratumPort != hub.DefaultStratumPort || p.Fingerprint != id.Fingerprint || p.Token != id.Token {
		t.Errorf("code carries %+v", p)
	}
	for _, want := range []string{`"user": "garage-server"`, `"tls-fingerprint": "` + id.FingerprintHex()} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %s:\n%s", want, out.String())
		}
	}
}

func TestPairWithAnOnion(t *testing.T) {
	state := t.TempDir()
	t.Setenv("STATE_DIRECTORY", state)
	config.SetPath(filepath.Join(t.TempDir(), "config.yaml"))
	t.Cleanup(func() { config.SetPath("") })
	cfg := config.Defaults()
	cfg.Wallet = sampleAddress
	cfg.Hub.Serve, cfg.Hub.Onion = true, true
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Ensure(core.HubDir()); err != nil {
		t.Fatal(err)
	}
	code := func(out string) hub.Pairing {
		t.Helper()
		for _, f := range strings.Fields(out) {
			if strings.HasPrefix(f, "km1-") || strings.HasPrefix(f, "km2-") {
				p, err := hub.ParseCode(f)
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
		}
		t.Fatalf("no code in:\n%s", out)
		return hub.Pairing{}
	}

	// Before Tor has made the service: a LAN code, and the output says so.
	var out bytes.Buffer
	if err := runPair(&out, "", "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	if p := code(out.String()); p.Onion != "" || !strings.Contains(out.String(), "LAN only") {
		t.Errorf("before the onion exists:\n%s", out.String())
	}

	onion := "duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion"
	if err := os.MkdirAll(hub.OnionDir(core.HubDir()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hub.OnionDir(core.HubDir()), "hostname"), []byte(onion+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runPair(&out, "garage", "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	if p := code(out.String()); p.Onion != onion {
		t.Errorf("code carries onion %q", p.Onion)
	}
	if !strings.Contains(out.String(), `"socks5": "127.0.0.1:9050"`) {
		t.Errorf("the bare-xmrig config lacks the Tor fallback:\n%s", out.String())
	}
}

func TestStatusListsTheHubsDevices(t *testing.T) {
	st := daemonStatus{Version: "v1", State: "mining", HubPort: 18087, Workers: []hub.Worker{
		{Name: "garage-server", Addr: "192.168.8.20:51234", Connected: 3 * time.Hour, Hashrate: 4000},
		{Addr: "192.168.8.21:40000", Connected: 5 * time.Second},
	}}
	out := formatStatus(st)
	for _, want := range []string{"port 18087 (TLS) · 2 connected", "garage-server", "4000 H/s", "(logging in)", "—", "up 5s", "up 3h0m0s"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(formatStatus(daemonStatus{State: "mining"}), "hub") {
		t.Error("a machine that is not a hub mentions one")
	}
	paired := formatStatus(daemonStatus{State: "mining", MinesTo: "moneronodo.lan:18087", HubError: "connection refused"})
	for _, want := range []string{"mining to the hub at moneronodo.lan:18087", "connection refused"} {
		if !strings.Contains(paired, want) {
			t.Errorf("paired status lacks %q:\n%s", want, paired)
		}
	}
}
