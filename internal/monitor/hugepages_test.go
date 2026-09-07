package monitor

import "testing"

const sampleMeminfo = `MemTotal:       65254040 kB
HugePages_Total:    2560
HugePages_Free:     1283
HugePages_Rsvd:        0
Hugepagesize:       2048 kB
`

func TestParseHugePages(t *testing.T) {
	total, free, size, ok := parseHugePages(sampleMeminfo)
	if !ok {
		t.Fatal("parseHugePages reported not ok on a well-formed meminfo")
	}
	if size != 2<<20 {
		t.Errorf("pageSize = %d, want %d", size, 2<<20)
	}
	if want := int64(2560) * (2 << 20); total != want {
		t.Errorf("total = %d, want %d", total, want)
	}
	if want := int64(1283) * (2 << 20); free != want {
		t.Errorf("free = %d, want %d", free, want)
	}
}

func TestParseHugePagesIncomplete(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "empty file", in: ""},
		{name: "no huge page fields at all", in: "MemTotal: 100 kB\n"},
		{
			name: "page size missing, so the counts cannot be sized",
			in:   "HugePages_Total: 2560\nHugePages_Free: 100\n",
		},
		{
			name: "a non-numeric count is not guessed at",
			in:   "HugePages_Total: lots\nHugePages_Free: 100\nHugepagesize: 2048 kB\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, _, ok := parseHugePages(c.in); ok {
				t.Error("want ok=false; a partial answer must not read as a real one")
			}
		})
	}
}

func TestRandomXHugePageBytes(t *testing.T) {
	// Dataset plus one scratchpad per thread.
	if got, want := RandomXHugePageBytes(12), RandomXDatasetBytes+12*RandomXScratchpadBytes; got != want {
		t.Errorf("RandomXHugePageBytes(12) = %d, want %d", got, want)
	}
	// A miner always has at least one thread, whatever it was asked for.
	if got := RandomXHugePageBytes(0); got != RandomXDatasetBytes+RandomXScratchpadBytes {
		t.Errorf("RandomXHugePageBytes(0) = %d, want the one-thread figure", got)
	}
}
