//go:build !linux

package monitor

// readL3CacheBytes has no portable equivalent outside Linux's sysfs cache
// topology. Reporting "unknown" leaves the thread count falling back to the
// core count, which is what this code did on every platform before.
func readL3CacheBytes() (int64, bool) { return 0, false }
