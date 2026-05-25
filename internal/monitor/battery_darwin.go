//go:build darwin

package monitor

import (
	"os/exec"
	"strings"
)

func batteryDischarging() (bool, error) {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return false, nil
	}
	// "Now drawing from 'Battery Power'" appears when on battery.
	return strings.Contains(string(out), "Battery Power"), nil
}
