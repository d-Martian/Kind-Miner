//go:build linux || freebsd || openbsd || netbsd

package autostart

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// portalTimeout bounds the wait for the portal's Response signal. The portal may
// show a permission dialog, so this has to allow for a human, but it must not
// hang the settings window forever if no portal implementation answers.
const portalTimeout = 2 * time.Minute

// portalAutostart asks the desktop's background portal to create (or remove) a
// host-side autostart entry for this Flatpak app.
//
// Inside a sandbox we cannot write the host's ~/.config/autostart ourselves: the
// path we can see is the app's private config dir, and an entry written there is
// never read by the session. The portal is the only supported route, and it runs
// the app via `flatpak run <app-id>` on the host.
//
// Spec: org.freedesktop.portal.Background.RequestBackground.
func portalAutostart(o Options, enable bool) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("session bus: %w", err)
	}

	// The portal replies asynchronously on a Request object whose path is
	// derived from our bus name and a token we choose. Subscribing before the
	// call is what avoids losing a fast response.
	token := fmt.Sprintf("kindminer_%d", time.Now().UnixNano())
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	reqPath := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + sender + "/" + token)

	match := []dbus.MatchOption{
		dbus.WithMatchObjectPath(reqPath),
		dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response"),
	}
	if err := conn.AddMatchSignal(match...); err != nil {
		return fmt.Errorf("subscribe to portal response: %w", err)
	}
	defer conn.RemoveMatchSignal(match...)

	sigCh := make(chan *dbus.Signal, 4)
	conn.Signal(sigCh)
	defer conn.RemoveSignal(sigCh)

	// Inside the sandbox the command is the in-sandbox command name; the portal
	// wraps it in `flatpak run` on the host side.
	cmd := append([]string{filepath.Base(o.Exec)}, o.Args...)
	opts := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token),
		"reason":       dbus.MakeVariant("Start " + o.Name + " when you log in"),
		"autostart":    dbus.MakeVariant(enable),
		"commandline":  dbus.MakeVariant(cmd),
	}

	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	var handle dbus.ObjectPath
	if err := obj.Call("org.freedesktop.portal.Background.RequestBackground", 0, "", opts).Store(&handle); err != nil {
		return fmt.Errorf("background portal: %w", err)
	}

	deadline := time.After(portalTimeout)
	for {
		select {
		case sig := <-sigCh:
			if sig.Path != reqPath || len(sig.Body) < 2 {
				continue
			}
			code, _ := sig.Body[0].(uint32)
			switch code {
			case 0: // success
				// The portal reports what it actually granted, which can differ
				// from what we asked for.
				if results, ok := sig.Body[1].(map[string]dbus.Variant); ok {
					if v, ok := results["autostart"]; ok {
						var granted bool
						if err := v.Store(&granted); err == nil && granted != enable {
							return fmt.Errorf("desktop portal did not grant autostart")
						}
					}
				}
				return nil
			case 1:
				return fmt.Errorf("autostart request was cancelled")
			default:
				return fmt.Errorf("autostart request failed (portal response %d)", code)
			}
		case <-deadline:
			return fmt.Errorf("timed out waiting for the desktop portal")
		}
	}
}
