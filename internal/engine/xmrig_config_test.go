package engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readConfig(t *testing.T, path string) xmrigConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c xmrigConfig
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestXMRigConfig(t *testing.T) {
	x := NewXMRig("", "127.0.0.1:3333", 12, 18080)
	x.SetLayout([]int{12, 13, 14, 15})
	data, err := x.configJSON()
	if err != nil {
		t.Fatal(err)
	}
	var c xmrigConfig
	json.Unmarshal(data, &c)
	if !reflect.DeepEqual(c.CPU.RX, []int{12, 13, 14, 15}) {
		t.Errorf("rx = %v, want the layout", c.CPU.RX)
	}
	// The watch is what makes a layout change live; MSR writes are
	// system-wide and need root, which kind-miner never has or wants.
	if !c.Watch || c.RandomX.WrMSR || c.RandomX.RdMSR || c.RandomX.OneGBPages {
		t.Errorf("watch %v, msr %v/%v, 1gb %v", c.Watch, c.RandomX.RdMSR, c.RandomX.WrMSR, c.RandomX.OneGBPages)
	}
	if len(c.Pools) != 1 || c.Pools[0].URL != "127.0.0.1:3333" || c.HTTP.Port != 18080 || c.HTTP.Host != "127.0.0.1" {
		t.Errorf("pools %+v http %+v", c.Pools, c.HTTP)
	}

	// No layout: the thread count, each without affinity.
	y := NewXMRig("", "127.0.0.1:3333", 3, 18080)
	data, _ = y.configJSON()
	json.Unmarshal(data, &c)
	if !reflect.DeepEqual(c.CPU.RX, []int{-1, -1, -1}) {
		t.Errorf("rx without a layout = %v, want three unpinned threads", c.CPU.RX)
	}
}

func noScopes(t *testing.T) {
	old := useScopes
	useScopes = false
	t.Cleanup(func() { useScopes = old })
}

func TestXMRigMinesToAHubOverPinnedTLS(t *testing.T) {
	x := NewXMRig("", "192.168.8.192:18087", 2, 18080)
	x.UseHub("laptop", "ab12")
	data, err := x.configJSON()
	if err != nil {
		t.Fatal(err)
	}
	var c xmrigConfig
	json.Unmarshal(data, &c)
	want := xmrigPool{URL: "192.168.8.192:18087", User: "laptop", TLS: true, TLSFingerprint: "ab12", Keepalive: true}
	if len(c.Pools) != 1 || c.Pools[0] != want {
		t.Errorf("pools = %+v, want %+v", c.Pools, want)
	}
	args := strings.Join(x.buildArgs(), " ")
	if !strings.Contains(args, "--user laptop --tls --tls-fingerprint ab12") {
		t.Errorf("flag form lacks the hub login and pin: %s", args)
	}

	// The loopback miner sends no user, no TLS: p2pool is on the same machine.
	data, _ = NewXMRig("", "127.0.0.1:3333", 2, 18080).configJSON()
	if strings.Contains(string(data), "tls") || strings.Contains(string(data), "user") {
		t.Errorf("loopback config carries hub fields: %s", data)
	}
}

func TestXMRigFallsBackToTheHubsOnion(t *testing.T) {
	x := NewXMRig("", "192.168.8.192:18087", 2, 18080)
	x.UseHub("laptop", "ab12")
	x.UseHubFallback("abc.onion:18087", "127.0.0.1:9050")
	data, _ := x.configJSON()
	var c xmrigConfig
	json.Unmarshal(data, &c)
	want := []xmrigPool{
		// The LAN first: xmrig keeps retrying pool 0 and returns to it.
		{URL: "192.168.8.192:18087", User: "laptop", TLS: true, TLSFingerprint: "ab12", Keepalive: true},
		{URL: "abc.onion:18087", User: "laptop", TLS: true, TLSFingerprint: "ab12", SOCKS5: "127.0.0.1:9050", Keepalive: true},
	}
	if !reflect.DeepEqual(c.Pools, want) {
		t.Errorf("pools = %+v\nwant  %+v", c.Pools, want)
	}
}

func TestSetLayoutRewritesTheWatchedConfig(t *testing.T) {
	noScopes(t)
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep binary")
	}
	path := filepath.Join(t.TempDir(), "xmrig.json")
	// A stand-in miner: it ignores its arguments and waits, as the real one
	// would while it hashes.
	bin := filepath.Join(t.TempDir(), "xmrig")
	os.WriteFile(bin, []byte("#!/bin/sh\nsleep 60\n"), 0o755)
	x := NewXMRig(bin, "127.0.0.1:3333", 0, 18080)
	x.UseConfigFile(path)
	x.SetLayout([]int{12, 13})
	if err := x.Start(); err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	if got := readConfig(t, path).CPU.RX; !reflect.DeepEqual(got, []int{12, 13}) {
		t.Fatalf("started with rx %v", got)
	}
	if err := x.SetLayout([]int{1, 3, 0, 12, 13}); err != nil {
		t.Fatal(err)
	}
	if got := readConfig(t, path).CPU.RX; !reflect.DeepEqual(got, []int{1, 3, 0, 12, 13}) {
		t.Errorf("after SetLayout rx = %v", got)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("the temporary file was left behind")
	}
	if x.PID() == 0 {
		t.Error("changing the layout stopped the miner")
	}
}

func TestSetLayoutNeedsAConfigFile(t *testing.T) {
	noScopes(t)
	bin := filepath.Join(t.TempDir(), "xmrig")
	os.WriteFile(bin, []byte("#!/bin/sh\nsleep 60\n"), 0o755)
	x := NewXMRig(bin, "127.0.0.1:3333", 2, 18080)
	if err := x.Start(); err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	if err := x.SetLayout([]int{0, 1}); err == nil {
		t.Error("a miner running from flags claimed to change its layout")
	}
}
