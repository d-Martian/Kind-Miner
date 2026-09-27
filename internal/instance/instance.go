// Package instance keeps kind-miner to one running copy per session, and turns
// a second launch into "show me the one that is running".
//
// Two copies are worse than useless: two supervisors, two xmrig processes
// fighting for the same cores, two p2pools for the same wallet. It happens
// easily — kind-miner starts with the computer, and the user then clicks its
// icon to look at it. On a desktop without a tray (stock GNOME) that click is
// also the only way back to a window that was closed, so the second launch
// must reach the first and open its dashboard rather than silently exit.
package instance

// Status is how a Claim went.
type Status int

const (
	// Held: this process is the instance, and a later launch will reach it.
	Held Status = iota
	// Unavailable: there is no session bus to claim on, so this process runs
	// as kind-miner always did — alone, as far as it can tell, and
	// unreachable by a later launch.
	Unavailable
	// Running: another instance holds the name. It has been asked to show
	// itself; the caller should exit.
	Running
)

// AppID is kind-miner's reverse-DNS identity — the desktop file, the Flatpak
// ID, and the single-instance bus name. The GUI and kind-minerd claim the same
// name, so the two never mine side by side.
const AppID = "io.github.kind_miner.KindMiner"

// Claim tries to become the running instance for appID. activate is called,
// on some goroutine, each time a later launch asks to be shown. release gives
// the name up, for shutdown.
func Claim(appID string, activate func()) (Status, func()) {
	return claim(appID, activate)
}
