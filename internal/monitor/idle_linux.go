//go:build linux || freebsd || openbsd || netbsd

package monitor

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/screensaver"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/godbus/dbus/v5"
)

// probeIdleBackend picks the first mechanism that answers on this session.
//
// Order matters. Under Wayland the X11 fallback only sees input delivered to
// XWayland clients, so it reports the user as idle while they type in any
// native Wayland window — the D-Bus interfaces, which the compositor itself
// implements, must be tried first.
func probeIdleBackend() idleBackend {
	candidates := []idleBackend{
		&mutterIdle{},      // GNOME (X11 and Wayland)
		&screenSaverIdle{}, // KDE and others exposing org.freedesktop.ScreenSaver
		&x11Idle{},         // any X11 session, and XWayland as a last resort
	}
	for _, c := range candidates {
		if _, err := c.idleTime(); err == nil {
			log.Printf("idle detection: using %s", c.name())
			return c
		}
	}
	log.Printf("idle detection: no supported backend; mining will not wait for you to go idle")
	return nil
}

// mutterIdle reads GNOME's idle monitor. Mutter is the compositor, so this is
// accurate on both X11 and Wayland, and it is reachable from inside Flatpak
// through the session bus the manifest already grants.
type mutterIdle struct{ conn *dbus.Conn }

func (m *mutterIdle) name() string { return "org.gnome.Mutter.IdleMonitor" }

func (m *mutterIdle) idleTime() (time.Duration, error) {
	if m.conn == nil {
		conn, err := dbus.SessionBus()
		if err != nil {
			return 0, err
		}
		m.conn = conn
	}
	obj := m.conn.Object("org.gnome.Mutter.IdleMonitor", "/org/gnome/Mutter/IdleMonitor/Core")
	var ms uint64
	if err := obj.Call("org.gnome.Mutter.IdleMonitor.GetIdletime", 0).Store(&ms); err != nil {
		return 0, err
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// screenSaverIdle reads GetSessionIdleTime, which KDE's screen locker exposes on
// org.freedesktop.ScreenSaver. GNOME owns the same bus name but rejects this
// method, which is why mutterIdle is tried first.
type screenSaverIdle struct{ conn *dbus.Conn }

func (s *screenSaverIdle) name() string { return "org.freedesktop.ScreenSaver" }

func (s *screenSaverIdle) idleTime() (time.Duration, error) {
	if s.conn == nil {
		conn, err := dbus.SessionBus()
		if err != nil {
			return 0, err
		}
		s.conn = conn
	}
	obj := s.conn.Object("org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver")
	// Seconds here, unlike Mutter's milliseconds.
	var secs uint32
	if err := obj.Call("org.freedesktop.ScreenSaver.GetSessionIdleTime", 0).Store(&secs); err != nil {
		return 0, err
	}
	return time.Duration(secs) * time.Second, nil
}

// x11Idle uses the X11 MIT-SCREEN-SAVER extension. The binding is pure Go, so
// this adds no C library to the build.
type x11Idle struct {
	conn *xgb.Conn
	root xproto.Drawable
}

func (x *x11Idle) name() string { return "X11 MIT-SCREEN-SAVER" }

func (x *x11Idle) idleTime() (time.Duration, error) {
	if x.conn == nil {
		if os.Getenv("DISPLAY") == "" {
			return 0, fmt.Errorf("no DISPLAY")
		}
		conn, err := xgb.NewConn()
		if err != nil {
			return 0, err
		}
		if err := screensaver.Init(conn); err != nil {
			conn.Close()
			return 0, err
		}
		x.conn = conn
		x.root = xproto.Drawable(xproto.Setup(conn).DefaultScreen(conn).Root)
	}
	reply, err := screensaver.QueryInfo(x.conn, x.root).Reply()
	if err != nil {
		x.conn.Close()
		x.conn = nil
		return 0, err
	}
	return time.Duration(reply.MsSinceUserInput) * time.Millisecond, nil
}
