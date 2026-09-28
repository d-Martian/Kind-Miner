package rollout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 3, 30, 0, 0, time.UTC)
	good := Observation{P2PoolExpected: true, P2PoolSynced: true, MinerAllowed: true, Hashing: true}
	cases := []struct {
		name   string
		obs    []Observation // one a minute from t0
		want   Verdict
		reason string
	}{
		{"a working pair passes once probation is over", repeat(good, 16), Healthy, ""},
		{"nothing is judged early, however good it looks", repeat(good, 10), Pending, ""},
		{"a p2pool that never syncs fails",
			repeat(Observation{P2PoolExpected: true, MinerAllowed: true, Hashing: true}, 16), Failed, "did not sync"},
		{"syncing late in the window is enough",
			append(repeat(Observation{P2PoolExpected: true, MinerAllowed: true, Hashing: true}, 14), repeat(good, 2)...), Healthy, ""},
		{"an island at the end fails",
			append(repeat(good, 15), Observation{P2PoolExpected: true, P2PoolSynced: true, Island: "sidechain height 12 is shorter than its window"}), Failed, "island"},
		{"an island that cleared does not",
			append(repeat(Observation{P2PoolExpected: true, P2PoolSynced: true, Island: "no peers"}, 8), repeat(good, 8)...), Healthy, ""},
		{"an xmrig that never hashes while allowed to fails",
			repeat(Observation{P2PoolExpected: true, P2PoolSynced: true, MinerAllowed: true}, 16), Failed, "xmrig did not hash"},
		{"an xmrig never allowed to run cannot be failed",
			// A Nodo that does not mine on itself runs p2pool alone.
			repeat(Observation{P2PoolExpected: true, P2PoolSynced: true}, 16), Healthy, ""},
		{"a device mining to a hub has no p2pool to sync",
			repeat(Observation{MinerAllowed: true, Hashing: true}, 16), Healthy, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := NewCheck(t0)
			var v Verdict
			var reason string
			for i, o := range c.obs {
				v, reason = ch.Observe(t0.Add(time.Duration(i)*time.Minute), o)
			}
			if v != c.want || !strings.Contains(reason, c.reason) {
				t.Errorf("got %v %q, want %v containing %q", v, reason, c.want, c.reason)
			}
		})
	}
}

func repeat(o Observation, n int) []Observation {
	out := make([]Observation, n)
	for i := range out {
		out[i] = o
	}
	return out
}

func TestChoose(t *testing.T) {
	cur := &Engines{ID: "new", Version: "xmrig 6.27.0 · p2pool 4.19"}
	prev := &Engines{ID: "old", Version: "xmrig 6.26.0 · p2pool 4.18"}
	cases := []struct {
		name      string
		cur, prev *Engines
		st        State
		use       *Engines
		probation bool
		amber     string
	}{
		{"not a packaged install", nil, nil, State{}, nil, false, ""},
		{"a new pair goes on probation", cur, prev, State{Healthy: "old"}, cur, true, ""},
		{"a first install too", cur, nil, State{}, cur, true, ""},
		{"a pair that passed runs as is", cur, prev, State{Healthy: "new"}, cur, false, ""},
		{"a failed pair gives way to the previous one", cur, prev,
			State{Healthy: "old", Rejected: "new", Reason: "p2pool did not sync"}, prev, false, "went back to xmrig 6.26.0"},
		{"with nothing to go back to it runs, flagged", cur, nil,
			State{Rejected: "new", Reason: "xmrig did not hash"}, cur, false, "no earlier pair"},
		{"a rebuild of the same versions is told apart by build", &Engines{ID: "aaaaaaaa11", Version: "xmrig 6.26.0 · p2pool 4.18"},
			&Engines{ID: "bbbbbbbb22", Version: "xmrig 6.26.0 · p2pool 4.18"},
			State{Healthy: "bbbbbbbb22", Rejected: "aaaaaaaa11", Reason: "x"}, &Engines{ID: "bbbbbbbb22"}, false,
			"4.18 (build aaaaaaaa) failed its health check (x), so this machine went back to xmrig 6.26.0 · p2pool 4.18 (build bbbbbbbb)"},
		{"a previous pair never seen working is not a way back", cur, prev,
			State{Healthy: "older", Rejected: "new", Reason: "x"}, cur, false, "no earlier pair"},
		{"a reinstall of the same pair is not a way back", cur, &Engines{ID: "new"},
			State{Rejected: "new", Reason: "x"}, cur, false, "no earlier pair"},
		{"the next release is judged afresh", &Engines{ID: "newer"}, cur,
			State{Rejected: "new"}, &Engines{ID: "newer"}, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Choose(c.cur, c.prev, c.st)
			if (got.Use == nil) != (c.use == nil) || (got.Use != nil && got.Use.ID != c.use.ID) ||
				got.Probation != c.probation || !strings.Contains(got.RolledBack, c.amber) || (c.amber == "") != (got.RolledBack == "") {
				t.Errorf("got %+v", got)
			}
		})
	}
}

func TestLoadAndState(t *testing.T) {
	dir := t.TempDir()
	if e, err := Load(filepath.Join(dir, "none")); e != nil || err != nil {
		t.Errorf("a missing pair: %v, %v; want nothing", e, err)
	}
	write := func(name, data string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("engines.json", `{"xmrig":"6.26.0","p2pool":"4.18","xmrig_sha256":"aa","p2pool_sha256":"bb"}`)
	if _, err := Load(dir); err == nil {
		t.Error("a manifest without its binaries loaded")
	}
	write("xmrig", "x")
	write("p2pool", "p")
	e, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e.Version != "xmrig 6.26.0 · p2pool 4.18" || e.XMRig != filepath.Join(dir, "xmrig") || e.ID == "" {
		t.Errorf("loaded %+v", e)
	}
	write("engines.json", `{"xmrig":"6.26.0","p2pool":"4.18","xmrig_sha256":"a2","p2pool_sha256":"bb"}`)
	if e2, _ := Load(dir); e2.ID == e.ID {
		t.Error("a different build of the same versions has the same ID")
	}

	path := filepath.Join(dir, "state", "engines.json")
	if st, err := ReadState(path); err != nil || st != (State{}) {
		t.Errorf("missing state: %+v, %v", st, err)
	}
	want := State{Healthy: "a", Rejected: "b", Reason: "p2pool did not sync", At: time.Unix(1, 0).UTC()}
	if err := WriteState(path, want); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadState(path); err != nil || got != want {
		t.Errorf("round trip: %+v, %v", got, err)
	}
}
