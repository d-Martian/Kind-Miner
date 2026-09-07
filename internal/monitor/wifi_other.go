//go:build !linux

package monitor

// readWiFiPowerSave has no implementation outside Linux, where the setting is
// read from NetworkManager. Reporting "unknown" keeps the warning silent rather
// than claiming a link is fine when nothing was checked.
func readWiFiPowerSave() (iface string, enabled bool, ok bool) { return "", false, false }
