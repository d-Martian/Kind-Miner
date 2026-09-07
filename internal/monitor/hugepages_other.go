//go:build !linux

package monitor

// readHugePages has no portable equivalent outside Linux's /proc/meminfo.
// Reporting "unknown" keeps the sizing warning silent rather than guessing.
func readHugePages() (total, free, pageSize int64, ok bool) { return 0, 0, 0, false }
