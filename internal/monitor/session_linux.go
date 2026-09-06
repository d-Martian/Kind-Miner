//go:build linux || freebsd || openbsd || netbsd

package monitor

import (
	"log"

	"github.com/godbus/dbus/v5"
)

// probeLockBackend picks the first screen-lock interface that answers.
//
// The order mirrors idle detection: GNOME owns the org.freedesktop.ScreenSaver
// bus name but implements only part of that interface, so its own name is tried
// first and the freedesktop one serves KDE and everything else.
func probeLockBackend() lockBackend {
	candidates := []lockBackend{
		&screenSaverLock{bus: "org.gnome.ScreenSaver", path: "/org/gnome/ScreenSaver"},
		&screenSaverLock{bus: "org.freedesktop.ScreenSaver", path: "/org/freedesktop/ScreenSaver"},
	}
	for _, c := range candidates {
		if _, err := c.locked(); err == nil {
			log.Printf("lock detection: using %s", c.name())
			return c
		}
	}
	log.Printf("lock detection: no supported backend; mining cannot be restricted to a locked screen")
	return nil
}

// screenSaverLock calls GetActive on a screensaver interface. Both GNOME's and
// freedesktop's expose the same method at their own bus name and object path.
type screenSaverLock struct {
	bus  string
	path string
	conn *dbus.Conn
}

func (s *screenSaverLock) name() string { return s.bus }

func (s *screenSaverLock) locked() (bool, error) {
	if s.conn == nil {
		conn, err := dbus.SessionBus()
		if err != nil {
			return false, err
		}
		s.conn = conn
	}
	obj := s.conn.Object(s.bus, dbus.ObjectPath(s.path))
	var active bool
	if err := obj.Call(s.bus+".GetActive", 0).Store(&active); err != nil {
		return false, err
	}
	return active, nil
}
