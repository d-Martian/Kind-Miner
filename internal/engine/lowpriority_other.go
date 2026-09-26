//go:build !linux

package engine

import "time"

// Outside Linux the miner keeps the demotion setPriority gives it; there is no
// scope, OOM score or I/O class to set.

func launchCommand(bin string, args []string) (string, []string, bool) { return bin, args, false }

func demote(int, bool) {}

func awaitExec(int, string, time.Duration) error { return nil }

func settle(int, bool, <-chan struct{}) {}

func describeLowPriority(bool) string { return "" }
