package monitor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file holds the samplers the Nodo profile needs: memory for the RandomX
// mode decision, the CPU clusters of a big.LITTLE board, CPU pressure on the
// node's own services, and whether the node is synchronised. The parsers are
// here rather than in the Linux file so they can be tested on any host.

// Memory reports total and available RAM in bytes. ok is false where it cannot
// be read.
func Memory() (total, available int64, ok bool) { return readMemory() }

// parseMemInfo pulls MemTotal and MemAvailable out of /proc/meminfo text.
func parseMemInfo(meminfo string) (total, available int64, ok bool) {
	var haveTotal, haveAvail bool
	for _, line := range strings.Split(meminfo, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			total, haveTotal = n<<10, true
		case "MemAvailable":
			available, haveAvail = n<<10, true
		}
	}
	return total, available, haveTotal && haveAvail
}

// CoreClusters splits the machine's CPUs into the low-power cluster and the
// whole set. On a board with one kind of core both are the same list.
//
// ok is false where the capacities cannot be read, in which case callers should
// treat every core as the same kind.
func CoreClusters() (little, all []int, ok bool) {
	caps, ok := readCoreCapacities()
	if !ok {
		return nil, nil, false
	}
	little, all = splitClusters(caps)
	return little, all, len(all) > 0
}

// splitClusters takes a capacity per CPU and returns the CPUs sharing the
// lowest capacity, and every CPU, both sorted.
//
// Capacity is the kernel's own ranking of the cores (cpu_capacity on arm64, or
// the maximum frequency as a stand-in), so the cheapest cores are the ones it
// already considers least valuable to everything else.
func splitClusters(caps map[int]int64) (little, all []int) {
	if len(caps) == 0 {
		return nil, nil
	}
	lowest := int64(-1)
	for cpu, c := range caps {
		all = append(all, cpu)
		if lowest < 0 || c < lowest {
			lowest = c
		}
	}
	for cpu, c := range caps {
		if c == lowest {
			little = append(little, cpu)
		}
	}
	sort.Ints(all)
	sort.Ints(little)
	return little, all
}

// pressureLimit is the share of the last ten seconds, in percent, that a
// service may spend waiting for a CPU before the miner counts as in its way.
// The node services are nearly idle once synced, so any sustained stall is the
// miner's doing; a couple of percent tolerates the odd scheduling blip.
const pressureLimit = 2.0

// busyCoresLimit stands in for pressureLimit on kernels without PSI. Without a
// stall figure the best available signal is the service being busy at all: a
// synced monerod averages well under a tenth of a core, so half a core means it
// is doing real work (a block, a reorg, a wallet scan) and wants the big cores.
const busyCoresLimit = 0.5

// ServicePressure samples whether a systemd service is short of CPU, from its
// cgroup. It keeps the previous cpu.stat reading for the fallback path.
type ServicePressure struct {
	unit string

	mu        sync.Mutex
	lastUsage int64
	lastAt    time.Time
}

// NewServicePressure returns a sampler for unit, e.g. "monerod.service".
func NewServicePressure(unit string) *ServicePressure {
	return &ServicePressure{unit: unit}
}

// Pressured reports whether the service is being held off the CPU.
//
// A service that is not installed has nothing to protect and reads as not
// pressured, known — Nodo's light-wallet server is optional. known is false
// when the service exists but neither cpu.pressure nor cpu.stat answered, and
// on the first fallback sample, which has no delta yet; callers must treat that
// as "do not take more", not as calm.
func (p *ServicePressure) Pressured() (pressured, known bool) {
	return p.sample(time.Now())
}

// psiSomeAvg10 reads the "some avg10" figure from a cpu.pressure file: the
// percentage of the last ten seconds in which at least one task in the group
// was runnable but waiting for a CPU.
func psiSomeAvg10(text string) (float64, bool) { return psiAvg10(text, "some") }

// cpuStatUsage reads usage_usec from a cgroup v2 cpu.stat file.
func cpuStatUsage(text string) (int64, bool) {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "usage_usec" {
			n, err := strconv.ParseInt(fields[1], 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// busyCores converts two usage_usec readings into the average number of cores
// the service kept busy between them.
func busyCores(prevUsec, curUsec int64, elapsed time.Duration) float64 {
	if elapsed <= 0 || curUsec < prevUsec {
		return 0
	}
	return float64(curUsec-prevUsec) / float64(elapsed.Microseconds())
}

// NodeSync asks the local monerod whether it is synchronised. The answer is
// cached for a while: sync state changes over minutes, and a request every
// scheduler tick would be load on the very service the miner is yielding to.
type NodeSync struct {
	url    string
	client *http.Client

	mu        sync.Mutex
	checkedAt time.Time
	synced    bool
	known     bool
}

// nodeSyncTTL is how long a sync answer is reused.
const nodeSyncTTL = 30 * time.Second

// NewNodeSync returns a sampler for the monerod RPC at addr ("127.0.0.1:18081").
//
// On a Nodo that is monerod's default unrestricted listener, which Nodo's start
// script leaves in place beside its own restricted one.
func NewNodeSync(addr string) *NodeSync {
	return &NodeSync{
		url:    "http://" + addr + "/get_info",
		client: &http.Client{Timeout: 3 * time.Second},
	}
}

// Synced reports whether the node has finished syncing. known is false when
// the node did not answer, or answered without saying; a caller must not read
// that as "still syncing" and stop mining on it, or a node behind an RPC login
// would silently never mine.
func (n *NodeSync) Synced() (synced, known bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.checkedAt.IsZero() && time.Since(n.checkedAt) < nodeSyncTTL {
		return n.synced, n.known
	}
	n.synced, n.known = n.fetch()
	n.checkedAt = time.Now()
	return n.synced, n.known
}

func (n *NodeSync) fetch() (synced, known bool) {
	resp, err := n.client.Get(n.url)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return false, false
	}
	s, err := parseGetInfo(body)
	if err != nil {
		return false, false
	}
	return s, true
}

// parseGetInfo reads monerod's /get_info reply. A node counts as synced only
// when it says so and is not busy catching up — "synchronized" alone stays true
// through a burst of new blocks after the node has been offline.
func parseGetInfo(body []byte) (bool, error) {
	var info struct {
		Synchronized *bool `json:"synchronized"`
		BusySyncing  bool  `json:"busy_syncing"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return false, err
	}
	if info.Synchronized == nil {
		return false, fmt.Errorf("get_info reply has no synchronized field")
	}
	return *info.Synchronized && !info.BusySyncing, nil
}
