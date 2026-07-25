//go:build linux || freebsd || openbsd || netbsd

package autostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempDir points the autostart directory at a temp dir and forces the
// non-Flatpak code path, so the test never touches the real session config.
func withTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origDir, origFlatpak := autostartDir, inFlatpak
	autostartDir = func() (string, error) { return dir, nil }
	inFlatpak = func() bool { return false }
	t.Cleanup(func() { autostartDir, inFlatpak = origDir, origFlatpak })
	return dir
}

func testOptions() Options {
	return Options{ID: "io.github.kind_miner.KindMiner", Name: "kind-miner", Exec: "/usr/bin/kind-miner"}
}

func TestEnableDisableRoundTrip(t *testing.T) {
	dir := withTempDir(t)
	o := testOptions()

	if on, err := Enabled(o); err != nil || on {
		t.Fatalf("Enabled() = %v, %v; want false, nil", on, err)
	}

	if err := Enable(o); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	path := filepath.Join(dir, o.ID+".desktop")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("desktop file not written: %v", err)
	}
	for _, want := range []string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=kind-miner",
		"Exec=/usr/bin/kind-miner",
		"X-GNOME-Autostart-enabled=true",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("desktop file missing %q:\n%s", want, body)
		}
	}

	if on, err := Enabled(o); err != nil || !on {
		t.Fatalf("Enabled() after Enable = %v, %v; want true, nil", on, err)
	}

	// Enabling twice must not fail or duplicate.
	if err := Enable(o); err != nil {
		t.Fatalf("second Enable() error = %v", err)
	}

	if err := Disable(o); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("desktop file still present after Disable: %v", err)
	}

	// Disabling when nothing is registered is not an error.
	if err := Disable(o); err != nil {
		t.Errorf("second Disable() error = %v", err)
	}
}

// A path containing a space must survive as one argument, or the session tries
// to launch a truncated command with a stray argument.
func TestExecLineQuoting(t *testing.T) {
	tests := []struct {
		name string
		exec string
		args []string
		want string
	}{
		{"plain", "/usr/bin/kind-miner", nil, "/usr/bin/kind-miner"},
		{"space in path", "/home/a b/kind-miner", nil, `"/home/a b/kind-miner"`},
		{"with args", "/usr/bin/kind-miner", []string{"--headless"}, "/usr/bin/kind-miner --headless"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := execLine(tt.exec, tt.args); got != tt.want {
				t.Errorf("execLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Exercises the real directory lookup rather than the injected one, so a broken
// XDG_CONFIG_HOME path would be caught here instead of at a user's login.
func TestRespectsXDGConfigHome(t *testing.T) {
	orig := inFlatpak
	inFlatpak = func() bool { return false }
	t.Cleanup(func() { inFlatpak = orig })

	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	o := testOptions()
	if err := Enable(o); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	want := filepath.Join(root, "autostart", o.ID+".desktop")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected desktop file at %s: %v", want, err)
	}
	if on, err := Enabled(o); err != nil || !on {
		t.Errorf("Enabled() = %v, %v; want true, nil", on, err)
	}
}

// Inside Flatpak the state is not readable; callers must be able to detect that
// rather than treating it as "off" and re-requesting on every launch.
func TestEnabledUnknownInFlatpak(t *testing.T) {
	withTempDir(t)
	orig := inFlatpak
	inFlatpak = func() bool { return true }
	t.Cleanup(func() { inFlatpak = orig })

	if _, err := Enabled(testOptions()); err != ErrStateUnknown {
		t.Errorf("Enabled() error = %v, want ErrStateUnknown", err)
	}
}
