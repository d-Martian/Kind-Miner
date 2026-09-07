package monitor

import (
	"strconv"
	"strings"
)

// HugePages reports the machine's huge page pool: total and free bytes, and the
// size of one page.
//
// RandomX keeps a ~2 GB dataset and a 2 MiB scratchpad per thread, and walks
// them randomly. On 4 KiB pages that walk misses the TLB constantly and costs
// a large fraction of the hashrate, which is why the pool matters at all.
//
// ok is false where the pool cannot be read, or where the platform has no
// equivalent. It is *not* false merely because the pool is empty: "configured,
// but zero pages" is a real and common answer.
func HugePages() (total, free, pageSize int64, ok bool) { return readHugePages() }

// RandomXDatasetBytes is the RandomX dataset in fast mode — the mode a miner
// uses, and the bulk of what wants to live on huge pages.
const RandomXDatasetBytes int64 = 2080 << 20

// RandomXScratchpadBytes is the per-thread working set.
const RandomXScratchpadBytes int64 = 2 << 20

// RandomXHugePageBytes is what a miner with this many threads wants to keep on
// huge pages: the shared dataset plus one scratchpad per thread.
func RandomXHugePageBytes(threads int) int64 {
	if threads < 1 {
		threads = 1
	}
	return RandomXDatasetBytes + int64(threads)*RandomXScratchpadBytes
}

// HugePagesFor reports whether free huge pages cover a miner of this size, and
// when they do not, the pool size in pages that would.
//
// This exists because kind-miner runs two RandomX consumers, not one: p2pool
// verifies shares with its own dataset and starts first, so on a pool sized for
// the miner alone p2pool takes nearly all of it and the miner — the process the
// pages were reserved for — silently falls back to 4 KiB pages. Observed on a
// machine with 1280 pages reserved: p2pool held 1177 of them and xmrig got 13.
//
// short is false when there is nothing to say, including when the machine has
// no huge pages at all: a user who has not set any has not made this mistake,
// and telling them to would be a different conversation.
func HugePagesFor(threads int) (short bool, wantPages int64) {
	total, free, pageSize, ok := HugePages()
	if !ok || total == 0 || pageSize <= 0 {
		return false, 0
	}
	need := RandomXHugePageBytes(threads)
	if free >= need {
		return false, 0
	}
	// What the pool would have to be for this miner to fit alongside whatever
	// has already taken pages out of it.
	inUse := total - free
	return true, ceilDiv(inUse+need, pageSize)
}

func ceilDiv(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// parseHugePages pulls the huge page fields out of /proc/meminfo text.
//
// Kept here rather than in the Linux file so it can be tested on any host: the
// interesting cases are malformed and partial files, which are awkward to
// produce for real.
func parseHugePages(meminfo string) (total, free, pageSize int64, ok bool) {
	var haveTotal, haveFree, haveSize bool
	for _, line := range strings.Split(meminfo, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		// "HugePages_Total:    2560" and "Hugepagesize:  2048 kB" — the unit,
		// where there is one, is always kB.
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "HugePages_Total":
			total, haveTotal = n, true
		case "HugePages_Free":
			free, haveFree = n, true
		case "Hugepagesize":
			pageSize, haveSize = n<<10, true
		}
	}
	if !haveTotal || !haveFree || !haveSize {
		return 0, 0, 0, false
	}
	return total * pageSize, free * pageSize, pageSize, true
}
