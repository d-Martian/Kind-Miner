package scheduler

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/kindness"
)

type fakePressure struct{ pressured, known bool }

func (f fakePressure) Pressured() (bool, bool) { return f.pressured, f.known }

// affinityMiner is a stubMiner that also records the cores it was confined to.
type affinityMiner struct {
	stubMiner
	cores []int
}

func (m *affinityMiner) SetAffinity(c []int) error { m.cores = c; return nil }

func TestNextWide(t *testing.T) {
	t.Run("calm must last before the big cores are taken", func(t *testing.T) {
		wide, calm := false, 0
		for i := 1; i < widenAfterTicks; i++ {
			wide, calm = nextWide(wide, calm, false)
			if wide {
				t.Fatalf("widened after %d calm ticks, want %d", i, widenAfterTicks)
			}
		}
		if wide, _ = nextWide(wide, calm, false); !wide {
			t.Errorf("still narrow after %d calm ticks", widenAfterTicks)
		}
	})
	t.Run("one pressured tick hands the big cores back at once", func(t *testing.T) {
		wide, calm := nextWide(true, 100, true)
		if wide || calm != 0 {
			t.Errorf("got wide=%v calm=%d, want narrow with the calm count reset", wide, calm)
		}
	})
	t.Run("a blip part-way through restarts the wait", func(t *testing.T) {
		wide, calm := false, widenAfterTicks-1
		wide, calm = nextWide(wide, calm, true)
		wide, _ = nextWide(wide, calm, false)
		if wide {
			t.Error("widened on the first calm tick after pressure")
		}
	})
}

func TestAnyPressured(t *testing.T) {
	cases := []struct {
		name    string
		sources []pressureSource
		want    bool
	}{
		{"every service calm", []pressureSource{fakePressure{false, true}, fakePressure{false, true}}, false},
		{"one service waiting for CPU", []pressureSource{fakePressure{false, true}, fakePressure{true, true}}, true},
		{"a service that cannot say is treated as waiting", []pressureSource{fakePressure{false, false}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := anyPressured(c.sources); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestTempLimitFor(t *testing.T) {
	cases := []struct {
		name       string
		nodo       bool
		configured float64
		want       float64
	}{
		{"a desktop keeps its configured limit", false, 95, 95},
		{"a desktop may switch the limit off", false, 0, 0},
		{"a Nodo is held to 70 °C", true, 95, nodoTempLimit},
		{"switching the limit off does not lift the Nodo ceiling", true, 0, nodoTempLimit},
		{"a stricter configured limit still wins", true, 60, 60},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tempLimitFor(&config.Config{MineOnNodo: c.nodo, TempLimitCelsius: c.configured})
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestThreadsOnNodo(t *testing.T) {
	if got := Threads(&config.Config{MineOnNodo: true}); got != runtime.NumCPU() {
		t.Errorf("auto threads on a Nodo = %d, want one per core (%d) so affinity can reach every core", got, runtime.NumCPU())
	}
	if got := Threads(&config.Config{MineOnNodo: true, MaxThreads: 3}); got != 3 {
		t.Errorf("an explicit max_threads = %d, want it honoured", got)
	}
}

func TestDecideStopsWhileNodeSyncs(t *testing.T) {
	p := policy{preset: kindness.Get(kindness.Full)}
	target, reason, hard := decide(p, conditions{nodeSyncing: true}, OverrideNone)
	if target != 0 || reason != ReasonNodeSyncing || !hard {
		t.Errorf("got target=%v reason=%q hard=%v; want a hard stop while the node syncs", target, reason, hard)
	}
}

// newNodoScheduler is an RK3588-shaped scheduler: eight cores, one thread per
// core, the four A55s as the little cluster.
func newNodoScheduler(pressured bool) (*Scheduler, *affinityMiner) {
	m := &affinityMiner{}
	s := &Scheduler{
		xmrig:      m,
		cores:      8,
		maxThreads: 8,
		preset:     kindness.Get(kindness.Full),
		stopCh:     make(chan struct{}),
		Events:     make(chan StateChange, 16),
		nodo: &nodoControl{
			pressure: []pressureSource{fakePressure{pressured, true}},
			little:   []int{0, 1, 2, 3},
			all:      []int{0, 1, 2, 3, 4, 5, 6, 7},
		},
	}
	return s, m
}

func TestPlaceOnCores(t *testing.T) {
	t.Run("starts on the little cluster", func(t *testing.T) {
		s, m := newNodoScheduler(false)
		s.placeOnCores()
		if !reflect.DeepEqual(m.cores, []int{0, 1, 2, 3}) {
			t.Errorf("first tick confined the miner to %v, want the A55s", m.cores)
		}
		// Eight threads on four cores can only ever occupy half the machine,
		// so a full allowance must map to full duty, not half.
		if got := s.minerFullShare(); got != 0.5 {
			t.Errorf("full-tilt share on the little cluster = %v, want 0.5", got)
		}
	})
	t.Run("spreads to every core after sustained calm", func(t *testing.T) {
		s, m := newNodoScheduler(false)
		for i := 0; i < widenAfterTicks; i++ {
			s.placeOnCores()
		}
		if len(m.cores) != 8 {
			t.Errorf("after %d calm ticks the miner is on %v, want all eight cores", widenAfterTicks, m.cores)
		}
		if got := s.minerFullShare(); got != 1 {
			t.Errorf("full-tilt share on every core = %v, want 1", got)
		}
	})
	t.Run("stays on the little cluster while the node is under pressure", func(t *testing.T) {
		s, m := newNodoScheduler(true)
		for i := 0; i < 3*widenAfterTicks; i++ {
			s.placeOnCores()
		}
		if len(m.cores) != 4 {
			t.Errorf("miner reached %v while the node was waiting for CPU", m.cores)
		}
	})
	t.Run("does nothing outside Nodo mode", func(t *testing.T) {
		s, m := newNodoScheduler(false)
		s.nodo = nil
		s.placeOnCores()
		if m.cores != nil || s.minerFullShare() != 1 {
			t.Errorf("a desktop miner was confined to %v", m.cores)
		}
	})
}

func TestDecideHold(t *testing.T) {
	p := policy{preset: kindness.Get(kindness.Full), hold: "p2pool has no peers"}
	target, reason, hard := decide(p, conditions{}, OverrideNone)
	if target != 0 || reason != "p2pool has no peers" || !hard {
		t.Errorf("got target=%v reason=%q hard=%v; want a hard stop naming the hold", target, reason, hard)
	}
	// The user's own pause is still theirs to see.
	if _, reason, _ := decide(p, conditions{}, OverridePause); reason != "manual pause" {
		t.Errorf("manual pause under a hold reads %q", reason)
	}
	// Named over a machine condition: the hold is what the user can act on.
	p.pauseOnBattery = true
	if _, reason, _ := decide(p, conditions{onBattery: true}, OverrideNone); reason != "p2pool has no peers" {
		t.Errorf("hold on battery reads %q, want the hold", reason)
	}
}
