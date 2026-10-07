package msr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// saved is what Apply found in the registers before it wrote them, so Restore
// can put the machine back exactly as it was: per CPU, per register.
type saved struct {
	Preset string                       `json:"preset"`
	Values map[string]map[string]uint64 `json:"values"` // cpu → register (hex) → value
}

// Apply writes p to every CPU. The registers' values before the first Apply
// are kept at statePath and never overwritten by a later one, so Restore
// always goes back to the machine's own settings, however often this runs —
// at every boot and resume, with --at-boot.
func Apply(d Device, p Preset, statePath string) error {
	cpus, err := d.CPUs()
	if err != nil {
		return err
	}
	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		s := saved{Preset: p.Name, Values: map[string]map[string]uint64{}}
		for _, cpu := range cpus {
			regs := map[string]uint64{}
			for _, it := range p.Items {
				v, err := d.Read(cpu, it.Reg)
				if err != nil {
					return err
				}
				regs[regKey(it.Reg)] = v
			}
			s.Values[strconv.Itoa(cpu)] = regs
		}
		if err := writeState(statePath, s); err != nil {
			return fmt.Errorf("saving the original register values: %w", err)
		}
	}
	for _, cpu := range cpus {
		for _, it := range p.Items {
			v := it.Value
			if it.Mask != NoMask {
				old, err := d.Read(cpu, it.Reg)
				if err != nil {
					return err
				}
				v = Masked(old, it.Value, it.Mask)
			}
			if err := d.Write(cpu, it.Reg, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// Restore writes back the values Apply saved, and forgets them.
func Restore(d Device, statePath string) error {
	data, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("the MSR boost was never turned on here; nothing to undo")
	}
	if err != nil {
		return err
	}
	var s saved
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("%s is unreadable: %w", statePath, err)
	}
	for cpuStr, regs := range s.Values {
		cpu, err := strconv.Atoi(cpuStr)
		if err != nil {
			continue
		}
		for regStr, v := range regs {
			reg, err := strconv.ParseUint(regStr, 0, 32)
			if err != nil {
				continue
			}
			if err := d.Write(cpu, uint32(reg), v); err != nil {
				return err
			}
		}
	}
	return os.Remove(statePath)
}

// Applied reports whether every CPU's registers hold p's values.
func Applied(d Device, p Preset) (bool, error) {
	cpus, err := d.CPUs()
	if err != nil {
		return false, err
	}
	for _, cpu := range cpus {
		for _, it := range p.Items {
			v, err := d.Read(cpu, it.Reg)
			if err != nil {
				return false, err
			}
			if v&it.Mask != it.Value&it.Mask {
				return false, nil
			}
		}
	}
	return true, nil
}

func regKey(reg uint32) string { return fmt.Sprintf("%#x", reg) }

func writeState(path string, s saved) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
