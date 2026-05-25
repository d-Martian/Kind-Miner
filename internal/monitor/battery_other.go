//go:build !linux && !darwin && !windows

package monitor

func batteryDischarging() (bool, error) {
	return false, nil // assume AC power on unsupported platforms
}
