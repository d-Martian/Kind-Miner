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

func claim(ids []string, activate func()) (Status, func()) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return Unavailable, func() {}
	}
	// Answer under every name, so an older version's launcher, which only
	// knows a former one, still brings this window forward.
	for _, id := range ids {
		if err := conn.Export(activator{activate}, objectPath(id), id); err != nil {
			conn.Close()
			return Unavailable, func() {}
		}
	}
	var held []string
	for i, id := range ids {
		reply, err := conn.RequestName(id, dbus.NameFlagDoNotQueue)
		if err != nil {
			if i == 0 {
				conn.Close()
				return Unavailable, func() {}
			}
			// A former name this process may not own: the current one still
			// keeps two of this version apart, so carry on without it.
			log.Printf("instance: could not claim the former name %s (%v)", id, err)
			continue
		}
		if reply == dbus.RequestNameReplyPrimaryOwner || reply == dbus.RequestNameReplyAlreadyOwner {
			held = append(held, id)
			continue
		}
		// Someone else is the instance, under this name: give back what was
		// taken and ask it to come forward.
		for _, h := range held {
			conn.ReleaseName(h)
		}
		call := conn.Object(id, objectPath(id)).Call(id+".Activate", 0)
		if call.Err != nil {
			log.Printf("kind-miner is already running but did not answer (%v)", call.Err)
		}
		conn.Close()
		return Running, func() {}
	}
	return Held, func() {
		for _, h := range held {
			conn.ReleaseName(h)
		}
		conn.Close()
	}
}
