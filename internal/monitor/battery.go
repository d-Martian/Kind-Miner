package monitor

// Battery checks whether the system is running on battery power.
type Battery struct{}

// NewBattery returns a Battery monitor.
func NewBattery() *Battery { return &Battery{} }

// OnBattery returns true when the system is discharging (not plugged in).
// Returns (false, nil) on desktop systems with no battery.
func (b *Battery) OnBattery() (bool, error) {
	return batteryDischarging()
}
