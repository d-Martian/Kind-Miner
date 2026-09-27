//go:build linux || freebsd || openbsd || netbsd

package monitor

import "github.com/godbus/dbus/v5"

// dbusGameMode reads the ClientCount property the GameMode daemon publishes on
// the session bus.
type dbusGameMode struct {
	conn *dbus.Conn
}

func newGameModeBackend() gameModeBackend { return &dbusGameMode{} }

func (d *dbusGameMode) clientCount() (int, error) {
	if d.conn == nil {
		conn, err := dbus.SessionBus()
		if err != nil {
			return 0, err
		}
		d.conn = conn
	}
	obj := d.conn.Object("com.feralinteractive.GameMode", "/com/feralinteractive/GameMode")
	v, err := obj.GetProperty("com.feralinteractive.GameMode.ClientCount")
	if err != nil {
		return 0, err
	}
	switch n := v.Value().(type) {
	case int32:
		return int(n), nil
	case uint32:
		return int(n), nil
	}
	return 0, nil
}
