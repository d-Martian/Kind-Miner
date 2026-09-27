package scheduler

import (
	"reflect"
	"testing"

	"github.com/kind-miner/kind-miner/internal/monitor"
)

// meteorLake155H is this repo's development machine as sysfs describes it:
// six P-cores with two threads each (0–11), eight E-cores (12–19) sharing the
// 24 MiB L3, and two LP E-cores (20–21) outside it.
func meteorLake155H() []monitor.CPUInfo {
	var cpus []monitor.CPUInfo
	pairs := [][2]int{{0, 5}, {1, 2}, {3, 4}, {6, 7}, {8, 9}, {10, 11}}
	for _, p := range pairs {
		freq := int64(4_500_000)
		if p[0] == 1 || p[0] == 3 {
			freq = 4_800_000
		}
		for _, id := range p {
			cpus = append(cpus, monitor.CPUInfo{ID: id, Siblings: []int{p[0], p[1]}, HasL3: true, MaxFreq: freq})
		}
	}
	for id := 12; id <= 19; id++ {
		cpus = append(cpus, monitor.CPUInfo{ID: id, Siblings: []int{id}, Efficiency: true, HasL3: true, MaxFreq: 3_800_000})
	}
	for id := 20; id <= 21; id++ {
		cpus = append(cpus, monitor.CPUInfo{ID: id, Siblings: []int{id}, Efficiency: true, HasL3: false, MaxFreq: 2_500_000})
	}
	return cpus
}

func TestLayoutsOnThe155H(t *testing.T) {
	// 24 MiB of L3 holds twelve 2 MiB scratchpads.
	l := buildLayouts(meteorLake155H(), 12)
	// The plan: "8 E-cores while you work, P-cores added when idle, the 2
	// LP-E cores never".
	if want := []int{12, 13, 14, 15, 16, 17, 18, 19}; !reflect.DeepEqual(l.present, want) {
		t.Errorf("present = %v, want the eight E-cores %v", l.present, want)
	}
	// One thread per P-core, fastest first, then E-cores up to the cache.
	if want := []int{1, 3, 0, 6, 8, 10, 12, 13, 14, 15, 16, 17}; !reflect.DeepEqual(l.idle, want) {
		t.Errorf("idle = %v, want %v", l.idle, want)
	}
	for _, id := range append(l.present, l.idle...) {
		if id == 20 || id == 21 {
			t.Errorf("layout uses LP E-core %d, which has no L3", id)
		}
		if id == 2 || id == 4 || id == 5 || id == 7 || id == 9 || id == 11 {
			t.Errorf("layout uses %d, the second hyperthread of a P-core", id)
		}
	}
}

func TestLayoutsOnAPlainDesktop(t *testing.T) {
	// Eight cores, SMT, one kind: half of them while present.
	var cpus []monitor.CPUInfo
	for core := 0; core < 8; core++ {
		for _, id := range []int{core, core + 8} {
			cpus = append(cpus, monitor.CPUInfo{ID: id, Siblings: []int{core, core + 8}, HasL3: true})
		}
	}
	l := buildLayouts(cpus, 16)
	if want := []int{0, 1, 2, 3}; !reflect.DeepEqual(l.present, want) {
		t.Errorf("present = %v, want half the physical cores", l.present)
	}
	if want := []int{0, 1, 2, 3, 4, 5, 6, 7}; !reflect.DeepEqual(l.idle, want) {
		t.Errorf("idle = %v, want every physical core once", l.idle)
	}
}

func TestLayoutsWithoutCacheInfo(t *testing.T) {
	// A VM that reports no caches: nothing can be excluded for lacking L3.
	cpus := []monitor.CPUInfo{{ID: 0, Siblings: []int{0}}, {ID: 1, Siblings: []int{1}}}
	if l := buildLayouts(cpus, 0); len(l.idle) != 2 {
		t.Errorf("idle = %v, want both cores", l.idle)
	}
}

// layoutMiner records layouts, standing in for XMRig.
type layoutMiner struct {
	stubMiner
	set [][]int
}

func (m *layoutMiner) SetLayout(c []int) error { m.set = append(m.set, c); return nil }

func TestPlaceLayoutFollowsTheIdleGate(t *testing.T) {
	m := &layoutMiner{}
	l := buildLayouts(meteorLake155H(), 12)
	s := &Scheduler{xmrig: m, cores: 22, maxThreads: 12,
		layouts: l, haveLayouts: true, layoutNow: l.present, layoutThreads: len(l.present)}

	s.placeLayout(true)
	if len(m.set) != 0 {
		t.Fatalf("re-set the layout it already had: %v", m.set)
	}
	s.placeLayout(false) // the user has been away long enough
	if len(m.set) != 1 || len(m.set[0]) != 12 {
		t.Fatalf("away: set %v, want the 12-core idle layout", m.set)
	}
	if got := s.minerFullShare(); got != 12.0/22 {
		t.Errorf("full-tilt share on the idle layout = %v, want 12/22", got)
	}
	s.placeLayout(true) // back at the keyboard
	if len(m.set) != 2 || len(m.set[1]) != 8 {
		t.Fatalf("present: set %v, want the E-cores", m.set)
	}
	if got := s.minerFullShare(); got != 8.0/22 {
		t.Errorf("full-tilt share on the present layout = %v, want 8/22", got)
	}
	s.duty = 0.5
	if active, total := s.ActiveThreads(); active != 4 || total != 8 {
		t.Errorf("ActiveThreads = %d of %d, want 4 of 8", active, total)
	}
}

func TestNoLayoutsLeavesTheMinerAlone(t *testing.T) {
	m := &layoutMiner{}
	s := &Scheduler{xmrig: m, cores: 8, maxThreads: 8}
	s.placeLayout(false)
	s.placeLayout(true)
	if len(m.set) != 0 {
		t.Errorf("changed layouts without any: %v", m.set)
	}
}
