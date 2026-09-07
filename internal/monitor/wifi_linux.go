//go:build linux

package monitor

import (
	"github.com/godbus/dbus/v5"
)

const (
	nmService  = "org.freedesktop.NetworkManager"
	nmPath     = "/org/freedesktop/NetworkManager"
	nmActive   = nmService + ".Connection.Active"
	nmDevice   = nmService + ".Device"
	nmSettings = nmService + ".Settings.Connection"

	// NM_DEVICE_TYPE_WIFI
	nmDeviceTypeWiFi uint32 = 2
)

// readWiFiPowerSave asks NetworkManager about the active wireless connection.
//
// NetworkManager is used rather than nl80211 because it needs no subprocess and
// no new dependency — godbus is already vendored for the tray and the idle and
// lock monitors — and because it is the layer a user changes to fix this
// (`nmcli con modify <name> wifi.powersave 2`). The cost is that NM reports the
// configured intent rather than the radio's live state; powerSaveEnabled
// documents how the ambiguous values are read.
//
// Every failure path returns "unknown": a machine with no NetworkManager, no
// wireless device, or no active wireless connection has nothing to warn about.
func readWiFiPowerSave() (iface string, enabled bool, ok bool) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return "", false, false
	}
	var active []dbus.ObjectPath
	if err := conn.Object(nmService, nmPath).
		StoreProperty(nmService+".ActiveConnections", &active); err != nil {
		return "", false, false
	}
	for _, ac := range active {
		acObj := conn.Object(nmService, ac)
		var devices []dbus.ObjectPath
		if err := acObj.StoreProperty(nmActive+".Devices", &devices); err != nil {
			continue
		}
		name, isWiFi := wirelessInterface(conn, devices)
		if !isWiFi {
			continue
		}
		var settingsPath dbus.ObjectPath
		if err := acObj.StoreProperty(nmActive+".Connection", &settingsPath); err != nil {
			continue
		}
		var settings map[string]map[string]dbus.Variant
		if err := conn.Object(nmService, settingsPath).
			Call(nmSettings+".GetSettings", 0).Store(&settings); err != nil {
			continue
		}
		// A missing key is NM's default (0), which powerSaveEnabled treats as on.
		var setting uint32 = nmPowerSaveDefault
		if v, found := settings["802-11-wireless"]["powersave"]; found {
			_ = v.Store(&setting)
		}
		return name, powerSaveEnabled(setting), true
	}
	return "", false, false
}

// wirelessInterface returns the name of the first Wi-Fi device in devices.
func wirelessInterface(conn *dbus.Conn, devices []dbus.ObjectPath) (string, bool) {
	for _, d := range devices {
		devObj := conn.Object(nmService, d)
		var kind uint32
		if err := devObj.StoreProperty(nmDevice+".DeviceType", &kind); err != nil {
			continue
		}
		if kind != nmDeviceTypeWiFi {
			continue
		}
		var name string
		if err := devObj.StoreProperty(nmDevice+".Interface", &name); err != nil {
			continue
		}
		return name, true
	}
	return "", false
}
