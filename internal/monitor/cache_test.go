package monitor

import "testing"

func TestParseCacheSize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int64
		ok   bool
	}{
		{name: "kibibyte suffix, the format Linux actually uses", in: "24576K", want: 24576 << 10, ok: true},
		{name: "trailing newline from sysfs is tolerated", in: "32768K\n", want: 32768 << 10, ok: true},
		{name: "mebibyte suffix", in: "16M", want: 16 << 20, ok: true},
		{name: "bare byte count with no suffix", in: "2097152", want: 2097152, ok: true},
		{name: "empty file reports unknown", in: "", ok: false},
		{name: "unparseable value reports unknown", in: "lots", ok: false},
		{name: "zero is not a usable cache size", in: "0K", ok: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseCacheSize(c.in)
			if ok != c.ok {
				t.Fatalf("parseCacheSize(%q) ok = %v, want %v", c.in, ok, c.ok)
			}
			if ok && got != c.want {
				t.Errorf("parseCacheSize(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
