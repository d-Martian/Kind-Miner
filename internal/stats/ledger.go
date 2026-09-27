package stats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Ledger keeps the last 24 hours of hashrate at one-minute resolution, for the
// rates the tray quotes: 30 minutes and 24 hours, alongside "now".
//
// They come from kind-miner's own record rather than XMRig's 10 s / 60 s /
// 15 m windows for two reasons. XMRig's clock stops while it is suspended, so
// its averages describe only the moments it was allowed to run and overstate
// what a throttled miner actually earns; the scheduler records a paused tick as
// zero, which is the truth. And they reset with every restart, where the
// ledger is saved and survives one.
//
// Each minute holds the sum of that minute's samples and how many there were,
// so a rate over any window is total ÷ count — every tick weighted equally,
// however the minutes were cut. Time kind-miner was not running has no samples
// and so does not count: the rates describe the machine while it mined.
type Ledger struct {
	mu      sync.Mutex
	minutes []minute // oldest first, one per minute that had samples
}

type minute struct {
	At  int64   `json:"t"`   // start of the minute, Unix seconds
	Sum float64 `json:"sum"` // sum of the hashrate samples in it, H/s
	N   int     `json:"n"`   // how many samples
}

// LedgerSpan is how much history the ledger keeps.
const LedgerSpan = 24 * time.Hour

// NewLedger returns an empty ledger.
func NewLedger() *Ledger { return &Ledger{} }

// Add records one sample's hashrate.
func (l *Ledger) Add(s Sample) {
	at := s.At.Truncate(time.Minute).Unix()
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := len(l.minutes); n > 0 && l.minutes[n-1].At == at {
		l.minutes[n-1].Sum += s.Hashrate
		l.minutes[n-1].N++
		return
	}
	l.minutes = append(l.minutes, minute{At: at, Sum: s.Hashrate, N: 1})
	l.prune(s.At)
}

// prune drops minutes older than LedgerSpan before now. Caller holds mu.
func (l *Ledger) prune(now time.Time) {
	cutoff := now.Add(-LedgerSpan).Unix()
	i := 0
	for i < len(l.minutes) && l.minutes[i].At < cutoff {
		i++
	}
	if i > 0 {
		l.minutes = append(l.minutes[:0], l.minutes[i:]...)
	}
}

// Rate returns the mean hashrate over the window ending at now.
//
// ok is false until the ledger holds at least half the window: a "24 h" figure
// computed from ten minutes of data would claim a measurement it does not
// have, and the house rule is to show "unknown" rather than a number that only
// looks like one.
func (l *Ledger) Rate(window time.Duration, now time.Time) (float64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-window).Unix()
	var sum float64
	var n, covered int
	for i := len(l.minutes) - 1; i >= 0 && l.minutes[i].At >= cutoff; i-- {
		sum += l.minutes[i].Sum
		n += l.minutes[i].N
		covered++
	}
	if n == 0 || time.Duration(covered)*time.Minute < window/2 {
		return 0, false
	}
	return sum / float64(n), true
}

// ledgerFile is the on-disk form. The version lets a later format change drop
// an old file instead of misreading it.
type ledgerFile struct {
	Version int      `json:"version"`
	Minutes []minute `json:"minutes"`
}

const ledgerVersion = 1

// Save writes the ledger to path atomically: a crash mid-write leaves the
// previous file, never half of one.
func (l *Ledger) Save(path string) error {
	l.mu.Lock()
	data, err := json.Marshal(ledgerFile{Version: ledgerVersion, Minutes: l.minutes})
	l.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load replaces the ledger with what path holds, keeping only the last
// LedgerSpan before now. A missing file is an empty ledger, not an error; an
// unreadable one is reported and also leaves the ledger empty — losing a day
// of averages is better than refusing to mine.
func (l *Ledger) Load(path string, now time.Time) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var f ledgerFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("ledger %s: %w", path, err)
	}
	if f.Version != ledgerVersion {
		return fmt.Errorf("ledger %s: version %d, want %d", path, f.Version, ledgerVersion)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minutes = l.minutes[:0]
	for _, m := range f.Minutes {
		// Out of order or in the future means the clock moved or the file was
		// edited; neither is worth trusting.
		if m.N <= 0 || m.At > now.Unix() || (len(l.minutes) > 0 && m.At <= l.minutes[len(l.minutes)-1].At) {
			continue
		}
		l.minutes = append(l.minutes, m)
	}
	l.prune(now)
	return nil
}
