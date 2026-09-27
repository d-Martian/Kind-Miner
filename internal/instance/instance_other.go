//go:build !(linux || freebsd || openbsd || netbsd)

package instance

// Without a session bus there is no shared place to claim. macOS already
// keeps an app bundle to one instance; elsewhere this is the behaviour
// kind-miner always had.
func claim(string, func()) (Status, func()) { return Unavailable, func() {} }
