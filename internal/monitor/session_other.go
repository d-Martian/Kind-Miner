//go:build !(linux || freebsd || openbsd || netbsd)

package monitor

// probeLockBackend has no implementation on these platforms yet, so the lock
// state stays unknown and the setting that depends on it stays inert.
func probeLockBackend() lockBackend { return nil }
