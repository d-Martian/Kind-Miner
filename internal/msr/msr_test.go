package msr

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func cpuinfo(vendor string, family, model int) string {
	return "processor\t: 0\nvendor_id\t: " + vendor + "\ncpu family\t: " + itoa(family) +
		"\nmodel\t\t: " + itoa(model) + "\nmodel name\t: x\n\nprocessor\t: 1\nvendor_id\t: other\n"
}

func itoa(n int) string {
	return string(rune('0'+n/100%10)) + string(rune('0'+n/10%10)) + string(rune('0'+n%10))
}

// The preset each CPU gets must be the one xmrig 6.26.0 would pick
// (BasicCpuInfo.cpp), or the "same boost as Gupax" claim is wrong.
func TestDetect(t *testing.T) {
	cases := []struct {
		name   string
		info   string
		preset string
		ok     bool
	}{
		{"Intel, any model", cpuinfo("GenuineIntel", 6, 170), "Intel", true},
		{"Zen 2", cpuinfo("AuthenticAMD", 0x17, 113), "AMD Ryzen (Zen, Zen+, Zen 2)", true},
		{"Zen 3", cpuinfo("AuthenticAMD", 0x19, 0x21), "AMD Ryzen (Zen 3)", true},
		{"Zen 4 desktop (model 0x61)", cpuinfo("AuthenticAMD", 0x19, 0x61), "AMD Ryzen (Zen 4)", true},
		{"Zen 4 mobile (model 0x75)", cpuinfo("AuthenticAMD", 0x19, 0x75), "AMD Ryzen (Zen 4)", true},
		{"Zen 5", cpuinfo("AuthenticAMD", 0x1a, 0x44), "AMD Ryzen (Zen 5)", true},
		{"an AMD before Zen has no preset", cpuinfo("AuthenticAMD", 0x15, 2), "", false},
		{"ARM has no MSRs to set", "processor\t: 0\nCPU implementer\t: 0x41\n", "", false},
	}
	for _, c := range cases {
		p, ok := Detect(c.info)
		if ok != c.ok || p.Name != c.preset {
			t.Errorf("%s: got %q %v, want %q %v", c.name, p.Name, ok, c.preset, c.ok)
		}
	}
}

func TestMasked(t *testing.T) {
	// xmrig keeps bit 5 of 0xC0011021 and takes the rest from the preset.
	mask := ^uint64(0x20)
	if got := Masked(0xffff, 0x40, mask); got != 0x60 {
		t.Errorf("Masked = %#x, want 0x60 (bit 5 kept, the rest from the preset)", got)
	}
	if got := Masked(0xffff, 0x40, NoMask); got != 0x40 {
		t.Errorf("NoMask = %#x, want the value whole", got)
	}
}

// fakeDevice is /dev/cpu with sparse files: an 8-byte value at offset reg.
func fakeDevice(t *testing.T, cpus int, init map[uint32]uint64) Device {
	t.Helper()
	dir := t.TempDir()
	for c := 0; c < cpus; c++ {
		p := filepath.Join(dir, strconv.Itoa(c), "msr")
		os.MkdirAll(filepath.Dir(p), 0o755)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		for reg, v := range init {
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], v)
			f.WriteAt(b[:], int64(reg)*8)
		}
		f.Close()
	}
	return Device{Dir: dir, Stride: 8}
}

func TestApplyAndRestore(t *testing.T) {
	orig := map[uint32]uint64{0xC0011020: 0x1111, 0xC0011021: 0x2222 | 0x20, 0xC0011022: 0x3333, 0xC001102b: 0x4444}
	d := fakeDevice(t, 4, orig)
	state := filepath.Join(t.TempDir(), "state", "original.json")
	p := ryzen19h

	if on, _ := Applied(d, p); on {
		t.Fatal("applied before Apply")
	}
	if err := Apply(d, p, state); err != nil {
		t.Fatal(err)
	}
	if on, err := Applied(d, p); err != nil || !on {
		t.Fatalf("not applied after Apply: %v", err)
	}
	for _, cpu := range []int{0, 3} {
		v, _ := d.Read(cpu, 0xC0011021)
		if want := Masked(0x2222|0x20, 0x001c000200000040, ^uint64(0x20)); v != want {
			t.Errorf("cpu %d 0xC0011021 = %#x, want %#x", cpu, v, want)
		}
	}
	// A second Apply — every boot with --at-boot — must not save the boosted
	// values as "original", or off could never undo it.
	if err := Apply(d, p, state); err != nil {
		t.Fatal(err)
	}
	if err := Restore(d, state); err != nil {
		t.Fatal(err)
	}
	for reg, want := range orig {
		for _, cpu := range []int{0, 1, 2, 3} {
			if v, _ := d.Read(cpu, reg); v != want {
				t.Errorf("cpu %d %#x = %#x after Restore, want %#x", cpu, reg, v, want)
			}
		}
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Error("Restore left the saved values behind")
	}
	if err := Restore(d, state); err == nil {
		t.Error("a second Restore did not say there was nothing to undo")
	}
}

func TestCPUsNeedTheDriver(t *testing.T) {
	if _, err := (Device{Dir: t.TempDir()}).CPUs(); err == nil {
		t.Error("no msr devices was not an error")
	}
	d := fakeDevice(t, 12, nil)
	cpus, err := d.CPUs()
	if err != nil || len(cpus) != 12 || cpus[11] != 11 {
		t.Errorf("CPUs = %v, %v", cpus, err)
	}
}
