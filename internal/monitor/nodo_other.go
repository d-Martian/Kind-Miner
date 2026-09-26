//go:build !linux

package monitor

import "time"

func readMemory() (total, available int64, ok bool) { return 0, 0, false }

func readCoreCapacities() (map[int]int64, bool) { return nil, false }

func (p *ServicePressure) sample(time.Time) (pressured, known bool) { return false, false }
