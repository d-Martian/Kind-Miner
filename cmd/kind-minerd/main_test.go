package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/nodes"
)

const sampleAddress = "44AFFq5kSiGBoZ4NMDwYtN18obc8AemS33DBLWs3H7otXft3XjrpDtQGv7SqSsaBYBb98uNbr2VBBEt7f2wfn3RVGQBEP3A"

func TestDiagnoseNamesTheFirstProblem(t *testing.T) {
	mining := daemonStatus{State: "mining", Hashrate: 3420}
	cases := []struct {
		name    string
		f       facts
		want    string
		problem bool
	}{
		{"no config", facts{configMissing: true}, "kind-minerd init --address", true},
		{"a bad config", facts{configErr: errors.New("wallet address is required")}, "wallet address is required", true},
		{"not running", facts{}, "not running", true},
		{"stopped for a reason", facts{running: true, status: daemonStatus{State: "paused", Reason: "p2pool has no peers"}}, "Mining is stopped: p2pool has no peers.", true},
		{
			"an onion with no Tor and none managed",
			facts{running: true, status: mining, torNeeded: true},
			"no Tor is running", true,
		},
		{
			// kind-miner starts its own Tor, so a missing one is only a
			// problem when that has been switched off.
			"no Tor, but kind-miner manages one",
			facts{running: true, status: mining, torNeeded: true, manageTor: true},
			"Everything looks fine", false,
		},
		{"short huge pages", facts{running: true, status: mining, hugePagesShort: true, hugePagesWant: 1400}, "vm.nr_hugepages=1400", true},
		{
			"a local node that could be used",
			facts{running: true, status: mining, local: nodes.LocalNode{State: nodes.LocalNoZMQ, Detail: "the Monero node running here has no ZMQ"}},
			"The Monero node running here has no ZMQ.", true,
		},
		{"all well", facts{running: true, status: mining}, "Everything looks fine: mining at 3420 H/s.", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, problem := diagnose(c.f)
			if !strings.Contains(got, c.want) || problem != c.problem {
				t.Errorf("got %q (problem %v), want it to contain %q (problem %v)", got, problem, c.want, c.problem)
			}
		})
	}
}

func TestStatusFileRoundTripAndGoesStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	now := time.Now().Truncate(time.Second)
	r30 := 3100.0
	st := daemonStatus{Version: "v1", UpdatedAt: now, State: "mining", Hashrate: 3420, Hashrate30m: &r30, Threads: 8}
	if err := writeStatus(path, st); err != nil {
		t.Fatal(err)
	}
	got, running, err := readStatus(path, now.Add(5*time.Second))
	if err != nil || !running || got.Threads != 8 || *got.Hashrate30m != 3100 {
		t.Fatalf("read back %+v running=%v err=%v", got, running, err)
	}
	if _, running, _ := readStatus(path, now.Add(staleAfter+time.Second)); running {
		t.Error("a status file nobody refreshed still reads as running")
	}
	if _, running, err := readStatus(filepath.Join(t.TempDir(), "none.json"), now); running || err != nil {
		t.Errorf("no file: running=%v err=%v", running, err)
	}
	out := formatStatus(got)
	for _, want := range []string{"mining", "3420 H/s now", "3100 H/s 30 min", "— 24 h", "8 threads"} {
		if !strings.Contains(out, want) {
			t.Errorf("status summary lacks %q:\n%s", want, out)
		}
	}
}

func TestInit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	config.SetPath(path)
	t.Cleanup(func() { config.SetPath("") })

	if err := runInit("4notanaddress", false); err == nil {
		t.Error("init accepted a malformed address")
	}
	if err := runInit(sampleAddress, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Wallet != sampleAddress {
		t.Fatalf("config %+v err %v", cfg, err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("init wrote a config that does not validate: %v", err)
	}
	if err := runInit(sampleAddress, false); err == nil {
		t.Error("init replaced an existing config without --force")
	}
	if err := runInit(sampleAddress, true); err != nil {
		t.Errorf("--force: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestConfigFor(t *testing.T) {
	none := func(string) bool { return false }
	some := func(p string) bool { return p == serviceConfig }
	cases := []struct {
		name, flag, creds string
		uid               int
		exists            func(string) bool
		want              string
	}{
		{"--config wins", "/x.yaml", "/run/credentials/kind-minerd.service", 0, some, "/x.yaml"},
		{"the service reads its credential", "", "/run/credentials/kind-minerd.service", 61234, none,
			"/run/credentials/kind-minerd.service/config.yaml"},
		{"root sets up the service's config", "", "", 0, none, serviceConfig},
		{"a user asking about a configured service", "", "", 1000, some, serviceConfig},
		{"a user with no service keeps their own", "", "", 1000, none, ""},
	}
	for _, c := range cases {
		if got := configFor(c.flag, c.creds, c.uid, c.exists); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
