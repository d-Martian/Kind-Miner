//go:build linux || freebsd || openbsd || netbsd

package instance

import (
	"log"
	"strings"

	"github.com/godbus/dbus/v5"
)

// The name is the app ID itself: inside a Flatpak an app may own names under
// its own ID without extra permissions, so this works sandboxed and native
// alike.
func objectPath(appID string) dbus.ObjectPath {
	return dbus.ObjectPath("/" + strings.ReplaceAll(appID, ".", "/"))
}

type activator struct{ fn func() }

// Activate is the method a later launch calls.
func (a activator) Activate() *dbus.Error {
	if a.fn != nil {
		go a.fn()
	}
	return nil
}

func claim(appID string, activate func()) (Status, func()) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return Unavailable, func() {}
	}
	path := objectPath(appID)
	if err := conn.Export(activator{activate}, path, appID); err != nil {
		conn.Close()
		return Unavailable, func() {}
	}
	reply, err := conn.RequestName(appID, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return Unavailable, func() {}
	}
	if reply == dbus.RequestNameReplyPrimaryOwner || reply == dbus.RequestNameReplyAlreadyOwner {
		return Held, func() {
			conn.ReleaseName(appID)
			conn.Close()
		}
	}
	// Someone else is the instance: ask it to come forward.
	call := conn.Object(appID, path).Call(appID+".Activate", 0)
	if call.Err != nil {
		log.Printf("kind-miner is already running but did not answer (%v)", call.Err)
	}
	conn.Close()
	return Running, func() {}
}
