package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kind-miner/kind-miner/internal/config"
)

// bundle lays out a packaged build the way the AppImage and Flatpak do:
// <prefix>/bin/kind-miner and <prefix>/lib/kind-miner/engines.
func bundle(t *testing.T, withTor bool) (exe, engines string) {
	t.Helper()
	prefix := t.TempDir()
	exe = filepath.Join(prefix, "bin", "kind-miner")
	engines = filepath.Join(prefix, "lib", "kind-miner", "engines")
	manifest := `{"xmrig":"6.26.0","p2pool":"4.18","xmrig_sha256":"a","p2pool_sha256":"b"`
	files := map[string]string{"xmrig": "x", "p2pool": "p"}
	if withTor {
		manifest += `,"tor":"15.0.15"`
		files["tor/tor"] = "t"
	}
	files["engines.json"] = manifest + "}"
	for name, data := range files {
		p := filepath.Join(engines, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return exe, engines
}

func TestResolveEngines(t *testing.T) {
	t.Setenv("STATE_DIRECTORY", t.TempDir()) // the managed download directory
	noop := func(Step) {}

	t.Run("a packaged build runs its own engines", func(t *testing.T) {
		exe, engines := bundle(t, true)
		s := New(config.Defaults())
		if err := s.resolveEngines(exe, true, noop); err != nil {
			t.Fatal(err)
		}
		if s.bin.xmrig != filepath.Join(engines, "xmrig") || s.bin.p2pool != filepath.Join(engines, "p2pool") || !s.bin.packaged {
			t.Errorf("bin = %+v", s.bin)
		}
		if s.cfg.XMRigBinPath != "" || s.cfg.P2PoolBinPath != "" {
			t.Error("resolved paths were written into the config, which the GUI saves")
		}
		if tor, err := s.torBinary(); err != nil || tor != filepath.Join(engines, "tor", "tor") {
			t.Errorf("tor = %q, %v", tor, err)
		}
	})
	t.Run("the user's own build wins over the bundle", func(t *testing.T) {
		exe, _ := bundle(t, false)
		cfg := config.Defaults()
		cfg.XMRigBinPath = "/opt/my-xmrig/xmrig"
		s := New(cfg)
		if err := s.resolveEngines(exe, true, noop); err != nil {
			t.Fatal(err)
		}
		if s.bin.xmrig != "/opt/my-xmrig/xmrig" {
			t.Errorf("xmrig = %q", s.bin.xmrig)
		}
	})
	t.Run("a download path an old version saved is not the user's own", func(t *testing.T) {
		exe, engines := bundle(t, false)
		cfg := config.Defaults()
		cfg.XMRigBinPath = filepath.Join(os.Getenv("STATE_DIRECTORY"), "bin", "xmrig")
		s := New(cfg)
		if err := s.resolveEngines(exe, true, noop); err != nil {
			t.Fatal(err)
		}
		if s.bin.xmrig != filepath.Join(engines, "xmrig") {
			t.Errorf("xmrig = %q", s.bin.xmrig)
		}
	})
	t.Run("the daemon's choice wins over everything", func(t *testing.T) {
		exe, _ := bundle(t, false)
		cfg := config.Defaults()
		cfg.XMRigBinPath = "/opt/my-xmrig/xmrig"
		s := New(cfg)
		s.UseEngines("/opt/kind-miner/engines.previous/xmrig", "/opt/kind-miner/engines.previous/p2pool")
		if err := s.resolveEngines(exe, true, noop); err != nil {
			t.Fatal(err)
		}
		if s.bin.xmrig != "/opt/kind-miner/engines.previous/xmrig" || s.bin.p2pool != "/opt/kind-miner/engines.previous/p2pool" {
			t.Errorf("bin = %+v", s.bin)
		}
	})
	t.Run("a packaged build without a Tor never downloads one", func(t *testing.T) {
		exe, _ := bundle(t, false)
		t.Setenv("PATH", t.TempDir()) // no tor installed
		s := New(config.Defaults())
		if err := s.resolveEngines(exe, true, noop); err != nil {
			t.Fatal(err)
		}
		if _, err := s.torBinary(); !errors.Is(err, errNoTor) {
			t.Errorf("err = %v, want errNoTor", err)
		}
	})
	t.Run("a device mining to a hub needs no p2pool", func(t *testing.T) {
		exe, _ := bundle(t, false)
		s := New(config.Defaults())
		if err := s.resolveEngines(exe, false, noop); err != nil || s.bin.p2pool != "" {
			t.Errorf("p2pool = %q, %v", s.bin.p2pool, err)
		}
	})
}
