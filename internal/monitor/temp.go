package monitor

import (
	"strings"

	"github.com/shirou/gopsutil/v3/host"
)

// Temp reads CPU temperature sensors.
type Temp struct{}

// NewTemp returns a Temp monitor.
func NewTemp() *Temp { return &Temp{} }

// MaxCPU returns the highest temperature reading across all CPU sensors in Celsius.
// Returns (0, nil) if no sensors are available (common on VMs and some platforms).
func (t *Temp) MaxCPU() (float64, error) {
	sensors, err := host.SensorsTemperatures()
	if err != nil || len(sensors) == 0 {
		return 0, nil
	}
	var max float64
	for _, s := range sensors {
		if isCPUSensor(s.SensorKey) && s.Temperature > max {
			max = s.Temperature
		}
	}
	return max, nil
}

func isCPUSensor(key string) bool {
	lower := strings.ToLower(key)
	for _, kw := range []string{"cpu", "core", "package", "coretemp", "k10temp", "zenpower"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
