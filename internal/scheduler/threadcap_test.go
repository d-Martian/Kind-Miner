package scheduler

import "testing"

func TestCapForCache(t *testing.T) {
	const mib = 1 << 20

	cases := []struct {
		name  string
		cores int
		l3    int64
		known bool
		want  int
	}{
		{
			name:  "caps at the cache when the cores would overflow it",
			cores: 22, l3: 24 * mib, known: true,
			want: 12,
		},
		{
			name:  "leaves the core count alone when every scratchpad fits",
			cores: 8, l3: 32 * mib, known: true,
			want: 8,
		},
		{
			name:  "keeps the core count when the cache size is unknown",
			cores: 22, l3: 0, known: false,
			want: 22,
		},
		{
			name:  "ignores a reported cache far larger than the machine",
			cores: 4, l3: 256 * mib, known: true,
			want: 4,
		},
		{
			name:  "never returns zero threads on a cache too small for one",
			cores: 8, l3: 1 * mib, known: true,
			want: 1,
		},
		{
			name:  "never returns zero threads on a machine reporting no cores",
			cores: 0, l3: 0, known: false,
			want: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := capForCache(c.cores, c.l3, c.known); got != c.want {
				t.Errorf("capForCache(%d, %d, %v) = %d, want %d",
					c.cores, c.l3, c.known, got, c.want)
			}
		})
	}
}
