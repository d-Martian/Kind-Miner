package monitor

// WiFiPowerSave reports whether the active wireless link is running with 802.11
// power save enabled, and which interface it is.
//
// This exists because of a measured failure, not a theoretical one. With power
// save on, the radio sleeps between beacons and has to be woken to be serviced;
// saturating the CPU with mining delays that path enough that the card misses
// service windows. On a laptop with the radio on the CPU package, hashing on 12
// threads drove round trips to the local gateway — one hop, no internet
// involved — from 4 ms to over 14 seconds, with a fifth of packets taking more
// than a second. The median stayed low, so the link looked healthy while
// browsers and VPN handshakes failed outright. Turning power save off removed
// the effect entirely: latency under the same load became indistinguishable
// from idle.
//
// A miner that makes the network unusable is not unnoticeable, whatever the CPU
// graph says, so kind-miner warns rather than letting the user spend a night
// blaming their ISP.
//
// ok is false where the answer cannot be determined — no wireless link, or no
// way to ask about it. Callers must not read that as "power save is off".
func WiFiPowerSave() (iface string, enabled bool, ok bool) { return readWiFiPowerSave() }

// NetworkManager's 802-11-wireless.powersave values.
const (
	nmPowerSaveDefault uint32 = 0
	nmPowerSaveIgnore  uint32 = 1
	nmPowerSaveDisable uint32 = 2
	nmPowerSaveEnable  uint32 = 3
)

// powerSaveEnabled maps a NetworkManager powersave setting to whether the radio
// actually sleeps.
//
// Only "disable" reliably means off. Both "default" and "ignore" leave the
// decision to the driver, and every mainline Linux wireless driver defaults to
// power save on — which is exactly the configuration that produced the stalls
// above, so reporting those as off would miss the case this check exists for.
func powerSaveEnabled(setting uint32) bool { return setting != nmPowerSaveDisable }
