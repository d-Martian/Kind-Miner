package main

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/nodes"
)

const sampleAddress = "44AFFq5kSiGBoZ4NMDwYtN18obc8AemS33DBLWs3H7otXft3XjrpDtQGv7SqSsaBYBb98uNbr2VBBEt7f2wfn3RVGQBEP3A"

// thirdAddress is a third real address, for a change between two others.
const thirdAddress = "48n5Ygcu2EpGSKE4NhKcqpQftUYvcmCweWh2mAPngjPZR96UEE5mAwvCGv9QJLXPnFYjnnDhiZoqXUxmDd5CZrDP9t6onUg"

func TestDiagnoseNamesTheFirstProblem(t *testing.T) {
	mining := daemonStatus{State: "mining", Hashrate: 3420}
	cases := []struct {
		name    string
		f       facts
		want    string
		problem bool
	}{
		{"no config", facts{configMissing: true}, "kind-minerd init --address", true},
		{
			"waiting for the desktop",
			facts{configMissing: true, running: true, status: daemonStatus{State: stateAwaitingSetup}},
			"Settings → Connection → Find a hub", true,
		},
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
		{
			"engines rolled back",
			facts{running: true, status: daemonStatus{State: "mining", EnginesWarning: "xmrig 6.27.0 · p2pool 4.19 failed its health check (p2pool did not sync), so this machine went back to xmrig 6.26.0 · p2pool 4.18"}},
			"The engines were rolled back: xmrig 6.27.0 · p2pool 4.19 failed its health check", true,
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
	type outcome struct {
		err       string // a fragment of the error; "" for success
		wrote     bool
		hub       bool
		restarted bool
		paired    bool
		mode      config.Mode
	}
	cases := []struct {
		name      string
		forSvc    bool // the path is the service's config
		env       initEnv
		noHub     bool
		restartOK bool
		want      outcome
	}{
		{"a desktop or dev machine gets a user config, no hub",
			false, initEnv{}, false, true,
			outcome{wrote: true, mode: config.ModeP2PoolRemote}},
		{"without sudo where the service is installed, it refuses",
			false, initEnv{serviceInstalled: true}, false, true,
			outcome{err: "sudo kind-minerd init --address"}},
		{"--config is a deliberate choice, and is honoured",
			false, initEnv{serviceInstalled: true, flagConfig: true}, false, true,
			outcome{wrote: true, mode: config.ModeP2PoolRemote}},
		{"as root on a Nodo: hub on, restarted, paired — one command",
			true, initEnv{root: true, serviceInstalled: true, isNodo: true}, false, true,
			outcome{wrote: true, hub: true, restarted: true, paired: true, mode: config.ModeP2PoolLocal}},
		{"--no-hub mines on its own, and needs no pairing",
			true, initEnv{root: true, serviceInstalled: true}, true, true,
			outcome{wrote: true, restarted: true, mode: config.ModeP2PoolRemote}},
		{"a restart that fails says how to do it by hand",
			true, initEnv{root: true, serviceInstalled: true}, false, false,
			outcome{err: "sudo systemctl restart kind-minerd", wrote: true, hub: true, restarted: true}},
		{"root with no service installed writes the config, and says to run it",
			true, initEnv{root: true}, false, true,
			outcome{wrote: true, hub: true, mode: config.ModeP2PoolRemote}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			old := serviceConfig
			serviceConfig = filepath.Join(dir, "etc", "config.yaml")
			t.Cleanup(func() { serviceConfig = old })
			path := filepath.Join(dir, "home", "config.yaml")
			if c.forSvc {
				path = serviceConfig
			}
			config.SetPath(path)
			t.Cleanup(func() { config.SetPath("") })

			var got outcome
			env := c.env
			env.restart = func() error {
				got.restarted = true
				if !c.restartOK {
					return errors.New("exit status 1")
				}
				return nil
			}
			env.pairing = func(io.Writer) error { got.paired = true; return nil }
			var out strings.Builder
			err := runInit(&out, sampleAddress, false, c.noHub, env)

			switch {
			case c.want.err == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.want.err != "" && (err == nil || !strings.Contains(err.Error(), c.want.err)):
				t.Fatalf("err = %v, want it to mention %q", err, c.want.err)
			}
			if cfg, lerr := config.Load(); lerr == nil {
				got.wrote, got.hub, got.mode = true, cfg.Hub.Serve, cfg.Mode
				if verr := cfg.Validate(); verr != nil {
					t.Errorf("init wrote a config that does not validate: %v", verr)
				}
			}
			if c.want.err != "" {
				// The error was checked above; the mode is not compared
				// when init failed.
				got.err, got.mode = c.want.err, c.want.mode
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v\noutput:\n%s", got, c.want, out.String())
			}
		})
	}

	t.Run("an existing config is replaced only with --force", func(t *testing.T) {
		config.SetPath(filepath.Join(t.TempDir(), "config.yaml"))
		t.Cleanup(func() { config.SetPath("") })
		if err := runInit(io.Discard, "4notanaddress", false, false, initEnv{}); err == nil {
			t.Error("init accepted a malformed address")
		}
		if err := runInit(io.Discard, sampleAddress, false, false, initEnv{}); err != nil {
			t.Fatal(err)
		}
		if err := runInit(io.Discard, sampleAddress, false, false, initEnv{}); err == nil {
			t.Error("init replaced an existing config without --force")
		}
		if err := runInit(io.Discard, sampleAddress, true, false, initEnv{}); err != nil {
			t.Errorf("--force: %v", err)
		}
	})
}

func TestConfigFor(t *testing.T) {
	const creds, state = "/run/credentials/kind-minerd.service", "/var/lib/kind-miner"
	none := func(string) bool { return false }
	only := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, q := range paths {
				if p == q {
					return true
				}
			}
			return false
		}
	}
	credential := creds + "/" + credentialConfig
	cases := []struct {
		name, flag, creds, state string
		uid                      int
		exists                   func(string) bool
		want                     string
	}{
		{"--config wins", "/x.yaml", creds, state, 0, only(credential), "/x.yaml"},
		{"the service reads root's config when there is one", "", creds, state, 61234, only(credential), credential},
		{"the service set up from the desktop reads its own", "", creds, state, 61234, none, state + "/config.yaml"},
		{"root sets up the service's config", "", "", "", 0, none, serviceConfig},
		{"root checks a service set up over SSH", "", "", "", 0, only(serviceConfig, serviceStateConfig), serviceConfig},
		{"root checks a service set up from the desktop", "", "", "", 0, only(serviceStateConfig), serviceStateConfig},
		{"a user asking about a configured service", "", "", "", 1000, only(serviceConfig), serviceConfig},
		{"a user with no service keeps their own", "", "", "", 1000, none, ""},
	}
	for _, c := range cases {
		if got := configFor(c.flag, c.creds, c.state, c.uid, c.exists); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPushable(t *testing.T) {
	const creds = "/run/credentials/kind-minerd.service"
	cases := []struct {
		name, path, creds string
		want              bool
	}{
		{"root's config, handed over as a credential", creds + "/" + credentialConfig, creds, false},
		{"the service's own, set up from the desktop", "/var/lib/kind-miner/config.yaml", creds, true},
		{"a user's own daemon", "/home/a/.config/kind-miner/config.yaml", "", true},
	}
	for _, c := range cases {
		if got := pushable(c.path, c.creds); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
