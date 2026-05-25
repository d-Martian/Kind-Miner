//go:build windows

package nodes

// tryStartTor is a no-op on Windows; Tor must be started by the user.
func tryStartTor() bool { return false }
