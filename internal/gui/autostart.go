package gui

import (
	"errors"
	"log"

	"github.com/kind-miner/kind-miner/internal/autostart"
	"github.com/kind-miner/kind-miner/internal/config"
)

// autostartOptions describes kind-miner's login entry. Reusing appID keeps the
// desktop file, LaunchAgent label, and registry value aligned with the rest of
// the app's identity.
func autostartOptions() autostart.Options {
	return autostart.Options{ID: appID, Name: "kind-miner"}
}

// SetAutostart turns the OS login entry on or off.
func SetAutostart(on bool) error {
	if on {
		return autostart.Enable(autostartOptions())
	}
	return autostart.Disable(autostartOptions())
}

// EnsureAutostart re-registers the login entry when the config asks for it but
// the OS no longer has it — which happens when an AppImage is moved or renamed,
// since the entry records an absolute path. Failures are logged and ignored:
// not starting at login is a nuisance, not a reason to refuse to mine.
func EnsureAutostart(cfg *config.Config) {
	if cfg == nil || !cfg.RunAtStartup {
		return
	}
	on, err := autostart.Enabled(autostartOptions())
	if err != nil && !errors.Is(err, autostart.ErrStateUnknown) {
		log.Printf("autostart: cannot read current state: %v", err)
		return
	}
	// ErrStateUnknown (Flatpak) means re-request: the portal call is idempotent.
	if on && err == nil {
		return
	}
	if err := autostart.Enable(autostartOptions()); err != nil {
		log.Printf("autostart: could not re-register login entry: %v", err)
	}
}
