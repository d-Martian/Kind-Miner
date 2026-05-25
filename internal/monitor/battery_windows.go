//go:build windows

package monitor

import "golang.org/x/sys/windows"

func batteryDischarging() (bool, error) {
	var status windows.SYSTEM_POWER_STATUS
	if err := windows.GetSystemPowerStatus(&status); err != nil {
		return false, err
	}
	// ACLineStatus: 0 = offline (battery), 1 = online (AC), 255 = unknown
	return status.ACLineStatus == 0, nil
}
