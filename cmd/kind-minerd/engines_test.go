package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kind-miner/kind-miner/internal/rollout"
)

// pair lays out a packaged pair the way scripts/build-deb.sh does.
func pair(t *testing.T, dir, xmrig string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"engines.json": `{"xmrig":"` + xmrig + `","p2pool":"4.18","xmrig_sha256":"` + xmrig + `","p2pool_sha256":"bb"}`,
		"xmrig":        "x", "p2pool": "p",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEngineRollback(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()

	t.Run("an install with no packaged engines downloads as before", func(t *testing.T) {
		e := chooseEngines(root, state)
		if e.choice.Use != nil || e.choice.Probation {
			t.Errorf("choice %+v", e.choice)
		}
	})

	pair(t, filepath.Join(root, enginesDir), "6.26.0")
	e := chooseEngines(root, state)
	if e.choice.Use == nil || !e.choice.Probation || e.choice.Use.XMRig != filepath.Join(root, enginesDir, "xmrig") {
		t.Fatalf("first install: %+v", e.choice)
	}
	e.pass()
	if e = chooseEngines(root, state); e.choice.Probation {
		t.Error("a pair that passed is on probation again")
	}

	// An upgrade: the package keeps the old pair and installs a new one.
	if err := os.Rename(filepath.Join(root, enginesDir), filepath.Join(root, previousEnginesDir)); err != nil {
		t.Fatal(err)
	}
	pair(t, filepath.Join(root, enginesDir), "6.27.0")
	e = chooseEngines(root, state)
	if !e.choice.Probation || !strings.Contains(e.choice.Use.Version, "6.27.0") {
		t.Fatalf("after upgrade: %+v", e.choice)
	}
	if !e.reject("p2pool did not sync within 15m0s") {
		t.Fatal("a failed upgrade had nowhere to go back to")
	}

	e = chooseEngines(root, state)
	if !strings.Contains(e.choice.Use.Version, "6.26.0") || e.choice.Probation {
		t.Errorf("after the failure: %+v", e.choice)
	}
	if w := e.warning(); !strings.Contains(w, "6.27.0 · p2pool 4.18 failed its health check (p2pool did not sync") || !strings.Contains(w, "went back to xmrig 6.26.0") {
		t.Errorf("warning %q", w)
	}
	st, _ := rollout.ReadState(filepath.Join(state, "engines.json"))
	if st.Rejected == "" || st.Healthy == st.Rejected {
		t.Errorf("state %+v", st)
	}

	t.Run("a failure with nowhere to go says so while running", func(t *testing.T) {
		os.RemoveAll(filepath.Join(root, previousEnginesDir))
		pair(t, filepath.Join(root, enginesDir), "6.28.0")
		e := chooseEngines(root, state)
		if e.reject("xmrig did not hash") {
			t.Error("rolled back to nothing")
		}
		if !strings.Contains(e.warning(), "no earlier pair") {
			t.Errorf("warning %q", e.warning())
		}
	})
}
