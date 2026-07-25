// Package autostart registers kind-miner to launch when the user logs in.
//
// kind-miner is meant to be set-and-forget: it only earns while it is running,
// so an app the user has to remember to start defeats the point. Each platform
// has its own mechanism (XDG autostart entry, LaunchAgent, registry Run key),
// all hidden behind Enable/Disable/Enabled.
package autostart

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrStateUnknown is returned by Enabled when the platform offers no way to
// read back the current setting. The Flatpak background portal is the case that
// matters: it can request autostart but not report it. Callers should fall back
// to their own stored preference rather than treating this as "off".
var ErrStateUnknown = errors.New("autostart state cannot be determined on this platform")

// Options describes the login entry to create.
type Options struct {
	// ID is the reverse-DNS application id. It names the desktop file, the
	// LaunchAgent label, and the registry value.
	ID string
	// Name is the human-readable name shown in the desktop environment's
	// startup-applications list.
	Name string
	// Exec is the absolute path to launch. Empty means ExecPath().
	Exec string
	// Args are passed to Exec at login.
	Args []string
}

// resolve fills in defaults that depend on the running process.
func (o Options) resolve() (Options, error) {
	if o.Exec == "" {
		p, err := ExecPath()
		if err != nil {
			return o, err
		}
		o.Exec = p
	}
	if o.Name == "" {
		o.Name = filepath.Base(o.Exec)
	}
	return o, nil
}

// Enable registers the app to start at login. It is idempotent.
func Enable(o Options) error {
	o, err := o.resolve()
	if err != nil {
		return err
	}
	return enable(o)
}

// Disable removes the login registration. Disabling when nothing is registered
// is not an error.
func Disable(o Options) error {
	o, err := o.resolve()
	if err != nil {
		return err
	}
	return disable(o)
}

// Enabled reports whether the app is currently registered to start at login.
// It may return ErrStateUnknown — see that variable.
func Enabled(o Options) (bool, error) {
	o, err := o.resolve()
	if err != nil {
		return false, err
	}
	return enabled(o)
}

// ExecPath returns the command to relaunch at login.
//
// Under an AppImage, os.Executable() points into the /tmp/.mount_* directory
// the image is extracted to, which no longer exists by the next login; $APPIMAGE
// is the durable path to the .AppImage file itself.
func ExecPath() (string, error) {
	if p := os.Getenv("APPIMAGE"); p != "" {
		return p, nil
	}
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Resolve symlinks so a relocated /usr/local/bin link doesn't outlive its
	// target, but keep the original path if resolution fails.
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	return p, nil
}
