//go:build linux

package monitor

import (
	"os"
	"path/filepath"
	"strings"
)

func batteryDischarging() (bool, error) {
	// /sys/class/power_supply/BAT*/status contains "Discharging", "Charging", or "Full".
	matches, err := filepath.Glob("/sys/class/power_supply/BAT*/status")
	if err != nil || len(matches) == 0 {
		return false, nil // no battery
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) == "Discharging", nil
}
