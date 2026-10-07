// Package msr applies xmrig's "MSR mod": a handful of model-specific register
// writes that switch off CPU prefetchers RandomX gains nothing from, worth
// 10–15% hashrate on most x86 CPUs. xmrig does it itself only when it runs as
// root, and kind-miner never runs xmrig as root — so this is a separate,
// explicit, reversible root command (kind-miner msr on|off|status) that the
// user chooses to run.
//
// The registers are machine state: they stay set until a reboot or a resume
// from sleep, for every program, mining or not. Prefetchers off can make other
// work a little slower, which is why this is opt-in and never automatic.
package msr

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// NoMask means an item's value replaces the register whole.
const NoMask = ^uint64(0)

// Item is one register write. With a mask, only the masked bits come from
// Value and the rest are kept, as xmrig's MsrItem::maskedValue does.
type Item struct {
	Reg   uint32
	Value uint64
	Mask  uint64
}

// Preset is the set of writes for one CPU family.
type Preset struct {
	Name  string
	Items []Item
}

// The presets, copied from the xmrig kind-miner pins (6.26.0):
// src/crypto/rx/RxConfig.cpp (msrPresets) and, for which CPU gets which,
// src/backend/cpu/platform/BasicCpuInfo.cpp. Moving the xmrig pin means
// re-checking these against the new tag.
var (
	ryzen17h = Preset{"AMD Ryzen (Zen, Zen+, Zen 2)", []Item{
		{0xC0011020, 0, NoMask},
		{0xC0011021, 0x40, ^uint64(0x20)},
		{0xC0011022, 0x1510000, NoMask},
		{0xC001102b, 0x2000cc16, NoMask},
	}}
	ryzen19h = Preset{"AMD Ryzen (Zen 3)", []Item{
		{0xC0011020, 0x0004480000000000, NoMask},
		{0xC0011021, 0x001c000200000040, ^uint64(0x20)},
		{0xC0011022, 0xc000000401570000, NoMask},
		{0xC001102b, 0x2000cc10, NoMask},
	}}
	ryzenZen4 = Preset{"AMD Ryzen (Zen 4)", []Item{
		{0xC0011020, 0x0004400000000000, NoMask},
		{0xC0011021, 0x0004000000000040, ^uint64(0x20)},
		{0xC0011022, 0x8680000401570000, NoMask},
		{0xC001102b, 0x2040cc10, NoMask},
	}}
	ryzenZen5 = Preset{"AMD Ryzen (Zen 5)", ryzenZen4.Items}
	intel     = Preset{"Intel", []Item{{0x1a4, 0xf, NoMask}}}
)

// Masked is the value written for an item over a register's old value.
func Masked(old, value, mask uint64) uint64 { return (value & mask) | (old &^ mask) }

// Detect picks the preset for the CPU described by /proc/cpuinfo's text, the
// way xmrig does. ok is false for anything xmrig has no preset for — ARM, an
// AMD before Zen — and then nothing is written.
func Detect(cpuinfo string) (Preset, bool) {
	var vendor string
	family, model := -1, -1
	sc := bufio.NewScanner(strings.NewReader(cpuinfo))
	for sc.Scan() {
		key, val, found := strings.Cut(sc.Text(), ":")
		if !found {
			if vendor != "" {
				break // the first processor is enough; they are all alike
			}
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "vendor_id":
			vendor = val
		case "cpu family":
			family, _ = strconv.Atoi(val)
		case "model":
			model, _ = strconv.Atoi(val)
		}
	}
	switch vendor {
	case "GenuineIntel":
		return intel, true
	case "AuthenticAMD":
		switch family {
		case 0x17:
			return ryzen17h, true
		case 0x19:
			if model == 0x61 || model == 0x75 {
				return ryzenZen4, true
			}
			return ryzen19h, true
		case 0x1a:
			return ryzenZen5, true
		}
	}
	return Preset{}, false
}

// Device reads and writes registers through the kernel's msr driver:
// <Dir>/<cpu>/msr, where an 8-byte read or write at offset reg is that
// register on that CPU. Dir is /dev/cpu, or a directory of files in tests.
type Device struct {
	Dir string
	// Stride spaces registers in tests' plain files, where neighbouring
	// registers (0xC0011020, 0xC0011021) would otherwise overlap; the kernel
	// driver takes the offset as the register number, so it is 0 (= 1) there.
	Stride int64
}

func (d Device) offset(reg uint32) int64 {
	if d.Stride > 1 {
		return int64(reg) * d.Stride
	}
	return int64(reg)
}

// CPUs lists the CPUs that have an msr device.
func (d Device) CPUs() ([]int, error) {
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		return nil, err
	}
	var cpus []int
	for _, e := range entries {
		n, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(d.Dir, e.Name(), "msr")); err == nil {
			cpus = append(cpus, n)
		}
	}
	if len(cpus) == 0 {
		return nil, errors.New("no MSR devices (is the msr kernel module loaded?)")
	}
	sort.Ints(cpus)
	return cpus, nil
}

func (d Device) path(cpu int) string { return filepath.Join(d.Dir, strconv.Itoa(cpu), "msr") }

// Read returns register reg on cpu.
func (d Device) Read(cpu int, reg uint32) (uint64, error) {
	f, err := os.Open(d.path(cpu))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var b [8]byte
	if _, err := f.ReadAt(b[:], d.offset(reg)); err != nil {
		return 0, fmt.Errorf("reading MSR %#x on CPU %d: %w", reg, cpu, err)
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

// Write sets register reg on cpu.
func (d Device) Write(cpu int, reg uint32, v uint64) error {
	f, err := os.OpenFile(d.path(cpu), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	if _, err := f.WriteAt(b[:], d.offset(reg)); err != nil {
		return fmt.Errorf("writing MSR %#x on CPU %d: %w", reg, cpu, err)
	}
	return nil
}
