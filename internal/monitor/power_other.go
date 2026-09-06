//go:build !linux

package monitor

// readEnergyMicrojoules has no portable equivalent outside Linux's powercap
// interface. Reporting "not available" keeps the dashboard showing "—" rather
// than a modelled figure presented as a measurement.
func readEnergyMicrojoules() (energy, wrapAt uint64, ok bool) { return 0, 0, false }
