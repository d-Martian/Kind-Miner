package stats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Payouts is the record of payouts kind-miner has seen p2pool announce.
//
// It is only what was seen while kind-miner was running: p2pool reports a
// payout in its log as the block is found, and nothing announces one that
// landed while the machine was off. The wallet is the full record; this is a
// glimpse of it, and the tray says so.
type Payouts struct {
	mu   sync.Mutex
	list []Payout // newest first
}

// Payout is one coinbase output p2pool reported for this wallet.
type Payout struct {
	Height uint64    `json:"height"` // Monero block that paid it
	Atomic uint64    `json:"atomic"` // amount in piconero
	SeenAt time.Time `json:"seen_at"`
}

// payoutsKept is how many payouts the record holds. The tray shows five; the
// rest cover a restart or two without the file growing forever.
const payoutsKept = 50

// NewPayouts returns an empty record.
func NewPayouts() *Payouts { return &Payouts{} }

// Record adds a payout and reports whether it was new. p2pool logs the same
// payout twice — once from the pool's side as the block is found, once from
// the sidechain as it arrives — so a block already recorded is ignored.
func (p *Payouts) Record(height, atomic uint64, at time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, x := range p.list {
		if x.Height == height {
			return false
		}
	}
	p.list = append(p.list, Payout{Height: height, Atomic: atomic, SeenAt: at})
	sort.SliceStable(p.list, func(i, j int) bool { return p.list[i].Height > p.list[j].Height })
	if len(p.list) > payoutsKept {
		p.list = p.list[:payoutsKept]
	}
	return true
}

// Recent returns up to n payouts, newest first.
func (p *Payouts) Recent(n int) []Payout {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n > len(p.list) {
		n = len(p.list)
	}
	return append([]Payout(nil), p.list[:n]...)
}

type payoutsFile struct {
	Version int      `json:"version"`
	Payouts []Payout `json:"payouts"`
}

const payoutsVersion = 1

// Save writes the record to path atomically.
func (p *Payouts) Save(path string) error {
	p.mu.Lock()
	data, err := json.MarshalIndent(payoutsFile{Version: payoutsVersion, Payouts: p.list}, "", "  ")
	p.mu.Unlock()
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

// Load replaces the record with what path holds. A missing file is an empty
// record; an unreadable one is reported and leaves it empty.
func (p *Payouts) Load(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var f payoutsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("payouts %s: %w", path, err)
	}
	if f.Version != payoutsVersion {
		return fmt.Errorf("payouts %s: version %d, want %d", path, f.Version, payoutsVersion)
	}
	p.mu.Lock()
	p.list = nil
	p.mu.Unlock()
	for _, x := range f.Payouts {
		if x.Height > 0 && x.Atomic > 0 {
			p.Record(x.Height, x.Atomic, x.SeenAt)
		}
	}
	return nil
}
