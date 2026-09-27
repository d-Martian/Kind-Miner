package scheduler

import (
	"log"
	"slices"
	"sort"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/monitor"
)

// Layouts are the two sets of cores the miner runs on: a small one while
// someone is using the machine and a full one once they have stepped away.
//
// Duty-cycling alone cannot make a full layout kind. However short its
// on-slices, a thread on every core bursts every core and the whole L3 at once,
// and the user feels those bursts as stutter even when the averaged CPU figure
// says the miner is barely there. So while someone is present the miner runs
// fewer threads, on the cores the user's work needs least, and the duty cycle
// fine-tunes within that.
type layouts struct {
	present []int
	idle    []int
}

// buildLayouts turns the machine's topology into the two layouts.
//
//   - One thread per physical core: a second hyperthread adds little to
//     RandomX and competes for the same L2.
//   - Never a core outside the L3 (the LP E-cores on Meteor Lake): its
//     scratchpad has nowhere to live, and it slows every other thread.
//   - While present: the efficiency cores, which the user's foreground work
//     is least likely to be scheduled on. On a machine with one kind of core,
//     half of them.
//   - While idle: the performance cores first, fastest first, then the
//     efficiency cores, up to cacheThreads — the same L3 limit ThreadCap
//     enforces, since past it threads evict each other's scratchpads.
func buildLayouts(cpus []monitor.CPUInfo, cacheThreads int) layouts {
	// Machines that report no cache at all (some VMs) cannot be told apart
	// by L3; every core counts.
	anyL3 := false
	for _, c := range cpus {
		anyL3 = anyL3 || c.HasL3
	}
	seen := map[int]bool{}
	var perf, eff []monitor.CPUInfo
	for _, c := range cpus {
		if anyL3 && !c.HasL3 {
			continue
		}
		core := c.ID
		if len(c.Siblings) > 0 {
			core = c.Siblings[0]
		}
		if seen[core] {
			continue
		}
		seen[core] = true
		c.ID = core
		if c.Efficiency {
			eff = append(eff, c)
		} else {
			perf = append(perf, c)
		}
	}
	byID := func(s []monitor.CPUInfo) {
		sort.Slice(s, func(i, j int) bool { return s[i].ID < s[j].ID })
	}
	byID(eff)
	byID(perf)
	sort.SliceStable(perf, func(i, j int) bool { return perf[i].MaxFreq > perf[j].MaxFreq })

	ids := func(s []monitor.CPUInfo, n int) []int {
		if n > len(s) {
			n = len(s)
		}
		out := make([]int, 0, n)
		for _, c := range s[:n] {
			out = append(out, c.ID)
		}
		return out
	}
	limit := func(n int) int {
		if cacheThreads > 0 && n > cacheThreads {
			return cacheThreads
		}
		return n
	}

	var l layouts
	if len(eff) > 0 && len(perf) > 0 {
		l.present = ids(eff, limit(len(eff)))
	} else {
		all := append(perf, eff...)
		l.present = ids(all, limit((len(all)+1)/2))
	}
	full := append(append([]monitor.CPUInfo{}, perf...), eff...)
	l.idle = ids(full, limit(len(full)))
	return l
}

// Layouts reads this machine's topology into the present and idle layouts.
// ok is false where layouts do not apply: an explicit max_threads is the
// user's own layout, the Nodo profile places threads by affinity instead, and
// a machine whose topology cannot be read keeps xmrig's own placement.
func Layouts(cfg *config.Config) (present, idle []int, ok bool) {
	if cfg.MineOnNodo || cfg.MaxThreads > 0 {
		return nil, nil, false
	}
	cpus, ok := monitor.Topology()
	if !ok {
		return nil, nil, false
	}
	cache := 0
	if l3, known := monitor.L3CacheBytes(); known {
		cache = int(l3 / randomxScratchpad)
	}
	l := buildLayouts(cpus, cache)
	if len(l.present) == 0 || len(l.idle) == 0 {
		return nil, nil, false
	}
	return l.present, l.idle, true
}

// layoutSetter is implemented by engines that can be re-threaded in place.
// Separate from miner, like starter: a layout change keeps the dataset, but it
// is still a change of cores, not the throttle.
type layoutSetter interface {
	SetLayout([]int) error
}

// placeLayout moves the miner onto the layout the idle gate calls for: the
// small one while the user is present, the full one once they have been away
// for idle_full_after_seconds. The gate already rises slowly and falls at once,
// so the layout does too — the user returning shrinks it on the next tick.
func (s *Scheduler) placeLayout(present bool) {
	if !s.haveLayouts {
		return
	}
	want := s.layouts.idle
	if present {
		want = s.layouts.present
	}
	if slices.Equal(want, s.layoutNow) {
		return
	}
	ls, ok := s.xmrig.(layoutSetter)
	if !ok {
		return
	}
	if err := ls.SetLayout(want); err != nil {
		log.Printf("could not change the miner's cores: %v", err)
		return
	}
	s.layoutNow = want
	s.mu.Lock()
	s.layoutThreads = len(want)
	s.mu.Unlock()
	if present {
		log.Printf("You're here: mining on %d cores %v", len(want), want)
	} else {
		log.Printf("You're away: mining on %d cores %v", len(want), want)
	}
}
