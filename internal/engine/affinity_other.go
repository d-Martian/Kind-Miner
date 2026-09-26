//go:build !linux

package engine

func setAffinity(pid int, cpus []int) error { return nil }
