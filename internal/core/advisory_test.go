package core

import (
	"testing"

	"github.com/kind-miner/kind-miner/internal/config"
)

func TestHoldsFromTwoSources(t *testing.T) {
	cases := []struct {
		name  string
		holds map[string]string
		want  string
	}{
		{"nothing holds", map[string]string{}, ""},
		{"the island alone", map[string]string{"island": "p2pool has not synced the sidechain yet"}, "p2pool has not synced the sidechain yet"},
		{"the update first, since the user can act on it",
			map[string]string{"island": "no peers", "advisory": "Update needed: p2pool 4.17 is below the safe minimum, 4.18"},
			"Update needed: p2pool 4.17 is below the safe minimum, 4.18; no peers"},
	}
	for _, c := range cases {
		if got := combineHolds(c.holds); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestReleasingOneHoldKeepsTheOther(t *testing.T) {
	s := New(config.Defaults())
	s.setHold("advisory", "Update needed")
	s.setHold("island", "no peers")
	s.setHold("island", "") // the island check sees a healthy chain
	if got := combineHolds(s.holds); got != "Update needed" {
		t.Errorf("after the island released: %q — its release cleared the advisory's hold", got)
	}
}

func TestEngineVersionsForTheAdvisory(t *testing.T) {
	t.Setenv("STATE_DIRECTORY", t.TempDir())
	exe, _ := bundle(t, false)
	cfg := config.Defaults()
	cfg.P2PoolBinPath = "/opt/my-p2pool/p2pool"
	s := New(cfg)
	if err := s.resolveEngines(exe, true, func(Step) {}); err != nil {
		t.Fatal(err)
	}
	if s.bin.versions["xmrig"] != "6.26.0" {
		t.Errorf("the bundled xmrig's version = %q", s.bin.versions["xmrig"])
	}
	if v, ok := s.bin.versions["p2pool"]; ok {
		t.Errorf("the user's own p2pool has a version, %q; the advisory could stop it", v)
	}
}
