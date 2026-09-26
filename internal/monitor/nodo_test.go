package monitor

import (
	"reflect"
	"testing"
	"time"
)

func TestParseMemInfo(t *testing.T) {
	total, avail, ok := parseMemInfo("MemTotal:       16303376 kB\nMemFree:  1000 kB\nMemAvailable:   9876543 kB\n")
	if !ok {
		t.Fatal("want ok on a well-formed meminfo")
	}
	if total != 16303376<<10 || avail != 9876543<<10 {
		t.Errorf("got total=%d avail=%d", total, avail)
	}
	// Kernels before 3.14 have no MemAvailable; MemFree is not a stand-in,
	// because it leaves out the page cache the kernel would give back.
	if _, _, ok := parseMemInfo("MemTotal: 100 kB\nMemFree: 50 kB\n"); ok {
		t.Error("want ok=false without MemAvailable")
	}
}

func TestSplitClusters(t *testing.T) {
	cases := []struct {
		name       string
		caps       map[int]int64
		wantLittle []int
		wantAll    []int
	}{
		{
			// The RK3588: four A55s, then two pairs of A76s that the kernel
			// ranks slightly differently. Only the A55s are the little cluster.
			name:       "RK3588 picks the four A55s",
			caps:       map[int]int64{0: 414, 1: 414, 2: 414, 3: 414, 4: 1024, 5: 1024, 6: 1019, 7: 1019},
			wantLittle: []int{0, 1, 2, 3},
			wantAll:    []int{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			name:       "a machine of one kind of core is all one cluster",
			caps:       map[int]int64{0: 1024, 1: 1024},
			wantLittle: []int{0, 1},
			wantAll:    []int{0, 1},
		},
		{name: "nothing to split", caps: nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			little, all := splitClusters(c.caps)
			if !reflect.DeepEqual(little, c.wantLittle) || !reflect.DeepEqual(all, c.wantAll) {
				t.Errorf("got little=%v all=%v, want %v %v", little, all, c.wantLittle, c.wantAll)
			}
		})
	}
}

func TestPSISomeAvg10(t *testing.T) {
	const text = "some avg10=3.25 avg60=1.00 avg300=0.40 total=123456\nfull avg10=9.99 avg60=0.00 avg300=0.00 total=0\n"
	got, ok := psiSomeAvg10(text)
	if !ok || got != 3.25 {
		t.Errorf("got %v, %v; want the some line's 3.25, never the full line's", got, ok)
	}
	if _, ok := psiSomeAvg10(""); ok {
		t.Error("want ok=false on an empty file (psi=0 kernels)")
	}
}

func TestCPUStatUsage(t *testing.T) {
	got, ok := cpuStatUsage("usage_usec 5000000\nuser_usec 4000000\nsystem_usec 1000000\n")
	if !ok || got != 5000000 {
		t.Errorf("got %d, %v", got, ok)
	}
	if _, ok := cpuStatUsage("user_usec 1\n"); ok {
		t.Error("want ok=false without usage_usec")
	}
}

func TestBusyCores(t *testing.T) {
	if got := busyCores(0, 1_000_000, 2*time.Second); got != 0.5 {
		t.Errorf("one CPU-second over two seconds = %v, want 0.5", got)
	}
	// A service restart resets its counter; that is not negative load.
	if got := busyCores(5_000_000, 1_000, 2*time.Second); got != 0 {
		t.Errorf("counter reset = %v, want 0", got)
	}
}

func TestParseGetInfo(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    bool
		wantErr bool
	}{
		{name: "synced and idle", body: `{"synchronized":true,"busy_syncing":false}`, want: true},
		{name: "still syncing", body: `{"synchronized":false,"busy_syncing":true}`, want: false},
		{
			// After time offline, monerod keeps reporting synchronized while
			// it pulls the blocks it missed.
			name: "catching up after downtime is not synced",
			body: `{"synchronized":true,"busy_syncing":true}`, want: false,
		},
		{name: "a reply that does not say is not an answer", body: `{"status":"OK"}`, wantErr: true},
		{name: "not JSON", body: `Unauthorized`, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseGetInfo([]byte(c.body))
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
