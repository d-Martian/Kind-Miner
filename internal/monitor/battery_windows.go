//go:build windows

package monitor

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// golang.org/x/sys/windows has never wrapped GetSystemPowerStatus, so this
// file used to call functions that do not exist and the Windows build never
// compiled. The call is made the way x/sys makes its own: a lazy handle on
// kernel32 and the documented struct.

// systemPowerStatus is SYSTEM_POWER_STATUS from winbase.h.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

func batteryDischarging() (bool, error) {
	var status systemPowerStatus
	if r, _, err := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status))); r == 0 {
		return false, err
	}
	// ACLineStatus: 0 = offline (battery), 1 = online (AC), 255 = unknown
	return status.ACLineStatus == 0, nil
}
